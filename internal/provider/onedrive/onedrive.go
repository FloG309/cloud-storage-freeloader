package onedrive

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/FloG309/cloud-storage-freeloader/internal/provider"
)

const (
	defaultGraphBase  = "https://graph.microsoft.com/v1.0"
	defaultBaseFolder = "cloud-storage-freeloader"
	smallFileLimit    = 4 * 1024 * 1024 // 4MB
)

// Backend implements provider.StorageBackend for Microsoft OneDrive.
// All operations are scoped to the cloud-storage-freeloader/ folder.
type Backend struct {
	store      provider.StorageBackend // for unit tests (NewWithStore)
	httpClient *http.Client            // for real Graph API
	graphBase  string                  // Graph API base URL
	baseFolder string                  // folder on OneDrive
}

// NewWithStore creates a OneDrive backend backed by the given store (for testing).
func NewWithStore(store provider.StorageBackend) *Backend {
	return &Backend{store: store}
}

// NewFromConfig creates a real OneDrive backend from configuration.
func NewFromConfig(cfg map[string]string) (*Backend, error) {
	clientID := cfg["client_id"]
	if clientID == "" {
		return nil, fmt.Errorf("onedrive: client_id is required")
	}

	tokensFile := cfg["tokens_file"]
	if tokensFile == "" {
		return nil, fmt.Errorf("onedrive: tokens_file is required")
	}

	baseFolder := cfg["base_folder"]
	if baseFolder == "" {
		baseFolder = defaultBaseFolder
	}

	tokenEndpoint := cfg["auth_endpoint"]

	ts, err := newTokenSource(clientID, tokensFile, tokenEndpoint)
	if err != nil {
		return nil, err
	}

	return &Backend{
		httpClient: newAuthClient(ts),
		graphBase:  defaultGraphBase,
		baseFolder: baseFolder,
	}, nil
}

// mapKey replaces "/" with "__" for flat file names on OneDrive.
func mapKey(key string) string {
	return strings.ReplaceAll(key, "/", "__")
}

// unmapKey reverses mapKey, converting "__" back to "/".
func unmapKey(name string) string {
	return strings.ReplaceAll(name, "__", "/")
}

func (b *Backend) Put(ctx context.Context, key string, data []byte) error {
	if b.store != nil {
		return b.store.Put(ctx, key, data)
	}
	name := mapKey(key)

	if len(data) < smallFileLimit {
		return b.putSmall(ctx, name, data)
	}
	return b.putLarge(ctx, name, data)
}

func (b *Backend) putSmall(ctx context.Context, name string, data []byte) error {
	url := fmt.Sprintf("%s/me/drive/root:/%s/%s:/content", b.graphBase, b.baseFolder, name)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("onedrive put %s: %w", name, err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("onedrive put %s: status %d", name, resp.StatusCode)
	}
	return nil
}

