// Package webhook delivers events to endpoints a tenant administrator
// registers.
//
// The interesting problem here is not retries. It is that Portico usually
// runs inside a network, and the destination URL is supplied by whoever
// administers a tenant. An unrestricted outbound request to an
// administrator-chosen address turns an identity provider into a request
// proxy against its own infrastructure: the cloud metadata service at
// 169.254.169.254 hands out credentials to anything that asks, and
// 127.0.0.1:5432 is the database this process is already authenticated to.
//
// So a destination is checked twice, and the second check is the one that
// matters. Checking only at registration is defeated by a name that resolves
// to a public address then and to 127.0.0.1 later — DNS rebinding, which
// needs no privileged position and no race, only a DNS record the attacker
// controls. The address the request actually connects to is therefore
// verified at connection time, inside the dialer, after resolution.
package webhook

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// ErrDestinationNotAllowed is returned for a URL this server will not call.
var ErrDestinationNotAllowed = errors.New("destination not allowed")

// ValidateDestination checks a URL at registration time.
//
// It refuses everything the dialer would refuse later, so an administrator
// finds out while they are looking at the form rather than from a delivery
// log. The dialer still checks: this one resolves a name that may resolve
// differently by the time anything is sent.
func ValidateDestination(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("%w: not a valid URL", ErrDestinationNotAllowed)
	}

	// HTTPS only. A webhook carries who exists in this directory and when
	// they were disabled, signed with a shared secret — over plaintext, both
	// the payload and the signature are readable by anything on the path,
	// and the signature stops being evidence of anything.
	if parsed.Scheme != "https" {
		return fmt.Errorf("%w: the URL must be https", ErrDestinationNotAllowed)
	}
	if parsed.Host == "" {
		return fmt.Errorf("%w: the URL has no host", ErrDestinationNotAllowed)
	}
	if parsed.User != nil {
		// Credentials in the URL end up in the subscription list, the audit
		// trail, and any log that records the destination.
		return fmt.Errorf("%w: the URL must not contain credentials", ErrDestinationNotAllowed)
	}

	host := parsed.Hostname()

	// A literal address is checked directly; there is nothing to resolve and
	// nothing that could change later.
	if ip := net.ParseIP(host); ip != nil {
		return checkIP(ip)
	}

	addresses, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("%w: %s does not resolve", ErrDestinationNotAllowed, host)
	}
	// Every address, not the first. A name with one public and one loopback
	// address would otherwise pass here and connect to the loopback one,
	// since which address a dialer picks is not something this code decides.
	for _, ip := range addresses {
		if err := checkIP(ip); err != nil {
			return err
		}
	}
	return nil
}

// checkIP refuses the addresses that are not somebody else's server.
func checkIP(ip net.IP) error {
	switch {
	case ip.IsLoopback():
		return fmt.Errorf("%w: %s is a loopback address", ErrDestinationNotAllowed, ip)
	case ip.IsPrivate():
		return fmt.Errorf("%w: %s is a private address", ErrDestinationNotAllowed, ip)
	case ip.IsLinkLocalUnicast(), ip.IsLinkLocalMulticast():
		// 169.254.0.0/16, which is where every major cloud puts its metadata
		// service. This is the single most valuable target reachable from
		// inside a machine like this one.
		return fmt.Errorf("%w: %s is a link-local address", ErrDestinationNotAllowed, ip)
	case ip.IsUnspecified():
		return fmt.Errorf("%w: %s is unspecified", ErrDestinationNotAllowed, ip)
	case ip.IsMulticast():
		return fmt.Errorf("%w: %s is a multicast address", ErrDestinationNotAllowed, ip)
	case isSharedAddressSpace(ip):
		return fmt.Errorf("%w: %s is in carrier-grade NAT space", ErrDestinationNotAllowed, ip)
	}
	return nil
}

// isSharedAddressSpace reports whether ip is in 100.64.0.0/10 (RFC 6598).
//
// Not covered by IsPrivate, and it is where a good deal of container and
// carrier infrastructure lives — including, on some platforms, the metadata
// endpoint that 169.254.169.254 is merely the famous address of.
func isSharedAddressSpace(ip net.IP) bool {
	v4 := ip.To4()
	if v4 == nil {
		return false
	}
	return v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127
}

// NewClient returns an HTTP client that refuses to connect to an address a
// destination is not allowed to reach.
//
// The check lives in the dialer rather than before the request because that
// is the only place the address is known. Between validating a URL and
// sending to it, a name can start resolving somewhere else; here, the
// address being connected to is the address being checked, and there is no
// window between the two.
func NewClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
		// Control, not Transport.DialContext.
		//
		// The two look interchangeable and are not. DialContext is handed the
		// address out of the URL, with the host still a name; Control is
		// called once per candidate address after resolution, with the
		// address the socket is about to be connected to. Checking in
		// DialContext meant net.ParseIP returned nil for every destination
		// that was not already an IP literal, and the check was skipped —
		// which is every destination this check exists for, since a URL
		// holding a loopback literal is refused at registration and a name is
		// the only thing that can resolve somewhere else afterwards.
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip := net.ParseIP(host)
			if ip == nil {
				// Unreachable: Control is called with a resolved address. If
				// it ever is not, refusing is the safe direction.
				return fmt.Errorf("%w: %s did not resolve to an address",
					ErrDestinationNotAllowed, host)
			}
			return checkIP(ip)
		},
	}

	transport := &http.Transport{
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		// One connection per destination is plenty, and an idle pool of
		// connections to somebody else's server is not worth keeping.
		MaxIdleConnsPerHost: 1,
		IdleConnTimeout:     30 * time.Second,
	}

	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		// Redirects are not followed. A destination that answers 302 to
		// http://169.254.169.254/ would otherwise walk the request straight
		// past every check above — the dialer would see the metadata address
		// and refuse, but only because of the dialer. Refusing here as well
		// means a redirect is reported as what it is rather than as a
		// connection failure, and a subscription cannot be pointed somewhere
		// its owner did not name.
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}
