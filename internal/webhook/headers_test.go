package webhook

// What a subscription may and may not put in a request.
//
// These are the rules that stop a custom header being a way to change what
// the delivery is, rather than a way to get past somebody's gateway. Every
// case below is refused at registration, which is where somebody can still
// see the message — the alternative is a delivery record hours later saying
// the receiver answered 400.

import (
	ctx "context"
	"net/http"
	"strings"
	"testing"
)

func TestAHeaderCannotOverrideTheSignature(t *testing.T) {
	for _, name := range []string{
		HeaderSignature, HeaderTimestamp, HeaderEvent, HeaderDelivery,
		// Case is not a defence: header names are case-insensitive, and a
		// check that compared them literally would be bypassed by typing it
		// differently.
		strings.ToLower(HeaderSignature),
		strings.ToUpper(HeaderSignature),
	} {
		err := ValidateHeaders(map[string]string{name: "forged"})
		if err == nil {
			t.Errorf("%s was accepted; whoever registers a subscription could "+
				"then choose what its receiver verifies", name)
		}
	}
}

func TestAHeaderCannotChangeWhatTheBodyIs(t *testing.T) {
	for _, name := range []string{"Content-Type", "Content-Length", "Host", "User-Agent"} {
		if err := ValidateHeaders(map[string]string{name: "x"}); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// The one that turns a value into a second header, or into a body.
func TestAValueCannotSmuggleALineBreak(t *testing.T) {
	for _, value := range []string{
		"Bearer x\r\nX-Portico-Signature: forged",
		"Bearer x\nX-Portico-Event: user.disabled",
		"Bearer x\r\n\r\n{\"type\":\"user.disabled\"}",
		"Bearer \x00null",
	} {
		if err := ValidateHeaders(map[string]string{"Authorization": value}); err == nil {
			t.Errorf("a value containing a line break was accepted: %q", value)
		}
	}
}

func TestAnInvalidNameIsRefused(t *testing.T) {
	for _, name := range []string{
		"X-Bad Name", "X-Bad:Name", "X-Bad\nName", "", "  ", "X-Bad\tName",
	} {
		if err := ValidateHeaders(map[string]string{name: "x"}); err == nil {
			t.Errorf("%q was accepted as a header name", name)
		}
	}
}

func TestAnOrdinaryHeaderIsAccepted(t *testing.T) {
	err := ValidateHeaders(map[string]string{
		"Authorization": "Bearer abc123",
		"X-Tenant-Key":  "acme",
	})
	if err != nil {
		t.Errorf("a perfectly normal pair was refused: %v", err)
	}
}

func TestTheSetIsBounded(t *testing.T) {
	headers := map[string]string{}
	for i := 0; i <= MaxHeaders; i++ {
		headers["X-Custom-"+string(rune('a'+i))] = "x"
	}
	if err := ValidateHeaders(headers); err == nil {
		t.Error("an unbounded set was accepted, which is a way to make this " +
			"server send arbitrarily large requests to somebody on a timer")
	}

	long := map[string]string{"X-Big": strings.Repeat("x", MaxHeaderValueLength+1)}
	if err := ValidateHeaders(long); err == nil {
		t.Error("an unbounded value was accepted")
	}
}

// The order is the other half, and the half that does not depend on the
// reserved list being complete. Even if a name slipped past validation, the
// subscription's headers are applied first and overwritten.
func TestPorticoHeadersWinRegardlessOfWhatWasStored(t *testing.T) {
	stored := map[string]string{
		// Not reachable through ValidateHeaders — which is the point. This is
		// what happens if it ever became reachable.
		HeaderSignature: "sha256=forged",
		"Authorization": "Bearer real",
	}

	result := Deliver(ctx.Background(), refusingClient(), "https://receiver.example.test",
		[]string{"whsec_test"}, stored, EventUserCreated, "delivery-1", []byte("{}"))
	_ = result

	req := lastRequest
	if req == nil {
		t.Fatal("no request was built")
	}
	if got := req.Header.Get(HeaderSignature); got == "sha256=forged" {
		t.Error("a stored header overrode the signature, so the order the " +
			"headers are applied in is wrong and the reserved list is the " +
			"only thing standing between a subscription and its own signature")
	}
	if got := req.Header.Get("Authorization"); got != "Bearer real" {
		t.Errorf("the subscription's own header did not survive: %q", got)
	}
}

// A transport that records the request and refuses to send it. Deliver's
// result is not under test here; what it built is.
var lastRequest *http.Request

type recordingTransport struct{}

func (recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	lastRequest = req
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       http.NoBody,
		Header:     http.Header{},
		Request:    req,
	}, nil
}

func refusingClient() *http.Client {
	return &http.Client{Transport: recordingTransport{}}
}
