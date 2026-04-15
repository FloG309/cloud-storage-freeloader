package gdrive

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/FloG309/cloud-storage-freeloader/internal/config"
)

const googleTokenEndpoint = "https://oauth2.googleapis.com/token"

// tokenSource provides auto-refreshing OAuth2 tokens, persisted to disk.
type tokenSource struct {
	clientID     string
	clientSecret string
	tokensFile   string

	accessToken  string
	refreshToken string
	expiry       time.Time
}

// newTokenSource creates a token source from stored tokens.
func newTokenSource(clientID, clientSecret, tokensFile string) (*tokenSource, error) {
	td, err := config.LoadTokens(tokensFile)
	if err != nil {
		return nil, fmt.Errorf("load tokens: %w", err)
	}
	ts := &tokenSource{
		clientID:     clientID,
		clientSecret: clientSecret,
		tokensFile:   tokensFile,
		accessToken:  td.AccessToken,
		refreshToken: td.RefreshToken,
		expiry:       time.Now().Add(time.Duration(td.ExpiresIn) * time.Second),
	}
	return ts, nil
}

// Token returns a valid access token, refreshing if needed.
func (ts *tokenSource) Token() (string, error) {
	if time.Now().Before(ts.expiry.Add(-60 * time.Second)) {
		return ts.accessToken, nil
	}
	return ts.refresh()
}

func (ts *tokenSource) refresh() (string, error) {
	data := url.Values{
		"client_id":     {ts.clientID},
		"client_secret": {ts.clientSecret},
		"refresh_token": {ts.refreshToken},
		"grant_type":    {"refresh_token"},
	}

	resp, err := http.PostForm(googleTokenEndpoint, data)
	if err != nil {
		return "", fmt.Errorf("refresh request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read refresh response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("refresh failed (%d): %s", resp.StatusCode, body)
	}

	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parse refresh response: %w", err)
	}

	ts.accessToken = result.AccessToken
	ts.expiry = time.Now().Add(time.Duration(result.ExpiresIn) * time.Second)

	// Persist updated token
	td := &config.TokenData{
		AccessToken:  ts.accessToken,
		RefreshToken: ts.refreshToken,
		ExpiresIn:    result.ExpiresIn,
	}
	if err := config.SaveTokens(ts.tokensFile, td); err != nil {
		return "", fmt.Errorf("save tokens: %w", err)
	}

	return ts.accessToken, nil
}

// oauthTransport is an http.RoundTripper that adds Bearer auth.
type oauthTransport struct {
	base   http.RoundTripper
	source *tokenSource
}

func (t *oauthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	token, err := t.source.Token()
	if err != nil {
		return nil, err
	}
	r := req.Clone(req.Context())
	r.Header.Set("Authorization", "Bearer "+token)
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(r)
}

// newOAuthClient creates an HTTP client with OAuth2 Bearer token auto-refresh.
func newOAuthClient(clientID, clientSecret, tokensFile string) (*http.Client, error) {
	ts, err := newTokenSource(clientID, clientSecret, tokensFile)
	if err != nil {
		return nil, err
	}
	return &http.Client{
		Transport: &oauthTransport{source: ts},
	}, nil
}

// roundTripperFunc adapts a function into http.RoundTripper.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// newRedirectClient creates an HTTP client that rewrites requests to hit baseURL
// instead of the real Google API. Used for testing with httptest.
func newRedirectClient(inner *http.Client, baseURL string) *http.Client {
	return &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			// Rewrite URL to point at the test server
			req.URL.Scheme = "http"
			req.URL.Host = strings.TrimPrefix(baseURL, "http://")
			if inner.Transport != nil {
				return inner.Transport.RoundTrip(req)
			}
			return http.DefaultTransport.RoundTrip(req)
		}),
	}
}
