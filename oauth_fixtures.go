package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
)

const oauthFixturePrefix = "/fixtures/oauth"

type oauthFixtureConfig struct {
	Issuer   string
	Resource string
}

func oauthFixtureConfigFromEnv() oauthFixtureConfig {
	return oauthFixtureConfig{
		Issuer:   os.Getenv("OAUTH_FIXTURE_ISSUER"),
		Resource: os.Getenv("OAUTH_FIXTURE_RESOURCE"),
	}
}

type oauthClient struct {
	RedirectURIs []string
}

type oauthCode struct {
	ClientID    string
	RedirectURI string
	Challenge   string
	Resource    string
	Scope       string
}

type oauthGrant struct {
	ClientID string
	Resource string
	Scope    string
}

type oauthCapture struct {
	Endpoint           string `json:"endpoint"`
	Method             string `json:"method"`
	Accept             string `json:"accept,omitempty"`
	ContentType        string `json:"content_type,omitempty"`
	MCPProtocolVersion string `json:"mcp_protocol_version,omitempty"`
	Authorization      bool   `json:"authorization_present"`
}

// oauthFixtures is a deterministic, in-memory OAuth authorization server used
// only by local integration tests. It stores no caller credentials or bearer
// values in captures and can be reset between test namespaces.
type oauthFixtures struct {
	mu          sync.Mutex
	config      oauthFixtureConfig
	mcpHandler  http.Handler
	sequence    int
	clients     map[string]oauthClient
	codes       map[string]oauthCode
	access      map[string]oauthGrant
	refresh     map[string]oauthGrant
	usedCodes   map[string]struct{}
	usedRefresh map[string]struct{}
	counters    map[string]int
	captures    []oauthCapture
}

func newOAuthFixtures(mcpHandler http.Handler, config oauthFixtureConfig) *oauthFixtures {
	return &oauthFixtures{
		config:      config,
		mcpHandler:  mcpHandler,
		clients:     make(map[string]oauthClient),
		codes:       make(map[string]oauthCode),
		access:      make(map[string]oauthGrant),
		refresh:     make(map[string]oauthGrant),
		usedCodes:   make(map[string]struct{}),
		usedRefresh: make(map[string]struct{}),
		counters:    make(map[string]int),
	}
}

func (f *oauthFixtures) register(mux *http.ServeMux) {
	mux.HandleFunc("/.well-known/oauth-protected-resource/fixtures/oauth/mcp", f.protectedResourceMetadata)
	mux.HandleFunc(oauthFixturePrefix+"/mcp/.well-known/oauth-protected-resource", f.protectedResourceMetadata)
	mux.HandleFunc("/.well-known/oauth-authorization-server/fixtures/oauth", f.authorizationServerMetadata)
	mux.HandleFunc(oauthFixturePrefix+"/.well-known/oauth-authorization-server", f.authorizationServerMetadata)
	mux.HandleFunc(oauthFixturePrefix+"/register", f.registerClient)
	mux.HandleFunc(oauthFixturePrefix+"/authorize", f.authorize)
	mux.HandleFunc(oauthFixturePrefix+"/token", f.token)
	mux.HandleFunc(oauthFixturePrefix+"/counters", f.serveCounters)
	mux.HandleFunc(oauthFixturePrefix+"/captures", f.serveCaptures)
	mux.HandleFunc(oauthFixturePrefix+"/reset", f.reset)
	mux.Handle(oauthFixturePrefix+"/mcp", f.requireAccessToken(f.mcpHandler))
}

func (f *oauthFixtures) identities(r *http.Request) (issuer, resource string, err error) {
	issuer, resource = f.config.Issuer, f.config.Resource
	if issuer == "" || resource == "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		base := scheme + "://" + r.Host
		if issuer == "" {
			issuer = base + oauthFixturePrefix
		}
		if resource == "" {
			resource = base + oauthFixturePrefix + "/mcp"
		}
	}
	if !validOAuthIdentity(issuer) || !validOAuthIdentity(resource) {
		return "", "", fmt.Errorf("invalid OAuth fixture identity")
	}
	return issuer, resource, nil
}

func validOAuthIdentity(value string) bool {
	u, err := url.Parse(value)
	return err == nil && u.IsAbs() && (u.Scheme == "https" || u.Scheme == "http") &&
		u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == ""
}

