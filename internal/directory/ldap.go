// Package directory reads accounts out of an LDAP directory.
//
// It is deliberately the thinnest layer that can exist between go-ldap and
// the service that reconciles what it returns: connect, bind, search, map
// attributes to a neutral struct. No database, no policy, no decisions about
// what to do with an entry that has vanished. Those live in the service,
// where they can be tested without a directory at all.
//
// Nothing here authenticates a person. Binding as somebody to check their
// password is a different feature — federation rather than synchronization —
// and mixing it in would put a login path inside a package whose whole job
// is a scheduled read.
package directory

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/go-ldap/ldap/v3"
)

// Encryption is how the connection to a directory is protected.
const (
	// EncryptionNone is plain LDAP on the wire. Permitted because plenty of
	// directories sit on a segment where that is the deployed reality, and
	// refusing it only moves the integration into a script nobody reviews.
	EncryptionNone = "none"
	// EncryptionSTARTTLS negotiates TLS on the plain port.
	EncryptionSTARTTLS = "starttls"
	// EncryptionTLS connects over TLS from the first byte (LDAPS).
	EncryptionTLS = "tls"
)

// Config is everything needed to read one directory.
type Config struct {
	Host       string
	Port       int
	Encryption string

	// BindDN empty means an anonymous bind, which some read-only
	// directories allow.
	BindDN       string
	BindPassword string

	BaseDN     string
	UserFilter string

	Attributes AttributeMap

	// Timeout bounds the whole conversation. A directory that accepts a
	// connection and then stops talking would otherwise hold a sync open
	// until the process restarts.
	Timeout time.Duration
}

// AttributeMap says which LDAP attribute carries which fact.
//
// There are no defaults here on purpose. AD and OpenLDAP disagree on every
// one of these — sAMAccountName against uid, objectGUID against entryUUID —
// and a guess that is wrong imports a directory's worth of accounts named
// after the wrong field, which looks like a working integration.
type AttributeMap struct {
	Username    string
	DisplayName string
	Email       string
	Phone       string
	ExternalID  string
}

// Entry is one account as the directory describes it.
type Entry struct {
	DN          string
	Username    string
	DisplayName string
	Email       string
	Phone       string
	// ExternalID is the directory's own stable identifier, already in
	// canonical text form. This is what reconciliation matches on, so it is
	// the one field whose encoding cannot be approximate.
	ExternalID string
}

// ErrNoExternalID is reported for an entry whose identifying attribute is
// absent. Such an entry is skipped rather than imported: without it a rename
// in the directory becomes a second account here, which is the failure this
// whole feature exists to prevent.
var ErrNoExternalID = errors.New("entry has no value for the external id attribute")

// ErrNoUsername is reported for an entry with nothing to call the account.
var ErrNoUsername = errors.New("entry has no value for the username attribute")

// Client is a connection to one directory. Not safe for concurrent use; a
// sync opens one, reads, and closes it.
type Client struct {
	conn *ldap.Conn
	cfg  Config
}

// Dial connects and binds.
func Dial(cfg Config) (*Client, error) {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}

	address := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	dialer := &net.Dialer{Timeout: cfg.Timeout}

	var conn *ldap.Conn
	var err error
	switch cfg.Encryption {
	case EncryptionTLS:
		conn, err = ldap.DialURL("ldaps://"+address,
			ldap.DialWithDialer(dialer),
			ldap.DialWithTLSConfig(&tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12}))
	default:
		conn, err = ldap.DialURL("ldap://"+address, ldap.DialWithDialer(dialer))
	}
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", address, err)
	}

	if cfg.Encryption == EncryptionSTARTTLS {
		if err := conn.StartTLS(&tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12}); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("start TLS: %w", err)
		}
	}

	conn.SetTimeout(cfg.Timeout)

	if cfg.BindDN == "" {
		err = conn.UnauthenticatedBind("")
	} else {
		err = conn.Bind(cfg.BindDN, cfg.BindPassword)
	}
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("bind as %q: %w", cfg.BindDN, err)
	}

	return &Client{conn: conn, cfg: cfg}, nil
}

// Close releases the connection.
func (c *Client) Close() {
	if c != nil && c.conn != nil {
		_ = c.conn.Close()
	}
}

