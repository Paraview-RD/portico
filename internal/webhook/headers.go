package webhook

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// Custom headers a subscription sends with every delivery.
//
// The case they exist for is a receiver behind an API gateway that refuses
// anything without an Authorization header of its own. The signature says
// who produced the body; the gateway is deciding whether to let the request
// through at all. Those are different questions, and the signature cannot
// answer the second.

// MaxHeaders bounds how many a subscription may carry.
//
// Not a storage concern — it is that every one of these is sent on every
// delivery, so an unbounded set is a way to make this server generate
// arbitrarily large requests to somebody else's endpoint on a schedule.
const MaxHeaders = 10

// MaxHeaderValueLength bounds one value, for the same reason.
const MaxHeaderValueLength = 2048

// reservedHeaders may not be set by a subscription.
//
// The signature headers are the obvious ones: allowing a subscription to
// supply X-Portico-Signature would let whoever registers it choose what the
// receiver verifies, which is the whole thing inverted. Content-Type and
// Content-Length are refused because the body is JSON and the transport
// decides its length — a subscription overriding either produces a request
// that disagrees with itself, and the receiver is the one who has to work
// out why.
//
// Host is refused because it decides which virtual host at the far end
// receives the delivery, which is a routing decision the registered URL has
// already made.
var reservedHeaders = map[string]bool{
	strings.ToLower(HeaderSignature): true,
	strings.ToLower(HeaderTimestamp): true,
	strings.ToLower(HeaderEvent):     true,
	strings.ToLower(HeaderDelivery):  true,
	"content-type":                   true,
	"content-length":                 true,
	"host":                           true,
	"user-agent":                     true,
}

// ValidateHeaders checks a set before it is stored.
//
// The name rules are http.Header's own, deliberately: a name containing a
// colon, a space, or a newline is how a header value becomes two headers, or
// becomes a body. Go's http client would reject most of these at send time,
// but that failure arrives on a schedule, in a delivery record, long after
// whoever typed it has gone — so it is refused at the point of typing.
func ValidateHeaders(headers map[string]string) error {
	if len(headers) > MaxHeaders {
		return fmt.Errorf("at most %d headers", MaxHeaders)
	}

	for name, value := range headers {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			return fmt.Errorf("a header name cannot be empty")
		}
		// http.CanonicalHeaderKey is not a validator — it returns its input
		// unchanged when it cannot canonicalize it, which is precisely the
		// case worth catching. The token check is what actually rejects.
		if !validHeaderName(trimmed) {
			return fmt.Errorf("%q is not a valid header name", name)
		}
		if reservedHeaders[strings.ToLower(trimmed)] {
			return fmt.Errorf("%s is set by Portico and cannot be overridden", trimmed)
		}
		if len(value) > MaxHeaderValueLength {
			return fmt.Errorf("the value for %s is longer than %d characters",
				trimmed, MaxHeaderValueLength)
		}
		// A newline in a value is header injection: everything after it is
		// read by the receiver as a header of its own, or as the body.
		if strings.ContainsAny(value, "\r\n\x00") {
			return fmt.Errorf("the value for %s contains a line break", trimmed)
		}
	}
	return nil
}

// validHeaderName reports whether every byte is an RFC 7230 token character.
func validHeaderName(name string) bool {
	for i := 0; i < len(name); i++ {
		if !isTokenByte(name[i]) {
			return false
		}
	}
	return true
}

func isTokenByte(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	}
	return strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0
}

// EncodeHeaders renders a set for storage. Empty in, empty out — so a
// subscription with none stores an empty string rather than "{}", and the
// column reads as absent rather than as a set that happens to be empty.
func EncodeHeaders(headers map[string]string) (string, error) {
	if len(headers) == 0 {
		return "", nil
	}
	encoded, err := json.Marshal(headers)
	if err != nil {
		return "", fmt.Errorf("encode headers: %w", err)
	}
	return string(encoded), nil
}

// DecodeHeaders parses what EncodeHeaders wrote.
func DecodeHeaders(encoded string) (map[string]string, error) {
	if strings.TrimSpace(encoded) == "" {
		return nil, nil
	}
	var headers map[string]string
	if err := json.Unmarshal([]byte(encoded), &headers); err != nil {
		return nil, fmt.Errorf("decode headers: %w", err)
	}
	return headers, nil
}

// HeaderNames returns the names alone, sorted.
//
// What the console and the API report, because the values are credentials
// and are never served back. A name is enough to answer "what is this
// subscription sending", which is the question somebody asks; the value is
// only ever answered by whoever typed it.
func HeaderNames(headers map[string]string) []string {
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// applyHeaders sets a subscription's own headers on a request.
//
// Before the ones Portico sets, never after: an ordering that let a stored
// header land last would make the reserved list above the only thing
// standing between a subscription and the signature it is verified by, and
// a list is a weaker guarantee than an order.
func applyHeaders(req *http.Request, headers map[string]string) {
	for name, value := range headers {
		req.Header.Set(name, value)
	}
}
