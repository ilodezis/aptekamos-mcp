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
	"time"
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

func TestProtectedResourceMetadataAndChallenge(t *testing.T) {
	srv, err := NewServer(Config{
		Password:     "pw",
		TokenSecret:  "ssssssssssssssssssssssssssssssss",
		ClientSecret: "cs",
		PublicURL:    "https://aptekamos.example.com/",
	})
	if err != nil {
		t.Fatalf("NewServer error: %v", err)
	}
	mux := http.NewServeMux()
	srv.Register(mux)
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	h := srv.BearerGate(mux)

	for _, path := range []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/mcp"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("X-Forwarded-Host", "evil.example")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200", path, rec.Code)
		}
		var out struct {
			Resource string   `json:"resource"`
			Servers  []string `json:"authorization_servers"`
			Methods  []string `json:"bearer_methods_supported"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if out.Resource != "https://aptekamos.example.com/mcp" ||
			len(out.Servers) != 1 || out.Servers[0] != "https://aptekamos.example.com" ||
			len(out.Methods) != 1 || out.Methods[0] != "header" {
			t.Errorf("%s got %+v", path, out)
		}
	}

	// Metadata uses PublicURL and ignores X-Forwarded-Host.
	metaReq := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil)
	metaReq.Header.Set("X-Forwarded-Host", "evil.example")
	metaRec := httptest.NewRecorder()
	h.ServeHTTP(metaRec, metaReq)
	if !strings.Contains(metaRec.Body.String(), "https://aptekamos.example.com/oauth/token") || strings.Contains(metaRec.Body.String(), "evil") {
		t.Errorf("metadata body = %s", metaRec.Body.String())
	}

	// 401 on /mcp carries RFC 9728 WWW-Authenticate resource_metadata.
	mcpReq := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{}`))
	mcpReq.Header.Set("X-Forwarded-Host", "evil.example")
	mcpRec := httptest.NewRecorder()
	h.ServeHTTP(mcpRec, mcpReq)
	if mcpRec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", mcpRec.Code)
	}
	wantChallenge := `Bearer resource_metadata="https://aptekamos.example.com/.well-known/oauth-protected-resource/mcp"`
	if got := mcpRec.Header().Get("WWW-Authenticate"); got != wantChallenge {
		t.Errorf("WWW-Authenticate = %q, want %q", got, wantChallenge)
	}

	// PublicURL must start with https:// when set.
	if _, err := NewServer(Config{
		Password: "pw", TokenSecret: "ssssssssssssssssssssssssssssssss", ClientSecret: "cs",
		PublicURL: "http://aptekamos.example.com",
	}); err == nil {
		t.Error("expected error for non-https PUBLIC_URL")
	}
}

func TestDefaultRedirectURIsIncludeClaudeComAndClaudeAi(t *testing.T) {
	srv, err := NewServer(Config{
		Password:     "pw",
		TokenSecret:  "ssssssssssssssssssssssssssssssss",
		ClientSecret: "cs",
	})
	if err != nil {
		t.Fatal(err)
	}
	verifier := strings.Repeat("v", 50)
	sha := sha256.Sum256([]byte(verifier))
	challenge := b64Encode(sha[:])

	for _, cb := range []string{
		"https://claude.ai/api/mcp/auth_callback",
		"https://claude.com/api/mcp/auth_callback",
	} {
		q := url.Values{
			"response_type":         {"code"},
			"client_id":             {DefaultClientID},
			"redirect_uri":          {cb},
			"code_challenge":        {challenge},
			"code_challenge_method": {"S256"},
		}
		rec := httptest.NewRecorder()
		srv.HandleAuthorize(rec, httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+q.Encode(), nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s status = %d, want 200", cb, rec.Code)
		}
	}
}