func (b *Backend) putLarge(ctx context.Context, name string, data []byte) error {
	// Create upload session
	url := fmt.Sprintf("%s/me/drive/root:/%s/%s:/createUploadSession", b.graphBase, b.baseFolder, name)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("onedrive create upload session: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("onedrive create upload session: status %d: %s", resp.StatusCode, body)
	}

	var session struct {
		UploadURL string `json:"uploadUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		return fmt.Errorf("onedrive parse upload session: %w", err)
	}

	// Upload in chunks (10MB each, as recommended by Microsoft)
	const chunkSize = 10 * 1024 * 1024
	total := len(data)
	for offset := 0; offset < total; offset += chunkSize {
		end := offset + chunkSize
		if end > total {
			end = total
		}
		chunk := data[offset:end]

		chunkReq, err := http.NewRequestWithContext(ctx, http.MethodPut, session.UploadURL, bytes.NewReader(chunk))
		if err != nil {
			return err
		}
		chunkReq.Header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", offset, end-1, total))
		chunkReq.Header.Set("Content-Type", "application/octet-stream")

		chunkResp, err := b.httpClient.Do(chunkReq)
		if err != nil {
			return fmt.Errorf("onedrive upload chunk: %w", err)
		}
		io.Copy(io.Discard, chunkResp.Body)
		chunkResp.Body.Close()

		// 202 = more chunks, 200/201 = done
		if chunkResp.StatusCode != http.StatusAccepted &&
			chunkResp.StatusCode != http.StatusOK &&
			chunkResp.StatusCode != http.StatusCreated {
			return fmt.Errorf("onedrive upload chunk: status %d", chunkResp.StatusCode)
		}
	}
	return nil
}

func (b *Backend) Get(ctx context.Context, key string) ([]byte, error) {
	if b.store != nil {
		return b.store.Get(ctx, key)
	}
	name := mapKey(key)
	url := fmt.Sprintf("%s/me/drive/root:/%s/%s:/content", b.graphBase, b.baseFolder, name)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("onedrive get %s: %w", key, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("onedrive get %s: not found", key)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("onedrive get %s: status %d", key, resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

func (b *Backend) Delete(ctx context.Context, key string) error {
	if b.store != nil {
		return b.store.Delete(ctx, key)
	}
	name := mapKey(key)

	// First, get the item ID
	itemURL := fmt.Sprintf("%s/me/drive/root:/%s/%s:", b.graphBase, b.baseFolder, name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, itemURL, nil)
	if err != nil {
		return err
	}
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("onedrive delete get-id %s: %w", key, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil // already gone
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("onedrive delete get-id %s: status %d", key, resp.StatusCode)
	}

	var item struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&item); err != nil {
		return fmt.Errorf("onedrive delete parse item: %w", err)
	}

	// Delete by ID
	delURL := fmt.Sprintf("%s/me/drive/items/%s", b.graphBase, item.ID)
	delReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, delURL, nil)
	if err != nil {
		return err
	}
	delResp, err := b.httpClient.Do(delReq)
	if err != nil {
		return fmt.Errorf("onedrive delete %s: %w", key, err)
	}
	defer delResp.Body.Close()
	io.Copy(io.Discard, delResp.Body)

	if delResp.StatusCode != http.StatusNoContent && delResp.StatusCode != http.StatusOK {
		return fmt.Errorf("onedrive delete %s: status %d", key, delResp.StatusCode)
	}
	return nil
}

func (b *Backend) Exists(ctx context.Context, key string) (bool, error) {
	if b.store != nil {
		return b.store.Exists(ctx, key)
	}
	name := mapKey(key)
	url := fmt.Sprintf("%s/me/drive/root:/%s/%s:", b.graphBase, b.baseFolder, name)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("onedrive exists %s: %w", key, err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode == http.StatusOK {
		return true, nil
	}
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	return false, fmt.Errorf("onedrive exists %s: status %d", key, resp.StatusCode)
}

func (b *Backend) List(ctx context.Context, prefix string) ([]string, error) {
	if b.store != nil {
		return b.store.List(ctx, prefix)
	}
	url := fmt.Sprintf("%s/me/drive/root:/%s:/children", b.graphBase, b.baseFolder)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("onedrive list: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("onedrive list: status %d", resp.StatusCode)
	}

	var result struct {
		Value []struct {
			Name string `json:"name"`
		} `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("onedrive list parse: %w", err)
	}

	mappedPrefix := mapKey(prefix)
	var keys []string
	for _, item := range result.Value {
		if strings.HasPrefix(item.Name, mappedPrefix) {
			keys = append(keys, unmapKey(item.Name))
		}
	}
	return keys, nil
}

func (b *Backend) Available(ctx context.Context) (int64, error) {
	if b.store != nil {
		return b.store.Available(ctx)
	}
	url := fmt.Sprintf("%s/me/drive", b.graphBase)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("onedrive available: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("onedrive available: status %d", resp.StatusCode)
	}

	var result struct {
		Quota struct {
			Remaining int64 `json:"remaining"`
		} `json:"quota"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("onedrive available parse: %w", err)
	}

	return result.Quota.Remaining, nil
}

func (b *Backend) Close() error {
	if b.store != nil {
		return b.store.Close()
	}
	return nil
}

// Profile returns OneDrive's constraint profile.
func (b *Backend) Profile() provider.ProviderProfile {
	return provider.ProviderProfile{
		DailyEgressLimit: 1 * 1024 * 1024 * 1024, // 1GB egress limit → warm tier
		MaxFileSize:      0,
	}
}

// ensureBaseFolder checks if the base folder exists and creates it if not.
func (b *Backend) ensureBaseFolder(ctx context.Context) error {
	checkURL := fmt.Sprintf("%s/me/drive/root:/%s", b.graphBase, b.baseFolder)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checkURL, nil)
	if err != nil {
		return err
	}

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("onedrive check folder: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode == http.StatusOK {
		return nil // folder exists
	}

	// Create the folder
	createURL := fmt.Sprintf("%s/me/drive/root/children", b.graphBase)
	body := map[string]interface{}{
		"name":                              b.baseFolder,
		"folder":                            map[string]interface{}{},
		"@microsoft.graph.conflictBehavior": "fail",
	}
	bodyBytes, _ := json.Marshal(body)

	createReq, err := http.NewRequestWithContext(ctx, http.MethodPost, createURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	createReq.Header.Set("Content-Type", "application/json")

	createResp, err := b.httpClient.Do(createReq)
	if err != nil {
		return fmt.Errorf("onedrive create folder: %w", err)
	}
	defer createResp.Body.Close()
	io.Copy(io.Discard, createResp.Body)

	if createResp.StatusCode != http.StatusCreated && createResp.StatusCode != http.StatusOK {
		return fmt.Errorf("onedrive create folder: status %d", createResp.StatusCode)
	}
	return nil
}

var _ provider.StorageBackend = (*Backend)(nil)