func issuerEndpoint(issuer, endpoint string) string {
	return strings.TrimSuffix(issuer, "/") + "/" + endpoint
}

func (f *oauthFixtures) record(r *http.Request, endpoint string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.counters[endpoint]++
	f.captures = append(f.captures, oauthCapture{
		Endpoint:           endpoint,
		Method:             r.Method,
		Accept:             r.Header.Get("Accept"),
		ContentType:        r.Header.Get("Content-Type"),
		MCPProtocolVersion: r.Header.Get("Mcp-Protocol-Version"),
		Authorization:      r.Header.Get("Authorization") != "",
	})
}

func (f *oauthFixtures) protectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	f.record(r, "protected_resource_metadata")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	issuer, resource, err := f.identities(r)
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error")
		return
	}
	writeOAuthJSON(w, http.StatusOK, map[string]any{
		"resource":              resource,
		"authorization_servers": []string{issuer},
		"scopes_supported":      []string{"mcp"},
		"fixture_extension":     map[string]any{"kept": true},
	})
}

func (f *oauthFixtures) authorizationServerMetadata(w http.ResponseWriter, r *http.Request) {
	f.record(r, "authorization_server_metadata")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	issuer, _, err := f.identities(r)
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error")
		return
	}
	metadataIssuer := issuer
	if r.URL.Query().Get("issuer") == "wrong" {
		metadataIssuer = issuer + "/wrong"
	}
	result := map[string]any{
		"issuer":                                         metadataIssuer,
		"authorization_endpoint":                         issuerEndpoint(issuer, "authorize"),
		"token_endpoint":                                 issuerEndpoint(issuer, "token"),
		"registration_endpoint":                          issuerEndpoint(issuer, "register"),
		"response_types_supported":                       []string{"code"},
		"grant_types_supported":                          []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":               []string{"S256"},
		"token_endpoint_auth_methods_supported":          []string{"none"},
		"authorization_response_iss_parameter_supported": true,
		"fixture_extension":                              map[string]any{"kept": true},
	}
	if r.URL.Query().Get("iss_support") == "missing" {
		delete(result, "authorization_response_iss_parameter_supported")
	}
	writeOAuthJSON(w, http.StatusOK, result)
}

func (f *oauthFixtures) registerClient(w http.ResponseWriter, r *http.Request) {
	f.record(r, "register")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var request struct {
		RedirectURIs            []string `json:"redirect_uris"`
		TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	if err != nil || decodeStrictJSONObject(body, &request) != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata")
		return
	}
	if request.TokenEndpointAuthMethod != "none" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata")
		return
	}
	if !validRedirectURIs(request.RedirectURIs) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_redirect_uri")
		return
	}
	f.mu.Lock()
	f.sequence++
	clientID := fmt.Sprintf("fixture-client-%d", f.sequence)
	f.clients[clientID] = oauthClient{RedirectURIs: slices.Clone(request.RedirectURIs)}
	f.mu.Unlock()
	writeOAuthJSON(w, http.StatusCreated, map[string]any{
		"client_id":                  clientID,
		"redirect_uris":              request.RedirectURIs,
		"token_endpoint_auth_method": "none",
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
	})
}

func decodeStrictJSONObject(body []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return fmt.Errorf("expected JSON object")
	}
	seen := make(map[string]struct{})
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return err
		}
		name, ok := key.(string)
		if !ok {
			return fmt.Errorf("invalid JSON object key")
		}
		matchName := strings.ToLower(name)
		if _, duplicate := seen[matchName]; duplicate {
			return fmt.Errorf("duplicate JSON object key %q", name)
		}
		seen[matchName] = struct{}{}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return err
		}
	}
	if _, err := decoder.Token(); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("trailing JSON value")
	}
	return json.Unmarshal(body, target)
}

