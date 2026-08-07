package oidcp

import (
	"encoding/json"
	"net/http"

	"github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/zitadel/oidc/v3/pkg/op"
)

// DiscoveryPath is where the specification puts the document that describes
// a provider, relative to its issuer. Every relying party reads it at
// start-up, and reads nothing else.
const DiscoveryPath = "/.well-known/openid-configuration"

// serveCorrectedDiscovery publishes a discovery document that describes this
// server rather than the protocol library's defaults.
//
// Three of the fields the library fills in are not configurable and are not
// true here. It lists the implicit flow's response types and the implicit
// grant, both of which OAuth 2.1 removes and this server refuses; it lists
// the JWT-bearer grant, whose storage method returns an error; and it
// advertises a device-authorization endpoint that is not mounted.
//
// A discovery document is a contract read by machines, once, before anybody
// is watching. A client that believes any of those and configures itself
// accordingly fails later, somewhere else, with an error nobody can trace
// back to a JSON field it never saw.
func serveCorrectedDiscovery(w http.ResponseWriter, r *http.Request, provider *op.Provider) {
	captured := &capturingWriter{header: http.Header{}, status: http.StatusOK}
	provider.ServeHTTP(captured, r)

	var config oidc.DiscoveryConfiguration
	if captured.status != http.StatusOK || json.Unmarshal(captured.body, &config) != nil {
		// Something other than the document we expected. Pass it through
		// rather than replacing an error with a silence.
		captured.flushTo(w)
		return
	}

	// The authorization code flow, and only it.
	config.ResponseTypesSupported = []string{string(oidc.ResponseTypeCode)}
	config.GrantTypesSupported = []oidc.GrantType{
		oidc.GrantTypeCode,
		oidc.GrantTypeRefreshToken,
	}
	// Not implemented, and omitempty drops the field entirely rather than
	// publishing an empty string somebody's client would try to POST to.
	config.DeviceAuthorizationEndpoint = ""

	body, err := json.Marshal(&config)
	if err != nil {
		http.Error(w, "could not build the discovery document", http.StatusInternalServerError)
		return
	}

	for name, values := range captured.header {
		w.Header()[name] = values
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Del("Content-Length")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// capturingWriter buffers a response so it can be rewritten. The document is
// a few kilobytes and is produced once per request, so buffering it costs
// nothing worth measuring.
type capturingWriter struct {
	header http.Header
	body   []byte
	status int
}

func (c *capturingWriter) Header() http.Header { return c.header }

func (c *capturingWriter) Write(p []byte) (int, error) {
	c.body = append(c.body, p...)
	return len(p), nil
}

func (c *capturingWriter) WriteHeader(status int) { c.status = status }

func (c *capturingWriter) flushTo(w http.ResponseWriter) {
	for name, values := range c.header {
		w.Header()[name] = values
	}
	w.WriteHeader(c.status)
	_, _ = w.Write(c.body)
}