// Users returns every entry matching the configured filter.
//
// Paged, because a directory of any size refuses an unpaged search rather
// than truncating — AD's default MaxPageSize is 1000, and a sync that
// silently received the first thousand accounts would deactivate everybody
// else as vanished. That is the single worst thing this code could do, so
// paging is not an optimization here.
func (c *Client) Users() ([]Entry, []error, error) {
	attributes := []string{"dn"}
	for _, name := range []string{
		c.cfg.Attributes.Username,
		c.cfg.Attributes.DisplayName,
		c.cfg.Attributes.Email,
		c.cfg.Attributes.Phone,
		c.cfg.Attributes.ExternalID,
	} {
		if name != "" {
			attributes = append(attributes, name)
		}
	}

	request := ldap.NewSearchRequest(
		c.cfg.BaseDN,
		ldap.ScopeWholeSubtree, ldap.NeverDerefAliases,
		0, int(c.cfg.Timeout.Seconds()), false,
		c.cfg.UserFilter,
		attributes,
		nil,
	)

	result, err := c.conn.SearchWithPaging(request, 500)
	if err != nil {
		return nil, nil, fmt.Errorf("search %q: %w", c.cfg.BaseDN, err)
	}

	entries := make([]Entry, 0, len(result.Entries))
	var skipped []error
	for _, raw := range result.Entries {
		entry, err := c.mapEntry(raw)
		if err != nil {
			// Collected rather than returned: one malformed entry in ten
			// thousand must not stop the other nine thousand nine hundred,
			// and the count of what was skipped belongs in the run record.
			skipped = append(skipped, fmt.Errorf("%s: %w", raw.DN, err))
			continue
		}
		entries = append(entries, entry)
	}
	return entries, skipped, nil
}

func (c *Client) mapEntry(raw *ldap.Entry) (Entry, error) {
	attrs := c.cfg.Attributes

	externalID, err := canonicalID(raw, attrs.ExternalID)
	if err != nil {
		return Entry{}, err
	}

	username := strings.TrimSpace(raw.GetAttributeValue(attrs.Username))
	if username == "" {
		return Entry{}, ErrNoUsername
	}

	displayName := strings.TrimSpace(raw.GetAttributeValue(attrs.DisplayName))
	if displayName == "" {
		// Not an error. A directory entry with no display name is a real
		// thing, and refusing it would drop an account over cosmetics.
		displayName = username
	}

	entry := Entry{
		DN:          raw.DN,
		Username:    username,
		DisplayName: displayName,
		ExternalID:  externalID,
	}
	if attrs.Email != "" {
		entry.Email = strings.TrimSpace(raw.GetAttributeValue(attrs.Email))
	}
	if attrs.Phone != "" {
		entry.Phone = strings.TrimSpace(raw.GetAttributeValue(attrs.Phone))
	}
	return entry, nil
}

// canonicalID reads the identifying attribute and renders it as stable text.
//
// This is the function to get right, because everything downstream keys on
// what it returns and a value that changes shape between releases silently
// duplicates every account.
//
// **objectGUID is binary.** Active Directory returns sixteen raw bytes, not
// a string. Reading it as text produces mojibake that varies with how the
// bytes happen to decode, and it goes straight into the reconciliation key —
// so a rename would become a second account, which is exactly what this
// feature exists to prevent, and it would happen silently. The bytes are
// therefore rendered in Microsoft's own mixed-endian GUID form, which is
// what every other tool an operator might use will show them.
//
// entryUUID, the OpenLDAP equivalent, is already printable text and passes
// through unchanged.
func canonicalID(raw *ldap.Entry, attribute string) (string, error) {
	values := raw.GetRawAttributeValues(attribute)
	if len(values) == 0 || len(values[0]) == 0 {
		return "", ErrNoExternalID
	}
	value := values[0]

	if len(value) == 16 && !printable(value) {
		return formatGUID(value), nil
	}

	text := strings.TrimSpace(string(value))
	if text == "" {
		return "", ErrNoExternalID
	}
	return text, nil
}

// printable reports whether every byte could plausibly be text, which is how
// a 16-byte identifier that happens to be a string is told apart from a
// binary GUID.
func printable(value []byte) bool {
	for _, b := range value {
		if b < 0x20 || b > 0x7e {
			return false
		}
	}
	return true
}

// formatGUID renders AD's objectGUID the way Microsoft's own tooling does.
//
// The first three groups are little-endian and the last two are not — an
// inconsistency in the format itself, not in this code. Rendering all five
// the same way would produce an identifier that matches nothing an operator
// can look up, which is worse than useless when they are trying to work out
// why an account did not sync.
func formatGUID(b []byte) string {
	return fmt.Sprintf("%02x%02x%02x%02x-%02x%02x-%02x%02x-%02x%02x-%02x%02x%02x%02x%02x%02x",
		b[3], b[2], b[1], b[0],
		b[5], b[4],
		b[7], b[6],
		b[8], b[9],
		b[10], b[11], b[12], b[13], b[14], b[15],
	)
}
