package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

const fixtureVerifier = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~"

func newOAuthFixtureServer(t *testing.T) (*oauthFixtures, *httptest.Server, *http.Client) {
	t.Helper()
	fixture := newOAuthFixtures(newProtocolSwitchHandler(setupServer()), oauthFixtureConfig{})
	mux := http.NewServeMux()
	fixture.register(mux)
	server := httptest.NewServer(mux)
	client := server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	t.Cleanup(server.Close)
	return fixture, server, client
}

func registerFixtureClient(t *testing.T, client *http.Client, baseURL string, redirects []string) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"redirect_uris": redirects, "token_endpoint_auth_method": "none",
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Post(baseURL+oauthFixturePrefix+"/register", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(response.Body)
		t.Fatalf("registration status=%d body=%s", response.StatusCode, raw)
	}
	var registration struct {
		ClientID string `json:"client_id"`
	}
	if err := json.NewDecoder(response.Body).Decode(&registration); err != nil {
		t.Fatal(err)
	}
	if registration.ClientID == "" {
		t.Fatal("missing client_id")
	}
	return registration.ClientID
}

func authorizeFixture(t *testing.T, client *http.Client, baseURL, clientID, redirectURI string, extra url.Values) *http.Response {
	t.Helper()
	query := url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {redirectURI},
		"state":                 {"state-%2F-exact"},
		"code_challenge":        {pkceChallenge(fixtureVerifier)},
		"code_challenge_method": {"S256"},
		"resource":              {baseURL + oauthFixturePrefix + "/mcp"},
		"scope":                 {"mcp"},
	}
	for key, values := range extra {
		query[key] = values
	}
	response, err := client.Get(baseURL + oauthFixturePrefix + "/authorize?" + query.Encode())
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func exchangeFixtureCode(t *testing.T, client *http.Client, baseURL, clientID, redirectURI, code string, overrides url.Values) (*http.Response, map[string]any) {
	t.Helper()
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"redirect_uri":  {redirectURI},
		"code":          {code},
		"code_verifier": {fixtureVerifier},
		"resource":      {baseURL + oauthFixturePrefix + "/mcp"},
	}
	for key, values := range overrides {
		form[key] = values
	}
	response, err := client.PostForm(baseURL+oauthFixturePrefix+"/token", form)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		response.Body.Close()
		t.Fatal(err)
	}
	response.Body.Close()
	return response, payload
}

