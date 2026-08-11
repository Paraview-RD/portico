package server_test

// Custom headers on a subscription, end to end.
//
// Two properties, and both are about the value being a credential rather
// than a setting. It is never served back, and it is never stored where a
// database dump would hand it over.

import (
	"database/sql"
	"net/http"
	"strings"
	"testing"
)

func createWebhookWithHeaders(t *testing.T, api *apiTest, token, name string,
	headers map[string]string) response {
	t.Helper()
	return api.do(http.MethodPost, "/api/v1/webhooks", token, map[string]any{
		"name":    name,
		"url":     "https://hooks.example.com/" + name,
		"events":  []string{"user.created"},
		"headers": headers,
	})
}

func TestACustomHeaderIsSealedAndNeverServedBack(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	const token = "Bearer super-secret-gateway-token"
	res := createWebhookWithHeaders(t, api, admin, "gateway",
		map[string]string{"Authorization": token})
	if res.Status != http.StatusOK {
		t.Fatalf("create: %d %s %s", res.Status, res.Code, res.Message)
	}

	// Not in the creation response beyond its name.
	if strings.Contains(string(res.Data), token) {
		t.Error("the header value came back in the creation response")
	}

	var listed []struct {
		Name        string   `json:"name"`
		HeaderNames []string `json:"headerNames"`
	}
	list := api.do(http.MethodGet, "/api/v1/webhooks", admin, nil)
	list.into(t, &listed)
	if strings.Contains(string(list.Data), token) {
		t.Fatal("the header value is served by the listing, which makes this " +
			"endpoint a way to read every credential the tenant has stored")
	}

	// The name is reported, because "what is this subscription sending" is a
	// question an operator asks and can be answered without the value.
	var found bool
	for _, sub := range listed {
		if sub.Name == "gateway" {
			found = true
			if len(sub.HeaderNames) != 1 || sub.HeaderNames[0] != "Authorization" {
				t.Errorf("headerNames = %v, want [Authorization]", sub.HeaderNames)
			}
		}
	}
	if !found {
		t.Fatal("the subscription is missing from the listing")
	}

	// And not in the table either, which is the property sealing it buys:
	// a dump taken for support does not carry somebody's gateway token.
	db, err := sql.Open("pgx", api.dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = db.Close() }()

	var stored string
	if err := db.QueryRow(
		`SELECT headers FROM webhook_subscriptions WHERE name = 'gateway'`).
		Scan(&stored); err != nil {
		t.Fatalf("read stored headers: %v", err)
	}
	if stored == "" {
		t.Fatal("nothing was stored, so the header would never be sent")
	}
	if strings.Contains(stored, token) {
		t.Errorf("the token is in the table in the clear: %q", stored)
	}
	if strings.Contains(stored, "Authorization") {
		t.Error("the header name is readable in the table, so the whole set " +
			"is not inside the sealed value")
	}
}

func TestAHeaderThatWouldForgeASignatureIsRefused(t *testing.T) {
	api := newAPITest(t)
	admin := api.adminToken()

	for _, headers := range []map[string]string{
		{"X-Portico-Signature": "sha256=forged"},
		{"x-portico-signature": "sha256=forged"},
		{"Authorization": "Bearer x\r\nX-Portico-Event: user.disabled"},
		{"Bad Name": "x"},
	} {
		res := createWebhookWithHeaders(t, api, admin, "refused", headers)
		if res.Status == http.StatusOK {
			t.Errorf("%v was accepted", headers)
		}
		if res.Code != "INVALID_WEBHOOK_HEADER" {
			t.Errorf("%v gave %s, want INVALID_WEBHOOK_HEADER", headers, res.Code)
		}
	}
}

// The deployment with no PORTICO_ENCRYPTION_KEY. Saving a header there is
// refused rather than written in the clear — the same rule a directory bind
// password follows, and for the same reason: a plaintext bearer token in a
// table is worse than a feature somebody cannot use.
func TestWithoutAnEncryptionKeyAHeaderIsRefusedRatherThanStoredInTheClear(t *testing.T) {
	cfg := testConfig(t)
	cfg.EncryptionKey = nil
	api := newAPITestWithConfig(t, cfg)
	admin := api.adminToken()

	res := createWebhookWithHeaders(t, api, admin, "no-key",
		map[string]string{"Authorization": "Bearer would-be-plaintext"})
	if res.Status == http.StatusOK {
		t.Fatal("accepted with no encryption key configured; the token would " +
			"have gone into the table in the clear")
	}
	if res.Code != "NO_ENCRYPTION_KEY" {
		t.Errorf("code = %s, want NO_ENCRYPTION_KEY", res.Code)
	}

	// A subscription without headers still works there. Refusing the whole
	// feature would make the encryption key a requirement for webhooks,
	// which it is not.
	plain := createWebhookWithHeaders(t, api, admin, "no-headers", nil)
	if plain.Status != http.StatusOK {
		t.Errorf("a subscription with no headers was refused too: %d %s",
			plain.Status, plain.Code)
	}
}
