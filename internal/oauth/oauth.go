package oauth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	DefaultClientID    = "aptekamos-claude-connector"
	DefaultCallbackURI = "https://claude.ai/api/mcp/auth_callback"
	DefaultTokenTTL    = 365 * 24 * 3600
	DefaultCodeTTL     = 300
)

type Config struct {
	Password     string
	TokenSecret  string
	ClientID     string
	ClientSecret string
	RedirectURIs []string
	TokenTTL     int64
	CodeTTL      int64
}

type codeEntry struct {
	expiresAt     int64
	redirectURI   string
	codeChallenge string
}

type Server struct {
	cfg          Config
	redirectURIs map[string]bool
	mu           sync.Mutex
	codes        map[string]codeEntry
}

func NewServer(cfg Config) (*Server, error) {
	if cfg.Password == "" {
		return nil, fmt.Errorf("PASSWORD is required")
	}
	if cfg.TokenSecret == "" {
		return nil, fmt.Errorf("TOKEN_SECRET is required")
	}
	if cfg.ClientSecret == "" {
		return nil, fmt.Errorf("CLIENT_SECRET is required")
	}
	if cfg.ClientID == "" {
		cfg.ClientID = DefaultClientID
	}
	if cfg.TokenTTL <= 0 {
		cfg.TokenTTL = DefaultTokenTTL
	}
	if cfg.CodeTTL <= 0 {
		cfg.CodeTTL = DefaultCodeTTL
	}

	redirectMap := make(map[string]bool)
	if len(cfg.RedirectURIs) == 0 {
		redirectMap[DefaultCallbackURI] = true
	} else {
		for _, u := range cfg.RedirectURIs {
			trimmed := strings.TrimSpace(u)
			if trimmed != "" {
				redirectMap[trimmed] = true
			}
		}
	}

	return &Server{
		cfg:          cfg,
		redirectURIs: redirectMap,
		codes:        make(map[string]codeEntry),
	}, nil
}

// base64 urlsafe without padding
func b64Encode(raw []byte) string {
	return base64.RawURLEncoding.EncodeToString(raw)
}

func b64Decode(s string) ([]byte, error) {
	// Support both padded and unpadded urlsafe base64
	s = strings.TrimRight(s, "=")
	return base64.RawURLEncoding.DecodeString(s)
}

func macOf(secret, body string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(body))
	return b64Encode(h.Sum(nil))
}

func (s *Server) mac(body string) string {
	return macOf(s.cfg.TokenSecret, body)
}

type tokenPayload struct {
	ClientID string `json:"client_id"`
	Exp      int64  `json:"exp"`
}

// Sign issues a bearer token for clientID that expires ttl seconds from now.
// It needs only the signing secret, so a token can be minted without the rest of the server config.
func Sign(secret, clientID string, ttl int64) string {
	p := tokenPayload{
		ClientID: clientID,
		Exp:      time.Now().Unix() + ttl,
	}
	raw, _ := json.Marshal(p)
	body := b64Encode(raw)
	return fmt.Sprintf("%s.%s", body, macOf(secret, body))
}

func (s *Server) SignToken(ttl int64) string {
	if ttl == 0 {
		ttl = s.cfg.TokenTTL
	}
	return Sign(s.cfg.TokenSecret, s.cfg.ClientID, ttl)
}

func (s *Server) TokenValid(token string) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return false
	}
	body, sig := parts[0], parts[1]
	expectedSig := s.mac(body)
	if !hmac.Equal([]byte(sig), []byte(expectedSig)) {
		return false
	}

	raw, err := b64Decode(body)
	if err != nil {
		return false
	}

	var p tokenPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return false
	}

	return p.ClientID == s.cfg.ClientID && p.Exp > time.Now().Unix()
}