func TestOAuthFixtureDiscoveryCaptureAndTLSConfiguration(t *testing.T) {
	_, server, client := newOAuthFixtureServer(t)
	for _, endpoint := range []string{
		"/.well-known/oauth-protected-resource/fixtures/oauth/mcp",
		"/.well-known/oauth-authorization-server/fixtures/oauth",
	} {
		req, err := http.NewRequest(http.MethodGet, server.URL+endpoint, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Mcp-Protocol-Version", "2026-07-28")
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusOK {
			t.Fatalf("%s status=%d", endpoint, response.StatusCode)
		}
		response.Body.Close()
	}

	response, err := client.Get(server.URL + oauthFixturePrefix + "/captures")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var captures []oauthCapture
	if err := json.NewDecoder(response.Body).Decode(&captures); err != nil {
		t.Fatal(err)
	}
	if len(captures) != 2 {
		t.Fatalf("captures=%+v", captures)
	}
	for _, capture := range captures {
		if capture.Accept != "application/json" || capture.MCPProtocolVersion != "2026-07-28" || capture.Authorization {
			t.Fatalf("unexpected safe capture: %+v", capture)
		}
	}

	response, err = client.Get(server.URL + "/.well-known/oauth-authorization-server/fixtures/oauth?issuer=wrong&iss_support=missing")
	if err != nil {
		t.Fatal(err)
	}
	var variantMetadata map[string]any
	if err := json.NewDecoder(response.Body).Decode(&variantMetadata); err != nil {
		response.Body.Close()
		t.Fatal(err)
	}
	response.Body.Close()
	if variantMetadata["issuer"] != server.URL+oauthFixturePrefix+"/wrong" {
		t.Fatalf("variant issuer=%v", variantMetadata["issuer"])
	}
	if _, ok := variantMetadata["authorization_response_iss_parameter_supported"]; ok {
		t.Fatalf("unexpected issuer response support declaration: %+v", variantMetadata)
	}

	fixture := newOAuthFixtures(http.NotFoundHandler(), oauthFixtureConfig{
		Issuer: "https://issuer.fixture.example/as", Resource: "https://resource.fixture.example/mcp",
	})
	mux := http.NewServeMux()
	fixture.register(mux)
	tlsServer := httptest.NewTLSServer(mux)
	defer tlsServer.Close()
	response, err = tlsServer.Client().Get(tlsServer.URL + "/.well-known/oauth-authorization-server/fixtures/oauth")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var metadata map[string]any
	if err := json.NewDecoder(response.Body).Decode(&metadata); err != nil {
		t.Fatal(err)
	}
	if metadata["issuer"] != "https://issuer.fixture.example/as" {
		t.Fatalf("issuer=%v", metadata["issuer"])
	}
}

func TestOAuthFixtureConfiguredIssuerIsExactAndInvalidIdentityFailsClosed(t *testing.T) {
	t.Setenv("OAUTH_FIXTURE_ISSUER", "https://issuer.example/as/")
	t.Setenv("OAUTH_FIXTURE_RESOURCE", "https://resource.example/mcp")
	config := oauthFixtureConfigFromEnv()
	if config.Issuer != "https://issuer.example/as/" {
		t.Fatalf("configured issuer normalized: %q", config.Issuer)
	}
	fixture := newOAuthFixtures(http.NotFoundHandler(), config)
	mux := http.NewServeMux()
	fixture.register(mux)
	request := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server/fixtures/oauth", nil)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	var metadata map[string]any
	if err := json.NewDecoder(recorder.Body).Decode(&metadata); err != nil {
		t.Fatal(err)
	}
	if metadata["issuer"] != config.Issuer || metadata["authorization_endpoint"] != "https://issuer.example/as/authorize" {
		t.Fatalf("metadata=%v", metadata)
	}

	invalid := newOAuthFixtures(http.NotFoundHandler(), oauthFixtureConfig{
		Issuer: "https://issuer.example/as?ambiguous=yes", Resource: config.Resource,
	})
	mux = http.NewServeMux()
	invalid.register(mux)
	recorder = httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("invalid configured identity status=%d", recorder.Code)
	}
}

