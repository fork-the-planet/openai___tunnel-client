package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestFetchAuthServerMetadata(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	payload := map[string]any{
		"issuer":                 server.URL,
		"authorization_endpoint": server.URL + "/authorize",
		"token_endpoint":         server.URL + "/token",
		"jwks_uri":               server.URL + "/jwks",
		"registration_endpoint":  server.URL + "/register",
		"introspection_endpoint": server.URL + "/introspect",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})

	meta, err := FetchAuthServerMetadata(context.Background(), server.Client(), server.URL)
	if err != nil {
		t.Fatalf("FetchAuthServerMetadata returned error: %v", err)
	}
	if meta == nil {
		t.Fatalf("expected metadata")
		return
	}
	if meta.Issuer != server.URL {
		t.Fatalf("issuer mismatch: got %q want %q", meta.Issuer, server.URL)
	}
	if meta.AuthorizationEndpoint != server.URL+"/authorize" {
		t.Fatalf("authorization_endpoint mismatch: got %q", meta.AuthorizationEndpoint)
	}
	if meta.TokenEndpoint != server.URL+"/token" {
		t.Fatalf("token_endpoint mismatch: got %q", meta.TokenEndpoint)
	}
	if meta.JWKSURI != server.URL+"/jwks" {
		t.Fatalf("jwks_uri mismatch: got %q", meta.JWKSURI)
	}
	if meta.IntrospectionEndpoint != server.URL+"/introspect" {
		t.Fatalf("introspection_endpoint mismatch: got %q", meta.IntrospectionEndpoint)
	}
	if meta.RegistrationEndpoint != server.URL+"/register" {
		t.Fatalf("registration_endpoint mismatch: got %q", meta.RegistrationEndpoint)
	}
}

func TestFetchAuthServerMetadataFallsBackToOIDCWellKnown(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	payload := map[string]any{
		"issuer":                 server.URL,
		"authorization_endpoint": server.URL + "/authorize",
		"token_endpoint":         server.URL + "/token",
		"jwks_uri":               server.URL + "/jwks",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	})
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})

	meta, err := FetchAuthServerMetadata(context.Background(), server.Client(), server.URL)
	if err != nil {
		t.Fatalf("FetchAuthServerMetadata returned error: %v", err)
	}
	if meta == nil {
		t.Fatalf("expected metadata")
		return
	}
	if meta.Issuer != server.URL {
		t.Fatalf("issuer mismatch: got %q want %q", meta.Issuer, server.URL)
	}
}

func TestFetchAuthServerMetadataSupportsAppendStyleOIDCPath(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	issuerURL := server.URL + "/tenant/v2.0"
	payload := map[string]any{
		"issuer":                 issuerURL,
		"authorization_endpoint": issuerURL + "/authorize",
		"token_endpoint":         issuerURL + "/token",
		"jwks_uri":               issuerURL + "/jwks",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	// Simulates providers like Azure AD that serve:
	//   /{tenant}/v2.0/.well-known/openid-configuration
	mux.HandleFunc("/tenant/v2.0/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(body)
	})

	meta, err := FetchAuthServerMetadata(context.Background(), server.Client(), issuerURL)
	if err != nil {
		t.Fatalf("FetchAuthServerMetadata returned error: %v", err)
	}
	if meta == nil {
		t.Fatalf("expected metadata")
		return
	}
	if meta.Issuer != issuerURL {
		t.Fatalf("issuer mismatch: got %q want %q", meta.Issuer, issuerURL)
	}
	if meta.TokenEndpoint != issuerURL+"/token" {
		t.Fatalf("token_endpoint mismatch: got %q", meta.TokenEndpoint)
	}
}