func validRedirectURIs(values []string) bool {
	if len(values) == 0 {
		return false
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if strings.Contains(value, "#") {
			return false
		}
		u, err := url.ParseRequestURI(value)
		if err != nil || !u.IsAbs() || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.Fragment != "" {
			return false
		}
		if _, duplicate := seen[value]; duplicate {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func (f *oauthFixtures) authorize(w http.ResponseWriter, r *http.Request) {
	f.record(r, "authorize")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	issuer, resource, err := f.identities(r)
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error")
		return
	}
	query := r.URL.Query()
	clientID, redirectURI := query.Get("client_id"), query.Get("redirect_uri")
	f.mu.Lock()
	client, knownClient := f.clients[clientID]
	f.mu.Unlock()
	if !knownClient || !slices.Contains(client.RedirectURIs, redirectURI) ||
		query.Get("response_type") != "code" || query.Get("code_challenge_method") != "S256" ||
		!validPKCEValue(query.Get("code_challenge")) || !singleValue(query, "client_id") ||
		!singleValue(query, "redirect_uri") || !singleValue(query, "response_type") ||
		!singleValue(query, "code_challenge") || !singleValue(query, "code_challenge_method") {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if !singleValue(query, "state") || query.Get("state") == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if !singleValue(query, "scope") || query.Get("scope") != "mcp" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_scope")
		return
	}
	if !singleValue(query, "resource") || query.Get("resource") != resource {
		writeOAuthError(w, http.StatusBadRequest, "invalid_target")
		return
	}
	callback, err := url.Parse(redirectURI)
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	params := callback.Query()
	for _, reserved := range []string{"code", "error", "error_description", "error_uri", "state", "iss"} {
		params.Del(reserved)
	}
	if state, present := query["state"]; present {
		switch query.Get("fixture_state") {
		case "missing":
		case "wrong":
			params.Set("state", "fixture-wrong-state")
		case "duplicate":
			params.Add("state", state[0])
			params.Add("state", state[0]+"-duplicate")
		default:
			params.Set("state", state[0])
		}
	}
	switch query.Get("fixture_iss") {
	case "missing":
	case "wrong":
		params.Set("iss", issuer+"/wrong")
	case "duplicate":
		params.Add("iss", issuer)
		params.Add("iss", issuer+"/duplicate")
	default:
		responseIssuer := issuer
		params.Set("iss", responseIssuer)
	}
	if query.Get("fixture_decision") == "deny" {
		params.Set("error", "access_denied")
		params.Set("error_description", "fixture authorization denied")
	} else {
		f.mu.Lock()
		f.sequence++
		code := fmt.Sprintf("fixture-code-%d", f.sequence)
		f.codes[code] = oauthCode{
			ClientID: clientID, RedirectURI: redirectURI, Challenge: query.Get("code_challenge"),
			Resource: resource, Scope: query.Get("scope"),
		}
		f.mu.Unlock()
		params.Set("code", code)
	}
	callback.RawQuery = params.Encode()
	http.Redirect(w, r, callback.String(), http.StatusFound)
}

func validPKCEValue(value string) bool {
	if len(value) < 43 || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || strings.ContainsRune("-._~", char)) {
			return false
		}
	}
	return true
}

func singleValue(values url.Values, key string) bool { return len(values[key]) == 1 }

func (f *oauthFixtures) token(w http.ResponseWriter, r *http.Request) {
	f.record(r, "token")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if r.Header.Get("Authorization") != "" || len(r.Form["client_secret"]) != 0 ||
		!singleValue(r.Form, "grant_type") || !singleValue(r.Form, "client_id") ||
		r.Form.Get("client_id") == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	switch r.Form.Get("grant_type") {
	case "authorization_code":
		f.exchangeCode(w, r.Form)
	case "refresh_token":
		f.exchangeRefresh(w, r.Form)
	default:
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type")
	}
}

func (f *oauthFixtures) exchangeCode(w http.ResponseWriter, form url.Values) {
	codeValue := form.Get("code")
	f.mu.Lock()
	code, exists := f.codes[codeValue]
	_, replayed := f.usedCodes[codeValue]
	if !exists || replayed {
		f.mu.Unlock()
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant")
		return
	}
	if form.Get("client_id") != code.ClientID || form.Get("redirect_uri") != code.RedirectURI ||
		!singleValue(form, "client_id") || !singleValue(form, "redirect_uri") || !singleValue(form, "code") ||
		!singleValue(form, "code_verifier") || !validPKCEValue(form.Get("code_verifier")) ||
		pkceChallenge(form.Get("code_verifier")) != code.Challenge {
		f.mu.Unlock()
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant")
		return
	}
	if !singleValue(form, "resource") || form.Get("resource") != code.Resource {
		f.mu.Unlock()
		writeOAuthError(w, http.StatusBadRequest, "invalid_target")
		return
	}
	delete(f.codes, codeValue)
	f.usedCodes[codeValue] = struct{}{}
	accessToken, refreshToken := f.issueGrantLocked(oauthGrant{ClientID: code.ClientID, Resource: code.Resource, Scope: code.Scope})
	f.mu.Unlock()
	writeTokenResponse(w, accessToken, refreshToken, code.Scope)
}