func TestOAuthFixtureStrictAuthorizationCodeAndRefresh(t *testing.T) {
	_, server, client := newOAuthFixtureServer(t)
	redirectURI := "https://client.example/callback?existing=kept"

	for _, registration := range []map[string]any{
		{},
		{"redirect_uris": []string{"/relative"}},
		{"redirect_uris": []string{"https://client.example/callback#fragment"}},
		{"redirect_uris": []string{redirectURI, redirectURI}},
		{"redirect_uris": []string{redirectURI}, "token_endpoint_auth_method": "client_secret_basic"},
	} {
		body, _ := json.Marshal(registration)
		response, err := client.Post(server.URL+oauthFixturePrefix+"/register", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("invalid registration accepted: %#v", registration)
		}
	}
	clientID := registerFixtureClient(t, client, server.URL, []string{redirectURI})

	for _, test := range []struct {
		name      string
		extra     url.Values
		wantCode  bool
		wantErr   string
		wantState []string
		wantISS   []string
	}{
		{name: "success exact state and issuer", wantCode: true, wantState: []string{"state-%2F-exact"}, wantISS: []string{server.URL + oauthFixturePrefix}},
		{name: "success missing issuer", extra: url.Values{"fixture_iss": {"missing"}}, wantCode: true, wantState: []string{"state-%2F-exact"}},
		{name: "success wrong issuer", extra: url.Values{"fixture_iss": {"wrong"}}, wantCode: true, wantState: []string{"state-%2F-exact"}, wantISS: []string{server.URL + oauthFixturePrefix + "/wrong"}},
		{name: "success duplicate issuer", extra: url.Values{"fixture_iss": {"duplicate"}}, wantCode: true, wantState: []string{"state-%2F-exact"}, wantISS: []string{server.URL + oauthFixturePrefix, server.URL + oauthFixturePrefix + "/duplicate"}},
		{name: "success missing state", extra: url.Values{"fixture_state": {"missing"}}, wantCode: true, wantISS: []string{server.URL + oauthFixturePrefix}},
		{name: "success wrong state", extra: url.Values{"fixture_state": {"wrong"}}, wantCode: true, wantState: []string{"fixture-wrong-state"}, wantISS: []string{server.URL + oauthFixturePrefix}},
		{name: "success duplicate state", extra: url.Values{"fixture_state": {"duplicate"}}, wantCode: true, wantState: []string{"state-%2F-exact", "state-%2F-exact-duplicate"}, wantISS: []string{server.URL + oauthFixturePrefix}},
		{name: "provider denial", extra: url.Values{"fixture_decision": {"deny"}}, wantErr: "access_denied", wantState: []string{"state-%2F-exact"}, wantISS: []string{server.URL + oauthFixturePrefix}},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := authorizeFixture(t, client, server.URL, clientID, redirectURI, test.extra)
			defer response.Body.Close()
			if response.StatusCode != http.StatusFound {
				t.Fatalf("status=%d", response.StatusCode)
			}
			location, err := url.Parse(response.Header.Get("Location"))
			if err != nil {
				t.Fatal(err)
			}
			query := location.Query()
			if fmt.Sprint(query["state"]) != fmt.Sprint(test.wantState) || fmt.Sprint(query["iss"]) != fmt.Sprint(test.wantISS) || query.Get("error") != test.wantErr {
				t.Fatalf("callback query=%v", query)
			}
			if (query.Get("code") != "") != test.wantCode {
				t.Fatalf("callback query=%v", query)
			}
		})
	}

	wrongResource := authorizeFixture(t, client, server.URL, clientID, redirectURI, url.Values{"resource": {"https://wrong.example/resource"}})
	wrongResource.Body.Close()
	if wrongResource.StatusCode != http.StatusBadRequest || wrongResource.Header.Get("Location") != "" {
		t.Fatal("wrong resource was redirected")
	}
	foreignCallback := authorizeFixture(t, client, server.URL, clientID, "https://foreign.example/callback", nil)
	foreignCallback.Body.Close()
	if foreignCallback.StatusCode != http.StatusBadRequest || foreignCallback.Header.Get("Location") != "" {
		t.Fatal("foreign callback was redirected")
	}

	authorization := authorizeFixture(t, client, server.URL, clientID, redirectURI, nil)
	location, _ := url.Parse(authorization.Header.Get("Location"))
	authorization.Body.Close()
	code := location.Query().Get("code")

	for _, invalid := range []struct {
		values    url.Values
		wantError string
	}{
		{values: url.Values{"resource": {"https://wrong.example/resource"}}, wantError: "invalid_target"},
		{values: url.Values{"code_verifier": {""}}, wantError: "invalid_grant"},
		{values: url.Values{"code_verifier": {strings.Repeat("a", 43)}}, wantError: "invalid_grant"},
	} {
		response, payload := exchangeFixtureCode(t, client, server.URL, clientID, redirectURI, code, invalid.values)
		if response.StatusCode != http.StatusBadRequest || payload["error"] != invalid.wantError {
			t.Fatalf("invalid exchange %v status=%d payload=%v", invalid, response.StatusCode, payload)
		}
	}
	response, payload := exchangeFixtureCode(t, client, server.URL, clientID, redirectURI, code, nil)
	if response.StatusCode != http.StatusOK || response.Header.Get("Cache-Control") != "no-store" || response.Header.Get("Pragma") != "no-cache" {
		t.Fatalf("token response status=%d headers=%v", response.StatusCode, response.Header)
	}
	accessToken, _ := payload["access_token"].(string)
	refreshToken, _ := payload["refresh_token"].(string)
	if accessToken == "" || refreshToken == "" {
		t.Fatalf("tokens=%v", payload)
	}
	response, payload = exchangeFixtureCode(t, client, server.URL, clientID, redirectURI, code, nil)
	if response.StatusCode != http.StatusBadRequest || payload["error"] != "invalid_grant" {
		t.Fatalf("code replay status=%d payload=%v", response.StatusCode, payload)
	}

	refresh := func(token, resource string) (*http.Response, map[string]any) {
		form := url.Values{
			"grant_type": {"refresh_token"}, "client_id": {clientID},
			"refresh_token": {token}, "resource": {resource},
		}
		response, err := client.PostForm(server.URL+oauthFixturePrefix+"/token", form)
		if err != nil {
			t.Fatal(err)
		}
		var result map[string]any
		if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		return response, result
	}
	response, payload = refresh(refreshToken, "https://wrong.example/resource")
	if response.StatusCode != http.StatusBadRequest || payload["error"] != "invalid_target" {
		t.Fatalf("wrong refresh resource status=%d payload=%v", response.StatusCode, payload)
	}
	response, payload = refresh(refreshToken, server.URL+oauthFixturePrefix+"/mcp")
	if response.StatusCode != http.StatusOK || payload["refresh_token"] == refreshToken {
		t.Fatalf("refresh rotation status=%d payload=%v", response.StatusCode, payload)
	}
	response, payload = refresh(refreshToken, server.URL+oauthFixturePrefix+"/mcp")
	if response.StatusCode != http.StatusBadRequest || payload["error"] != "invalid_grant" {
		t.Fatalf("refresh replay status=%d payload=%v", response.StatusCode, payload)
	}
}