func TestLockoutSlidingWindow(t *testing.T) {
	srv, err := NewServer(Config{
		Password:     "correct-password",
		TokenSecret:  "ssssssssssssssssssssssssssssssss",
		ClientSecret: "cs",
	})
	if err != nil {
		t.Fatal(err)
	}
	verifier := strings.Repeat("v", 50)
	sha := sha256.Sum256([]byte(verifier))
	challenge := b64Encode(sha[:])

	postAuth := func(pw string) *httptest.ResponseRecorder {
		v := url.Values{
			"response_type":         {"code"},
			"client_id":             {DefaultClientID},
			"redirect_uri":          {DefaultCallbackURI},
			"code_challenge":        {challenge},
			"code_challenge_method": {"S256"},
			"password":              {pw},
		}
		req := httptest.NewRequest(http.MethodPost, "/oauth/authorize", strings.NewReader(v.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		srv.HandleAuthorize(rec, req)
		return rec
	}

	for i := 0; i < maxFailures; i++ {
		if rec := postAuth("wrong"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, want 401", i+1, rec.Code)
		}
	}

	// Even the right password is locked out (429) once maxFailures is reached.
	rec := postAuth("correct-password")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("locked out status = %d, want 429", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Блокировка") {
		t.Errorf("lockout page missing stamp: %s", rec.Body.String())
	}

	// Aging failures outside lockWindow clears the lockout.
	srv.mu.Lock()
	old := time.Now().Add(-lockWindow - time.Second)
	for i := range srv.fails {
		srv.fails[i] = old
	}
	srv.mu.Unlock()

	if rec := postAuth("correct-password"); rec.Code != http.StatusSeeOther {
		t.Errorf("after window expiry status = %d, want 303", rec.Code)
	}
}

func TestPKCEVerifierLengthAndMaxBody(t *testing.T) {
	srv, err := NewServer(Config{
		Password:     "pw",
		TokenSecret:  "ssssssssssssssssssssssssssssssss",
		ClientSecret: "cs",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Reject code_verifier shorter than 43 or longer than 128 even when challenge matches its SHA-256.
	for _, vLen := range []int{42, 129} {
		verifier := strings.Repeat("a", vLen)
		sha := sha256.Sum256([]byte(verifier))
		challenge := b64Encode(sha[:])

		authVals := url.Values{
			"response_type":         {"code"},
			"client_id":             {DefaultClientID},
			"redirect_uri":          {DefaultCallbackURI},
			"code_challenge":        {challenge},
			"code_challenge_method": {"S256"},
			"password":              {"pw"},
		}
		aReq := httptest.NewRequest(http.MethodPost, "/oauth/authorize", strings.NewReader(authVals.Encode()))
		aReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		aRec := httptest.NewRecorder()
		srv.HandleAuthorize(aRec, aReq)
		if aRec.Code != http.StatusSeeOther {
			t.Fatalf("len %d authorize = %d", vLen, aRec.Code)
		}
		loc, _ := url.Parse(aRec.Header().Get("Location"))
		code := loc.Query().Get("code")

		tokVals := url.Values{
			"grant_type":    {"authorization_code"},
			"code":          {code},
			"redirect_uri":  {DefaultCallbackURI},
			"client_id":     {DefaultClientID},
			"client_secret": {"cs"},
			"code_verifier": {verifier},
		}
		tReq := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(tokVals.Encode()))
		tReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		tRec := httptest.NewRecorder()
		srv.HandleToken(tRec, tReq)
		if tRec.Code != http.StatusBadRequest || !strings.Contains(tRec.Body.String(), "invalid_grant") {
			t.Errorf("verifier len %d: status=%d body=%s, want 400 invalid_grant", vLen, tRec.Code, tRec.Body.String())
		}
	}

	// MaxBytesReader rejects > 64 KiB bodies on /oauth/authorize and /oauth/token.
	huge := strings.Repeat("x", maxBody+1024)
	for _, ep := range []struct {
		path string
		fn   http.HandlerFunc
	}{
		{"/oauth/authorize", srv.HandleAuthorize},
		{"/oauth/token", srv.HandleToken},
	} {
		req := httptest.NewRequest(http.MethodPost, ep.path, strings.NewReader("data="+huge))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		ep.fn(rec, req)
		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Errorf("%s oversized body status = %d, want 413", ep.path, rec.Code)
		}
	}
}