func (f *oauthFixtures) exchangeRefresh(w http.ResponseWriter, form url.Values) {
	refreshValue := form.Get("refresh_token")
	f.mu.Lock()
	grant, exists := f.refresh[refreshValue]
	_, replayed := f.usedRefresh[refreshValue]
	if exists && !replayed && singleValue(form, "client_id") && form.Get("client_id") == grant.ClientID &&
		singleValue(form, "refresh_token") {
		if !singleValue(form, "resource") || form.Get("resource") != grant.Resource {
			f.mu.Unlock()
			writeOAuthError(w, http.StatusBadRequest, "invalid_target")
			return
		}
		delete(f.refresh, refreshValue)
		f.usedRefresh[refreshValue] = struct{}{}
		accessToken, refreshToken := f.issueGrantLocked(grant)
		f.mu.Unlock()
		writeTokenResponse(w, accessToken, refreshToken, grant.Scope)
		return
	}
	f.mu.Unlock()
	writeOAuthError(w, http.StatusBadRequest, "invalid_grant")
}

func (f *oauthFixtures) issueGrantLocked(grant oauthGrant) (accessToken, refreshToken string) {
	f.sequence++
	accessToken = fmt.Sprintf("fixture-access-%d", f.sequence)
	f.sequence++
	refreshToken = fmt.Sprintf("fixture-refresh-%d", f.sequence)
	f.access[accessToken] = grant
	f.refresh[refreshToken] = grant
	return accessToken, refreshToken
}

func pkceChallenge(verifier string) string {
	digest := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func writeTokenResponse(w http.ResponseWriter, accessToken, refreshToken, scope string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	writeOAuthJSON(w, http.StatusOK, map[string]any{
		"access_token": accessToken, "refresh_token": refreshToken,
		"token_type": "Bearer", "expires_in": 300, "scope": scope,
	})
}

func (f *oauthFixtures) requireAccessToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.record(r, "mcp")
		const prefix = "Bearer "
		authorization := r.Header.Get("Authorization")
		if !strings.HasPrefix(authorization, prefix) {
			w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
			writeOAuthError(w, http.StatusUnauthorized, "invalid_token")
			return
		}
		tokenValue := strings.TrimPrefix(authorization, prefix)
		_, resource, err := f.identities(r)
		if err != nil {
			writeOAuthError(w, http.StatusInternalServerError, "server_error")
			return
		}
		f.mu.Lock()
		grant, ok := f.access[tokenValue]
		f.mu.Unlock()
		if !ok || grant.Resource != resource {
			w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
			writeOAuthError(w, http.StatusUnauthorized, "invalid_token")
			return
		}
		if r.Body != nil {
			body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
			if err != nil {
				writeOAuthError(w, http.StatusBadRequest, "invalid_request")
				return
			}
			r.Body.Close()
			r.Body = io.NopCloser(strings.NewReader(string(body)))
			var envelope struct {
				Method string `json:"method"`
			}
			if json.Unmarshal(body, &envelope) == nil && envelope.Method != "" {
				f.mu.Lock()
				f.counters["mcp:"+envelope.Method]++
				f.mu.Unlock()
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (f *oauthFixtures) serveCounters(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	copy := make(map[string]int, len(f.counters))
	for key, value := range f.counters {
		copy[key] = value
	}
	writeOAuthJSON(w, http.StatusOK, copy)
}

func (f *oauthFixtures) serveCaptures(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	writeOAuthJSON(w, http.StatusOK, slices.Clone(f.captures))
}

func (f *oauthFixtures) reset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	f.mu.Lock()
	f.sequence = 0
	f.clients = make(map[string]oauthClient)
	f.codes = make(map[string]oauthCode)
	f.access = make(map[string]oauthGrant)
	f.refresh = make(map[string]oauthGrant)
	f.usedCodes = make(map[string]struct{})
	f.usedRefresh = make(map[string]struct{})
	f.counters = make(map[string]int)
	f.captures = nil
	f.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func writeOAuthError(w http.ResponseWriter, status int, code string) {
	writeOAuthJSON(w, status, map[string]any{"error": code})
}

func writeOAuthJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