func TestFetchAuthServerMetadataWithResultIncludesAttempts(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	issuerURL := server.URL + "/tenant/v2.0"
	successPath := "/tenant/v2.0/.well-known/openid-configuration"

	mux.HandleFunc("/.well-known/oauth-authorization-server/tenant/v2.0", func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	})
	mux.HandleFunc("/tenant/v2.0/.well-known/oauth-authorization-server", func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	})
	mux.HandleFunc("/.well-known/openid-configuration/tenant/v2.0", func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	})
	mux.HandleFunc(successPath, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"issuer":"` + issuerURL + `",
			"token_endpoint":"` + issuerURL + `/token"
		}`))
	})

	meta, result, err := FetchAuthServerMetadataWithResult(context.Background(), server.Client(), issuerURL)
	if err != nil {
		t.Fatalf("FetchAuthServerMetadataWithResult returned error: %v", err)
	}
	if meta == nil {
		t.Fatalf("expected metadata")
		return
	}
	if result == nil {
		t.Fatalf("expected fetch result")
		return
	}
	if result.SelectedURL != server.URL+successPath {
		t.Fatalf("unexpected selected URL: got %q want %q", result.SelectedURL, server.URL+successPath)
	}
	if len(result.Attempts) == 0 {
		t.Fatalf("expected attempts to be populated")
	}
	last := result.Attempts[len(result.Attempts)-1]
	if !last.Selected {
		t.Fatalf("expected final attempt to be selected")
	}
	if last.PathStyle != "append" {
		t.Fatalf("unexpected path style: got %q want %q", last.PathStyle, "append")
	}
	if last.Document != "openid-configuration" {
		t.Fatalf("unexpected document: got %q want %q", last.Document, "openid-configuration")
	}
	if last.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status code: got %d want %d", last.StatusCode, http.StatusOK)
	}
	if len(last.Body) == 0 {
		t.Fatalf("expected selected attempt body")
	}
}

func TestFetchAuthServerMetadataRetriesOnlyAfterAllTimeouts(t *testing.T) {
	issuerURL := "https://issuer.example.com/tenant/v2.0"
	parsedIssuer, err := url.Parse(issuerURL)
	if err != nil {
		t.Fatalf("parse issuer URL: %v", err)
	}

	candidates, err := buildAuthServerMetadataCandidateURLs(issuerURL)
	if err != nil {
		t.Fatalf("build candidates: %v", err)
	}
	if len(candidates) == 0 {
		t.Fatalf("expected candidates")
	}

	var calls int
	client := &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			if req.URL.Host != parsedIssuer.Host {
				t.Fatalf("unexpected host: got %q want %q", req.URL.Host, parsedIssuer.Host)
			}
			return nil, context.DeadlineExceeded
		}),
	}

	meta, result, fetchErr := FetchAuthServerMetadataWithResult(context.Background(), client, issuerURL)
	if fetchErr == nil {
		t.Fatal("expected fetch error")
	}
	if meta != nil {
		t.Fatal("expected nil metadata on timeout failures")
	}
	if result == nil {
		t.Fatal("expected result")
		return
	}
	if len(result.Attempts) != len(candidates) {
		t.Fatalf("expected attempts from retry pass only: got %d want %d", len(result.Attempts), len(candidates))
	}

	expectedCalls := len(candidates) + len(candidates)*oauthMetadataRequestRetryCount
	if calls != expectedCalls {
		t.Fatalf("unexpected call count: got %d want %d", calls, expectedCalls)
	}
	for _, attempt := range result.Attempts {
		if attempt.Error == "" {
			t.Fatalf("expected timeout error in attempt %+v", attempt)
		}
		if !errors.Is(fetchErr, context.DeadlineExceeded) {
			t.Fatalf("expected joined timeout error, got %v", fetchErr)
		}
	}
}

func TestBuildAuthServerMetadataCandidateURLsPathfulIssuerPrefersAppendStyle(t *testing.T) {
	issuerURL := "https://example.com/tenant/v2.0"
	candidates, err := buildAuthServerMetadataCandidateURLs(issuerURL)
	if err != nil {
		t.Fatalf("build candidates: %v", err)
	}
	if len(candidates) < 4 {
		t.Fatalf("expected at least 4 candidates, got %d", len(candidates))
	}

	firstByDoc := map[AuthServerMetadataDocument]AuthServerMetadataPathStyle{}
	for _, candidate := range candidates {
		if _, seen := firstByDoc[candidate.Document]; seen {
			continue
		}
		firstByDoc[candidate.Document] = candidate.PathStyle
	}
	for _, doc := range []AuthServerMetadataDocument{
		AuthServerMetadataDocumentOAuthAuthorizationServer,
		AuthServerMetadataDocumentOpenIDConfiguration,
	} {
		style, ok := firstByDoc[doc]
		if !ok {
			t.Fatalf("missing candidate for document %q", doc)
		}
		if style != AuthServerMetadataPathStyleAppend {
			t.Fatalf("first path style for %q = %q, want %q", doc, style, AuthServerMetadataPathStyleAppend)
		}
	}
}