func TestOAuthFixtureRejectsAmbiguousPublicInputs(t *testing.T) {
	_, server, client := newOAuthFixtureServer(t)
	redirectURI := "https://client.example/callback?kept=yes&code=attacker&error=attacker"

	for _, body := range []string{
		`{"redirect_uris":["https://client.example/callback"]}`,
		`{"redirect_uris":["https://client.example/callback"],"token_endpoint_auth_method":"none"} {}`,
		`{"redirect_uris":["https://attacker.example/callback"],"redirect_uris":["https://client.example/callback"],"token_endpoint_auth_method":"client_secret_basic","token_endpoint_auth_method":"none"}`,
	} {
		response, err := client.Post(server.URL+oauthFixturePrefix+"/register", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("non-strict DCR accepted body %q: status=%d", body, response.StatusCode)
		}
	}

	clientID := registerFixtureClient(t, client, server.URL, []string{redirectURI})
	for _, states := range [][]string{nil, {"one", "two"}, {""}} {
		response := authorizeFixture(t, client, server.URL, clientID, redirectURI, url.Values{"state": states})
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("ambiguous state %v accepted", states)
		}
	}
	for _, scopes := range [][]string{{"admin"}, {"mcp", "admin"}, {""}} {
		response := authorizeFixture(t, client, server.URL, clientID, redirectURI, url.Values{"scope": scopes})
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest || response.Header.Get("Location") != "" {
			t.Fatalf("unsupported scope %v accepted", scopes)
		}
	}

	for _, extra := range []url.Values{nil, {"fixture_decision": {"deny"}}} {
		response := authorizeFixture(t, client, server.URL, clientID, redirectURI, extra)
		response.Body.Close()
		location, err := url.Parse(response.Header.Get("Location"))
		if err != nil {
			t.Fatal(err)
		}
		query := location.Query()
		if len(query["code"]) > 0 && len(query["error"]) > 0 {
			t.Fatalf("reserved callback ambiguity: %v", query)
		}
		if query.Get("kept") != "yes" {
			t.Fatalf("non-reserved callback query lost: %v", query)
		}
	}

	for _, test := range []struct {
		name      string
		mutate    func(url.Values)
		basicAuth bool
	}{
		{name: "duplicate grant type", mutate: func(values url.Values) {
			values["grant_type"] = []string{"authorization_code", "refresh_token"}
		}},
		{name: "client secret form", mutate: func(values url.Values) { values.Set("client_secret", "secret") }},
		{name: "basic authorization", mutate: func(url.Values) {}, basicAuth: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			authorization := authorizeFixture(t, client, server.URL, clientID, redirectURI, nil)
			location, _ := url.Parse(authorization.Header.Get("Location"))
			authorization.Body.Close()
			values := url.Values{
				"grant_type": {"authorization_code"}, "client_id": {clientID}, "redirect_uri": {redirectURI},
				"code": {location.Query().Get("code")}, "code_verifier": {fixtureVerifier},
				"resource": {server.URL + oauthFixturePrefix + "/mcp"},
			}
			test.mutate(values)
			request, err := http.NewRequest(http.MethodPost, server.URL+oauthFixturePrefix+"/token", strings.NewReader(values.Encode()))
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if test.basicAuth {
				request.SetBasicAuth("client", "secret")
			}
			response, err := client.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != http.StatusBadRequest {
				t.Fatalf("ambiguous/authenticated token request accepted: status=%d", response.StatusCode)
			}
		})
	}
}

