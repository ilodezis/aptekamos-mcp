package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ilodezis/aptekamos-mcp/internal/oauth"
)

const testSecret = "ssssssssssssssssssssssssssssssss"

func gateStatus(t *testing.T, secret, token string) int {
	t.Helper()
	srv, err := oauth.NewServer(oauth.Config{Password: "pw", TokenSecret: secret, ClientSecret: "cs"})
	if err != nil {
		t.Fatal(err)
	}
	h := srv.BearerGate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestSignTokenIsAcceptedByTheHTTPServer(t *testing.T) {
	var out bytes.Buffer
	if err := signTokenCmd(nil, env(map[string]string{"TOKEN_SECRET": testSecret}), &out); err != nil {
		t.Fatal(err)
	}
	token := strings.TrimSpace(out.String())
	if got := gateStatus(t, testSecret, token); got != http.StatusOK {
		t.Errorf("issued token status = %d, want 200", got)
	}
	if got := gateStatus(t, "another-secret-another-secret-12", token); got != http.StatusUnauthorized {
		t.Errorf("token under a different secret status = %d, want 401", got)
	}
}

func TestSignTokenDaysAndClientID(t *testing.T) {
	var out bytes.Buffer
	e := env(map[string]string{"TOKEN_SECRET": testSecret, "CLIENT_ID": "custom"})
	if err := signTokenCmd([]string{"--days", "30"}, e, &out); err != nil {
		t.Fatal(err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.Split(strings.TrimSpace(out.String()), ".")[0])
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		ClientID string `json:"client_id"`
		Exp      int64  `json:"exp"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	if p.ClientID != "custom" {
		t.Errorf("client_id = %q, want custom", p.ClientID)
	}
	if want := time.Now().Add(30 * 24 * time.Hour).Unix(); p.Exp < want-5 || p.Exp > want+5 {
		t.Errorf("exp = %d, want about %d", p.Exp, want)
	}
}

func TestSignTokenRejectsBadInput(t *testing.T) {
	good := env(map[string]string{"TOKEN_SECRET": testSecret})
	for name, c := range map[string]struct {
		args []string
		env  func(string) string
	}{
		"no secret":     {nil, env(nil)},
		"zero days":     {[]string{"--days", "0"}, good},
		"negative days": {[]string{"--days", "-1"}, good},
		"extra arg":     {[]string{"oops"}, good},
		"unknown flag":  {[]string{"--nope"}, good},
	} {
		var out bytes.Buffer
		if err := signTokenCmd(c.args, c.env, &out); err == nil {
			t.Errorf("%s: expected an error", name)
		}
		if out.Len() != 0 {
			t.Errorf("%s: nothing should be printed on error", name)
		}
	}
}