// BearerGate middleware checks for valid Bearer token on /mcp requests.
func (s *Server) BearerGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/mcp") {
			auth := r.Header.Get("Authorization")
			if !strings.HasPrefix(strings.ToLower(auth), "bearer ") {
				s.denyToken(w)
				return
			}
			token := strings.TrimSpace(auth[7:])
			if !s.TokenValid(token) {
				s.denyToken(w)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) denyToken(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("WWW-Authenticate", "Bearer")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"invalid_token"}`))
}

func (s *Server) getBaseURL(r *http.Request) string {
	proto := r.Header.Get("X-Forwarded-Proto")
	if proto == "" {
		if r.TLS != nil {
			proto = "https"
		} else {
			proto = "http"
		}
	}
	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	return fmt.Sprintf("%s://%s", proto, host)
}

func (s *Server) HandleMetadata(w http.ResponseWriter, r *http.Request) {
	base := s.getBaseURL(r)
	meta := map[string]any{
		"issuer":                                base,
		"authorization_endpoint":                base + "/oauth/authorize",
		"token_endpoint":                        base + "/oauth/token",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_post", "client_secret_basic"},
		"code_challenge_methods_supported":      []string{"S256"},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(meta)
}

func (s *Server) requestError(p map[string]string) string {
	if p["client_id"] != s.cfg.ClientID {
		return "Неизвестный client_id."
	}
	if p["response_type"] != "code" {
		return "Поддерживается только response_type=code."
	}
	if !s.redirectURIs[p["redirect_uri"]] {
		return "Этот redirect_uri не в списке разрешённых."
	}
	if p["code_challenge"] == "" || p["code_challenge_method"] != "S256" {
		return "Нужен PKCE с методом S256."
	}
	return ""
}

var authFields = []string{"response_type", "client_id", "redirect_uri", "state", "code_challenge", "code_challenge_method"}

func (s *Server) HandleAuthorize(w http.ResponseWriter, r *http.Request) {
	p := make(map[string]string)
	if r.Method == http.MethodGet {
		q := r.URL.Query()
		for _, f := range authFields {
			p[f] = q.Get(f)
		}
	} else if r.Method == http.MethodPost {
		_ = r.ParseForm()
		for _, f := range authFields {
			p[f] = r.FormValue(f)
		}
	} else {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if errStr := s.requestError(p); errStr != "" {
		s.renderConsentPage(w, p, errStr, true, http.StatusBadRequest)
		return
	}

	if r.Method == http.MethodGet {
		s.renderConsentPage(w, p, "", false, http.StatusOK)
		return
	}

	// POST - verify password
	pwd := r.FormValue("password")
	if !hmac.Equal([]byte(pwd), []byte(s.cfg.Password)) {
		time.Sleep(1 * time.Second)
		s.renderConsentPage(w, p, "Неверный пароль", false, http.StatusUnauthorized)
		return
	}

	now := time.Now().Unix()
	codeBytes := make([]byte, 32)
	_, _ = rand.Read(codeBytes)
	code := b64Encode(codeBytes)

	s.mu.Lock()
	// purge expired
	for k, v := range s.codes {
		if v.expiresAt <= now {
			delete(s.codes, k)
		}
	}
	s.codes[code] = codeEntry{
		expiresAt:     now + s.cfg.CodeTTL,
		redirectURI:   p["redirect_uri"],
		codeChallenge: p["code_challenge"],
	}
	s.mu.Unlock()

	redir, _ := url.Parse(p["redirect_uri"])
	rq := redir.Query()
	rq.Set("code", code)
	if state := p["state"]; state != "" {
		rq.Set("state", state)
	}
	redir.RawQuery = rq.Encode()

	http.Redirect(w, r, redir.String(), http.StatusSeeOther)
}

func (s *Server) HandleToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	bodyMap := make(map[string]string)
	contentType := r.Header.Get("Content-Type")

	if strings.Contains(contentType, "application/json") {
		var jm map[string]any
		bodyBytes, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(bodyBytes, &jm); err == nil {
			for k, v := range jm {
				bodyMap[k] = fmt.Sprint(v)
			}
		}
	} else {
		_ = r.ParseForm()
		for k := range r.PostForm {
			bodyMap[k] = r.PostFormValue(k)
		}
	}

	clientID := bodyMap["client_id"]
	clientSecret := bodyMap["client_secret"]

	auth := r.Header.Get("Authorization")
	if clientSecret == "" && strings.HasPrefix(strings.ToLower(auth), "basic ") {
		raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(auth[6:]))
		if err == nil {
			parts := strings.SplitN(string(raw), ":", 2)
			if len(parts) == 2 {
				if clientID == "" {
					clientID = parts[0]
				}
				clientSecret = parts[1]
			}
		}
	}

	if clientID != s.cfg.ClientID || !hmac.Equal([]byte(clientSecret), []byte(s.cfg.ClientSecret)) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_client"})
		return
	}

	if bodyMap["grant_type"] != "authorization_code" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "unsupported_grant_type"})
		return
	}

	code := bodyMap["code"]
	s.mu.Lock()
	entry, found := s.codes[code]
	delete(s.codes, code) // single-use
	s.mu.Unlock()

	now := time.Now().Unix()
	if !found || entry.expiresAt <= now || bodyMap["redirect_uri"] != entry.redirectURI {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
		return
	}

	// Verify PKCE
	codeVerifier := bodyMap["code_verifier"]
	sha := sha256.Sum256([]byte(codeVerifier))
	challenge := b64Encode(sha[:])

	if !hmac.Equal([]byte(challenge), []byte(entry.codeChallenge)) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
		return
	}

	token := s.SignToken(s.cfg.TokenTTL)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token": token,
		"token_type":   "Bearer",
		"expires_in":   s.cfg.TokenTTL,
	})
}

func (s *Server) renderConsentPage(w http.ResponseWriter, p map[string]string, errorMsg string, fatal bool, status int) {
	var lede, body, stamp string

	if fatal {
		lede = fmt.Sprintf("Запрос на доступ не прошёл проверку. %s", html.EscapeString(errorMsg))
		body = ""
		stamp = "Отказ"
	} else {
		u, _ := url.Parse(p["redirect_uri"])
		host := ""
		if u != nil {
			host = u.Host
		}
		var hidden strings.Builder
		for _, k := range authFields {
			hidden.WriteString(fmt.Sprintf(`<input type="hidden" name="%s" value="%s">`, k, html.EscapeString(p[k])))
		}
		lede = fmt.Sprintf("<b>%s</b> просит доступ к справочнику АптекаМос. Только чтение: искать лекарства, цены и аптеки.", html.EscapeString(host))
		body = fmt.Sprintf(`<form method="post" action="/oauth/authorize" class="rise r4">%s
      <input type="text" name="username" value="aptekamos-mcp" autocomplete="username" hidden>
      <label for="pw">Пароль владельца</label>
      <input id="pw" name="password" type="password" autocomplete="current-password" required autofocus>
      <button type="submit">Разрешить доступ</button>
    </form>`, hidden.String())
		stamp = errorMsg
	}

	stampHTML := ""
	if stamp != "" {
		stampHTML = fmt.Sprintf(`<p class="stamp" role="alert">%s</p>`, html.EscapeString(stamp))
	}

	page := consentPageHTML
	page = strings.ReplaceAll(page, "$lede", lede)
	page = strings.ReplaceAll(page, "$body", body)
	page = strings.ReplaceAll(page, "$stamp", stampHTML)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(page))
}

const consentPageHTML = `<!doctype html>
<html lang="ru">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex">
<meta name="referrer" content="no-referrer">
<title>АптекаМос → Claude</title>
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=IBM+Plex+Mono:wght@400;600&family=IBM+Plex+Sans+Condensed:wght@700&display=swap" rel="stylesheet">
<style>
  :root {
    --bg: #dfe7e2; --paper: #fbfcf8; --ink: #17241d; --muted: #66756c;
    --green: #1f8a4c; --red: #c8352a; --rule: rgba(23, 36, 29, .22);
    --mono: "IBM Plex Mono", ui-monospace, Menlo, Consolas, monospace;
    --head: "IBM Plex Sans Condensed", "Arial Narrow", sans-serif;
    color-scheme: light;
  }
  * { box-sizing: border-box; }
  body {
    margin: 0; min-height: 100vh; display: grid; place-items: center; padding: 48px 16px;
    color: var(--ink); font: 14px/1.55 var(--mono);
    background: radial-gradient(110% 80% at 50% 20%, #eef3ef 30%, var(--bg));
  }
  .receipt {
    position: relative; width: min(100%, 360px); padding: 28px 24px 34px; background: var(--paper);
    box-shadow: 0 1px 0 rgba(0,0,0,.05), 0 24px 40px -22px rgba(23, 36, 29, .55);
    animation: feed .6s cubic-bezier(.2, .9, .3, 1.1) both;
    -webkit-mask: radial-gradient(9px at 50% 100%, #0000 98%, #000) 50% / 18px 100% repeat-x;
            mask: radial-gradient(9px at 50% 100%, #0000 98%, #000) 50% / 18px 100% repeat-x;
  }
  .head { display: flex; align-items: center; gap: 14px; }
  .cross { width: 46px; height: 46px; flex: none; position: relative; }
  .cross::before, .cross::after { content: ""; position: absolute; inset: 15px 0; background: var(--green); border-radius: 3px; }
  .cross::after { inset: 0 15px; }
  h1 { margin: 0; font: 700 34px/1 var(--head); letter-spacing: .01em; text-transform: uppercase; }
  .sub { margin: 2px 0 0; color: var(--muted); font-size: 11px; letter-spacing: .22em; text-transform: uppercase; }
  .rule { margin: 18px 0; border: 0; border-top: 2px dashed var(--rule); }
  .row { display: flex; justify-content: space-between; color: var(--muted); font-size: 12px; }
  .row b { color: var(--ink); font-weight: 600; }
  .lede { margin: 0 0 18px; }
  .lede b { color: var(--green); }
  label { display: block; margin-bottom: 2px; color: var(--muted); font-size: 11px; font-weight: 600; letter-spacing: .2em; text-transform: uppercase; }
  input[type=password] {
    width: 100%; padding: 6px 2px; border: 0; border-bottom: 2px solid var(--ink);
    background: transparent; color: var(--ink); font: 18px var(--mono); letter-spacing: .12em;
  }
  input[type=password]:focus { outline: none; border-bottom-color: var(--green); }
  button {
    width: 100%; margin-top: 22px; padding: 11px 14px; border: 0; border-radius: 4px;
    background: var(--green); color: #fff; cursor: pointer;
    font: 700 20px/1 var(--head); letter-spacing: .06em; text-transform: uppercase;
    transition: transform .12s, filter .12s;
  }
  button:hover { filter: brightness(1.08); }
  button:active { transform: translateY(2px); }
  button:focus-visible { outline: 3px solid var(--ink); outline-offset: 3px; }
  .stamp {
    position: absolute; top: 92px; right: 18px; margin: 0; padding: 3px 10px 1px;
    border: 3px double var(--red); color: var(--red); mix-blend-mode: multiply;
    font: 700 22px/1 var(--head); text-transform: uppercase; transform: rotate(-9deg);
    animation: stamp .3s .4s cubic-bezier(.3, 1.6, .5, 1) both;
  }
  .rise { animation: rise .45s both; }
  .r1 { animation-delay: .15s; } .r2 { animation-delay: .22s; } .r3 { animation-delay: .3s; } .r4 { animation-delay: .38s; }
  @keyframes feed { from { opacity: 0; transform: translateY(-18px); } }
  @keyframes rise { from { opacity: 0; transform: translateY(8px); } }
  @keyframes stamp { from { opacity: 0; transform: scale(1.8) rotate(-9deg); } }
  @media (prefers-reduced-motion: reduce) { *, *::before, *::after { animation: none !important; transition: none !important; } }
</style>
</head>
<body>
<main class="receipt">
  <div class="head rise r1">
    <span class="cross" aria-hidden="true"></span>
    <div><h1>АптекаМос</h1><p class="sub">MCP · только чтение</p></div>
  </div>
  <hr class="rule">
  <div class="row rise r2"><span>Доступ</span><b>лекарства · цены · аптеки</b></div>
  <hr class="rule">
  <p class="lede rise r3">$lede</p>
  $body
  $stamp
</main>
</body>
</html>`