func TestOAuthFixtureAuthenticatedMCP(t *testing.T) {
	_, server, client := newOAuthFixtureServer(t)
	redirectURI := "https://client.example/callback"
	clientID := registerFixtureClient(t, client, server.URL, []string{redirectURI})
	authorization := authorizeFixture(t, client, server.URL, clientID, redirectURI, nil)
	location, _ := url.Parse(authorization.Header.Get("Location"))
	authorization.Body.Close()
	response, tokens := exchangeFixtureCode(t, client, server.URL, clientID, redirectURI, location.Query().Get("code"), nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("token status=%d payload=%v", response.StatusCode, tokens)
	}
	accessToken := tokens["access_token"].(string)
	refreshToken := tokens["refresh_token"].(string)

	unauthorized, err := client.Post(server.URL+oauthFixturePrefix+"/mcp", "application/json", strings.NewReader(`{"jsonrpc":"2.0","id":"unauthorized","method":"server/discover"}`))
	if err != nil {
		t.Fatal(err)
	}
	unauthorized.Body.Close()
	if unauthorized.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d", unauthorized.StatusCode)
	}

	request := func(method, id string, params map[string]any, modern bool, session string) (map[string]json.RawMessage, http.Header) {
		t.Helper()
		if params == nil {
			params = make(map[string]any)
		}
		if modern {
			params["_meta"] = map[string]any{
				"io.modelcontextprotocol/protocolVersion":    modernProtocolVersion,
				"io.modelcontextprotocol/clientInfo":         map[string]any{"name": "oauth-fixture-test", "version": "1"},
				"io.modelcontextprotocol/clientCapabilities": map[string]any{},
			}
		}
		requestEnvelope := map[string]any{"jsonrpc": "2.0", "method": method, "params": params}
		if id != "" {
			requestEnvelope["id"] = id
		}
		body, _ := json.Marshal(requestEnvelope)
		req, err := http.NewRequest(http.MethodPost, server.URL+oauthFixturePrefix+"/mcp", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if modern || session != "" {
			req.Header.Set("Mcp-Protocol-Version", map[bool]string{true: modernProtocolVersion, false: "2025-06-18"}[modern])
		}
		if session != "" {
			req.Header.Set("Mcp-Session-Id", session)
		}
		if modern {
			req.Header.Set("Mcp-Method", method)
		}
		if modern && method == "tools/call" {
			req.Header.Set("Mcp-Name", "get_users")
		}
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		raw, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		expectedStatus := http.StatusOK
		if id == "" {
			expectedStatus = http.StatusAccepted
		}
		if response.StatusCode != expectedStatus {
			t.Fatalf("%s status=%d body=%s", method, response.StatusCode, raw)
		}
		if id == "" {
			return nil, response.Header
		}
		if strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
			_, data, _ := strings.Cut(string(raw), "data: ")
			raw = []byte(strings.TrimSpace(data))
		}
		var envelope map[string]json.RawMessage
		if err := json.Unmarshal(raw, &envelope); err != nil {
			t.Fatalf("%s decode: %v body=%s", method, err, raw)
		}
		if string(envelope["id"]) != fmt.Sprintf("%q", id) || envelope["error"] != nil {
			t.Fatalf("%s response=%s", method, raw)
		}
		return envelope, response.Header
	}

	request("server/discover", "discover", nil, true, "")
	_, initializeHeaders := request("initialize", "initialize", map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "oauth-fixture-test", "version": "1"},
	}, false, "")
	session := initializeHeaders.Get("Mcp-Session-Id")
	if session == "" {
		t.Fatal("legacy initialize did not return a session")
	}
	request("notifications/initialized", "", nil, false, session)
	request("tools/list", "list", nil, false, session)
	request("tools/call", "call", map[string]any{"name": "get_users", "arguments": map[string]any{}}, false, session)

	response, err = client.Get(server.URL + oauthFixturePrefix + "/counters")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var counters map[string]int
	if err := json.NewDecoder(response.Body).Decode(&counters); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"server/discover", "initialize", "tools/list", "tools/call"} {
		if counters["mcp:"+method] != 1 {
			t.Fatalf("%s count=%d counters=%v", method, counters["mcp:"+method], counters)
		}
	}

	response, err = client.Get(server.URL + oauthFixturePrefix + "/captures")
	if err != nil {
		t.Fatal(err)
	}
	captured, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	for name, secret := range map[string]string{
		"authorization code": location.Query().Get("code"),
		"access token":       accessToken,
		"refresh token":      refreshToken,
	} {
		if secret != "" && bytes.Contains(captured, []byte(secret)) {
			t.Fatalf("%s leaked into capture output", name)
		}
	}
	if !bytes.Contains(captured, []byte(`"authorization_present":true`)) {
		t.Fatal("authenticated MCP requests were not safely recorded")
	}
}

