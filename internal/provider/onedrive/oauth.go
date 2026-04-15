package onedrive

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/FloG309/cloud-storage-freeloader/internal/config"
)

const defaultTokenEndpoint = "https://login.microsoftonline.com/common/oauth2/v2.0/token"

// tokenSource manages OAuth2 tokens with auto-refresh for public clients.
type tokenSource struct {
	mu            sync.Mutex
	clientID      string
	tokenEndpoint string
	tokensFile    string
	accessToken   string
	refreshToken  string
	expiry        time.Time
	httpClient    *http.Client
}

// newTokenSource loads tokens from disk and returns a tokenSource that
// auto-refreshes the access token when expired.
func newTokenSource(clientID, tokensFile, tokenEndpoint string) (*tokenSource, error) {
	if tokenEndpoint == "" {
		tokenEndpoint = defaultTokenEndpoint
	}

	td, err := config.LoadTokens(tokensFile)
	if err != nil {
		return nil, fmt.Errorf("onedrive: load tokens: %w", err)
	}

	ts := &tokenSource{
		clientID:      clientID,
		tokenEndpoint: tokenEndpoint,
		tokensFile:    tokensFile,
		accessToken:   td.AccessToken,
		refreshToken:  td.RefreshToken,
		expiry:        time.Now().Add(time.Duration(td.ExpiresIn) * time.Second),
		httpClient:    &http.Client{Timeout: 30 * time.Second},
	}

	return ts, nil
}

// Token returns a valid access token, refreshing if necessary.
func (ts *tokenSource) Token() (string, error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	// Refresh 60 seconds before expiry
	if time.Now().Before(ts.expiry.Add(-60 * time.Second)) {
		return ts.accessToken, nil
	}

	if err := ts.refresh(); err != nil {
		return "", err
	}
	return ts.accessToken, nil
}

// refresh exchanges the refresh token for a new access token.
// Public client: no client_secret.
func (ts *tokenSource) refresh() error {
	data := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {ts.clientID},
		"refresh_token": {ts.refreshToken},
	}

	resp, err := ts.httpClient.Post(ts.tokenEndpoint, "application/x-www-form-urlencoded", strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("onedrive: refresh request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("onedrive: read refresh response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("onedrive: refresh failed (%d): %s", resp.StatusCode, body)
	}

	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("onedrive: parse refresh response: %w", err)
	}

	ts.accessToken = result.AccessToken
	if result.RefreshToken != "" {
		ts.refreshToken = result.RefreshToken
	}
	ts.expiry = time.Now().Add(time.Duration(result.ExpiresIn) * time.Second)

	// Persist to disk
	td := &config.TokenData{
		AccessToken:  ts.accessToken,
		RefreshToken: ts.refreshToken,
		ExpiresIn:    result.ExpiresIn,
	}
	if err := config.SaveTokens(ts.tokensFile, td); err != nil {
		// Log but don't fail — token is still valid in memory
		fmt.Printf("onedrive: warning: failed to save tokens: %v\n", err)
	}

	return nil
}

// authTransport is an http.RoundTripper that adds the Bearer token.
type authTransport struct {
	ts   *tokenSource
	base http.RoundTripper
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	token, err := t.ts.Token()
	if err != nil {
		return nil, fmt.Errorf("onedrive: get token: %w", err)
	}
	req2 := req.Clone(req.Context())
	req2.Header.Set("Authorization", "Bearer "+token)
	return t.base.RoundTrip(req2)
}

// newAuthClient creates an http.Client that automatically adds auth headers.
func newAuthClient(ts *tokenSource) *http.Client {
	return &http.Client{
		Transport: &authTransport{
			ts:   ts,
			base: http.DefaultTransport,
		},
		Timeout: 5 * time.Minute,
	}
}
