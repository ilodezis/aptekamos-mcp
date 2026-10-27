package oauth

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestOAuthFlowAndBearerGate(t *testing.T) {
	const (
		callback = "https://claude.ai/api/mcp/auth_callback"
		password = "pw"
		secret   = "ssssssssssssssssssssssssssssssss"
		cs       = "cs"
		clientID = "aptekamos-claude-connector"
	)
	verifier := strings.Repeat("v", 50)
	sha := sha256.Sum256([]byte(verifier))
	challenge := b64Encode(sha[:])

	server, err := NewServer(Config{
		Password:     password,
		TokenSecret:  secret,
		ClientID:     clientID,
		ClientSecret: cs,
		RedirectURIs: []string{callback},
	})
	if err != nil {
		t.Fatalf("NewServer error: %v", err)
	}

	// Create test HTTP multiplexer
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/oauth-authorization-server", server.HandleMetadata)
	mux.HandleFunc("/oauth/authorize", server.HandleAuthorize)
	mux.HandleFunc("/oauth/token", server.HandleToken)
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	})

	handler := server.BearerGate(mux)

	// Helper to send request with Host: aptekamos.example.com
	doReq := func(req *http.Request) *httptest.ResponseRecorder {
		req.Host = "aptekamos.example.com"
		req.Header.Set("X-Forwarded-Proto", "https")
		req.Header.Set("X-Forwarded-Host", "aptekamos.example.com")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	// 1. Metadata endpoint
	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil)
	rec := doReq(req)
	if rec.Code != http.StatusOK {
		t.Fatalf("metadata status = %d, want 200", rec.Code)
	}
	var meta map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &meta)
	if meta["token_endpoint"] != "https://aptekamos.example.com/oauth/token" {
		t.Errorf("token_endpoint = %v, want https://aptekamos.example.com/oauth/token", meta["token_endpoint"])
	}

	// 2. Authorize validation
	baseAuth := url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {callback},
		"state":                 {"st"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}

	// Evil redirect uri
	badAuth := url.Values{}
	for k, v := range baseAuth {
		badAuth[k] = v
	}
	badAuth.Set("redirect_uri", "https://evil.example/cb")
	rec = doReq(httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+badAuth.Encode(), nil))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("evil redirect_uri status = %d, want 400", rec.Code)
	}

	// Empty code_challenge
	badChallenge := url.Values{}
	for k, v := range baseAuth {
		badChallenge[k] = v
	}
	badChallenge.Set("code_challenge", "")
	rec = doReq(httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+badChallenge.Encode(), nil))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty code_challenge status = %d, want 400", rec.Code)
	}

	// Valid GET authorize
	rec = doReq(httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+baseAuth.Encode(), nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "claude.ai") {
		t.Errorf("valid GET authorize code = %d, body contains claude.ai = %v", rec.Code, strings.Contains(rec.Body.String(), "claude.ai"))
	}

	// Wrong password POST
	wrongPw := url.Values{}
	for k, v := range baseAuth {
		wrongPw[k] = v
	}
	wrongPw.Set("password", "nope")
	postReq := httptest.NewRequest(http.MethodPost, "/oauth/authorize", strings.NewReader(wrongPw.Encode()))
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = doReq(postReq)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong password status = %d, want 401", rec.Code)
	}

	// Helper to get code from POST authorize
	getCode := func() string {
		pwVals := url.Values{}
		for k, v := range baseAuth {
			pwVals[k] = v
		}
		pwVals.Set("password", password)
		pReq := httptest.NewRequest(http.MethodPost, "/oauth/authorize", strings.NewReader(pwVals.Encode()))
		pReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		res := doReq(pReq)
		if res.Code != http.StatusSeeOther {
			t.Fatalf("expected 303 See Other, got %d", res.Code)
		}
		loc, _ := url.Parse(res.Header().Get("Location"))
		q := loc.Query()
		if q.Get("state") != "st" {
			t.Errorf("expected state=st, got %s", q.Get("state"))
		}
		return q.Get("code")
	}

	code := getCode()
	if code == "" {
		t.Fatalf("code was empty")
	}

	// Token request with wrong secret
	tokenVals := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {callback},
		"client_id":     {clientID},
		"client_secret": {"bad"},
		"code_verifier": {verifier},
	}
	tReq := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(tokenVals.Encode()))
	tReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = doReq(tReq)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("bad client_secret status = %d, want 401", rec.Code)
	}

	// Basic auth token exchange with fresh code
	code = getCode()
	tokenVals.Set("code", code)
	tokenVals.Del("client_id")
	tokenVals.Del("client_secret")
	basicAuth := base64.StdEncoding.EncodeToString([]byte(clientID + ":" + cs))

	tReq = httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(tokenVals.Encode()))
	tReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tReq.Header.Set("Authorization", "Basic "+basicAuth)
	rec = doReq(tReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("token exchange status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
	var tokenResp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &tokenResp)
	token, ok := tokenResp["access_token"].(string)
	if !ok || token == "" {
		t.Fatalf("no access_token in response: %v", tokenResp)
	}

	// Same code cannot be used again
	tReq = httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(tokenVals.Encode()))
	tReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tReq.Header.Set("Authorization", "Basic "+basicAuth)
	rec = doReq(tReq)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("reused code status = %d, want 400", rec.Code)
	}

	// Wrong verifier
	badVerifierVals := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {getCode()},
		"redirect_uri":  {callback},
		"client_id":     {clientID},
		"client_secret": {cs},
		"code_verifier": {strings.Repeat("x", 50)},
	}
	tReq = httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(badVerifierVals.Encode()))
	tReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = doReq(tReq)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("wrong verifier status = %d, want 400", rec.Code)
	}

	// Bearer gate tests
	checkMCP := func(bearer string) int {
		mReq := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{}`))
		mReq.Header.Set("Accept", "application/json, text/event-stream")
		if bearer != "" {
			mReq.Header.Set("Authorization", "Bearer "+bearer)
		}
		return doReq(mReq).Code
	}

	if status := checkMCP(""); status != http.StatusUnauthorized {
		t.Errorf("MCP no token status = %d, want 401", status)
	}
	if status := checkMCP(token[:len(token)-2] + "xx"); status != http.StatusUnauthorized {
		t.Errorf("MCP tampered token status = %d, want 401", status)
	}
	expiredToken := server.SignToken(-1)
	if status := checkMCP(expiredToken); status != http.StatusUnauthorized {
		t.Errorf("MCP expired token status = %d, want 401", status)
	}
	if status := checkMCP(token); status != http.StatusOK {
		t.Errorf("MCP valid token status = %d, want 200", status)
	}
}