func TestOAuthFixtureAtomicCodeAndRefreshConsumption(t *testing.T) {
	_, server, client := newOAuthFixtureServer(t)
	redirectURI := "https://client.example/concurrent-callback"
	clientID := registerFixtureClient(t, client, server.URL, []string{redirectURI})
	authorization := authorizeFixture(t, client, server.URL, clientID, redirectURI, nil)
	location, _ := url.Parse(authorization.Header.Get("Location"))
	authorization.Body.Close()
	code := location.Query().Get("code")

	type result struct {
		status  int
		payload map[string]any
	}
	exchange := func(form url.Values) result {
		response, err := client.PostForm(server.URL+oauthFixturePrefix+"/token", form)
		if err != nil {
			return result{payload: map[string]any{"transport_error": err.Error()}}
		}
		defer response.Body.Close()
		var payload map[string]any
		_ = json.NewDecoder(response.Body).Decode(&payload)
		return result{status: response.StatusCode, payload: payload}
	}
	runConcurrent := func(form url.Values) []result {
		results := make(chan result, 12)
		var wait sync.WaitGroup
		for range 12 {
			wait.Add(1)
			go func() {
				defer wait.Done()
				results <- exchange(form)
			}()
		}
		wait.Wait()
		close(results)
		returnResults := make([]result, 0, 12)
		for result := range results {
			returnResults = append(returnResults, result)
		}
		return returnResults
	}

	codeResults := runConcurrent(url.Values{
		"grant_type": {"authorization_code"}, "client_id": {clientID},
		"redirect_uri": {redirectURI}, "code": {code}, "code_verifier": {fixtureVerifier},
		"resource": {server.URL + oauthFixturePrefix + "/mcp"},
	})
	winners := 0
	refreshToken := ""
	for _, result := range codeResults {
		if result.status == http.StatusOK {
			winners++
			refreshToken, _ = result.payload["refresh_token"].(string)
		} else if result.status != http.StatusBadRequest || result.payload["error"] != "invalid_grant" {
			t.Fatalf("unexpected code result: %+v", result)
		}
	}
	if winners != 1 || refreshToken == "" {
		t.Fatalf("code winners=%d refresh=%q", winners, refreshToken)
	}

	refreshResults := runConcurrent(url.Values{
		"grant_type": {"refresh_token"}, "client_id": {clientID},
		"refresh_token": {refreshToken}, "resource": {server.URL + oauthFixturePrefix + "/mcp"},
	})
	winners = 0
	for _, result := range refreshResults {
		if result.status == http.StatusOK {
			winners++
		} else if result.status != http.StatusBadRequest || result.payload["error"] != "invalid_grant" {
			t.Fatalf("unexpected refresh result: %+v", result)
		}
	}
	if winners != 1 {
		t.Fatalf("refresh winners=%d", winners)
	}
}
