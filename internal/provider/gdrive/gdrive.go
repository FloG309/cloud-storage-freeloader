package gdrive

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"

	"github.com/FloG309/cloud-storage-freeloader/internal/provider"
)

const driveAPIBase = "https://www.googleapis.com"

// Backend implements provider.StorageBackend for Google Drive.
// All operations are scoped to the cloud-storage-freeloader/ folder.
//
// When store is non-nil (NewWithStore), all calls delegate to the in-memory store.
// When httpClient is non-nil (NewFromConfig/NewFromClient), calls hit Drive API v3.
type Backend struct {
	store      provider.StorageBackend
	httpClient *http.Client
	apiBase    string // e.g. "https://www.googleapis.com"
	baseFolder string // folder name, e.g. "cloud-storage-freeloader"

	baseFolderID string // cached folder ID
}

// NewWithStore creates a Google Drive backend backed by the given store (for testing).
func NewWithStore(store provider.StorageBackend) *Backend {
	return &Backend{store: store}
}

// NewFromConfig creates a real Google Drive backend using OAuth2 credentials.
func NewFromConfig(cfg map[string]string) (*Backend, error) {
	clientID := cfg["client_id"]
	clientSecret := cfg["client_secret"]
	tokensFile := cfg["tokens_file"]
	baseFolder := cfg["base_folder"]
	if baseFolder == "" {
		baseFolder = "cloud-storage-freeloader"
	}

	client, err := newOAuthClient(clientID, clientSecret, tokensFile)
	if err != nil {
		return nil, fmt.Errorf("gdrive oauth: %w", err)
	}

	return &Backend{
		httpClient: client,
		apiBase:    driveAPIBase,
		baseFolder: baseFolder,
	}, nil
}

// NewFromClient creates a backend with the given HTTP client and API base URL.
// Useful for testing with httptest.
func NewFromClient(client *http.Client, apiBase string, baseFolder string) *Backend {
	return &Backend{
		httpClient: newRedirectClient(client, apiBase),
		apiBase:    driveAPIBase, // URL gets rewritten by redirectClient
		baseFolder: baseFolder,
	}
}

// keyToFileName maps a key like "shards/file/seg0/shard1" to "shards__file__seg0__shard1".
func keyToFileName(key string) string {
	return strings.ReplaceAll(key, "/", "__")
}

// fileNameToKey reverses the key mapping.
func fileNameToKey(name string) string {
	return strings.ReplaceAll(name, "__", "/")
}

// ensureBaseFolder finds or creates the base folder and caches its ID.
func (b *Backend) ensureBaseFolder(ctx context.Context) (string, error) {
	if b.baseFolderID != "" {
		return b.baseFolderID, nil
	}

	// Search for existing folder
	q := fmt.Sprintf("name = '%s' and mimeType = 'application/vnd.google-apps.folder' and trashed = false", b.baseFolder)
	u := fmt.Sprintf("%s/drive/v3/files?q=%s", b.apiBase, url.QueryEscape(q))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("search base folder: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Files []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"files"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode folder search: %w", err)
	}

	if len(result.Files) > 0 {
		b.baseFolderID = result.Files[0].ID
		return b.baseFolderID, nil
	}

	// Create the folder
	folderID, err := b.createFolder(ctx, b.baseFolder)
	if err != nil {
		return "", err
	}
	b.baseFolderID = folderID
	return b.baseFolderID, nil
}

func (b *Backend) createFolder(ctx context.Context, name string) (string, error) {
	meta := map[string]interface{}{
		"name":     name,
		"mimeType": "application/vnd.google-apps.folder",
	}
	metaBytes, _ := json.Marshal(meta)

	// Use multipart upload for folder creation
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	metaHeader := make(textproto.MIMEHeader)
	metaHeader.Set("Content-Type", "application/json")
	part, err := w.CreatePart(metaHeader)
	if err != nil {
		return "", err
	}
	part.Write(metaBytes)

	// Empty content part
	contentHeader := make(textproto.MIMEHeader)
	contentHeader.Set("Content-Type", "application/octet-stream")
	part, err = w.CreatePart(contentHeader)
	if err != nil {
		return "", err
	}
	part.Write([]byte{})
	w.Close()

	u := fmt.Sprintf("%s/upload/drive/v3/files?uploadType=multipart", b.apiBase)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "multipart/related; boundary="+w.Boundary())

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("create folder: %w", err)
	}
	defer resp.Body.Close()

	var created struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return "", err
	}
	return created.ID, nil
}

// findFile searches for a file by name in the base folder. Returns file ID or "".
func (b *Backend) findFile(ctx context.Context, fileName string) (string, error) {
	folderID, err := b.ensureBaseFolder(ctx)
	if err != nil {
		return "", err
	}

	q := fmt.Sprintf("name = '%s' and '%s' in parents and trashed = false", fileName, folderID)
	u := fmt.Sprintf("%s/drive/v3/files?q=%s", b.apiBase, url.QueryEscape(q))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("find file: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Files []struct {
			ID string `json:"id"`
		} `json:"files"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if len(result.Files) > 0 {
		return result.Files[0].ID, nil
	}
	return "", nil
}

func (b *Backend) Put(ctx context.Context, key string, data []byte) error {
	if b.store != nil {
		return b.store.Put(ctx, key, data)
	}

	fileName := keyToFileName(key)

	// Delete existing file if present (overwrite semantics)
	existingID, err := b.findFile(ctx, fileName)
	if err != nil {
		return err
	}
	if existingID != "" {
		if err := b.deleteByID(ctx, existingID); err != nil {
			return fmt.Errorf("delete existing: %w", err)
		}
	}

	folderID, err := b.ensureBaseFolder(ctx)
	if err != nil {
		return err
	}

	// Multipart upload
	meta := map[string]interface{}{
		"name":    fileName,
		"parents": []string{folderID},
	}
	metaBytes, _ := json.Marshal(meta)

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	metaHeader := make(textproto.MIMEHeader)
	metaHeader.Set("Content-Type", "application/json")
	part, err := w.CreatePart(metaHeader)
	if err != nil {
		return err
	}
	part.Write(metaBytes)

	contentHeader := make(textproto.MIMEHeader)
	contentHeader.Set("Content-Type", "application/octet-stream")
	part, err = w.CreatePart(contentHeader)
	if err != nil {
		return err
	}
	part.Write(data)
	w.Close()

	u := fmt.Sprintf("%s/upload/drive/v3/files?uploadType=multipart", b.apiBase)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "multipart/related; boundary="+w.Boundary())

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("upload: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("upload failed (%d): %s", resp.StatusCode, body)
	}
	return nil
}

func (b *Backend) Get(ctx context.Context, key string) ([]byte, error) {
	if b.store != nil {
		return b.store.Get(ctx, key)
	}

	fileName := keyToFileName(key)
	fileID, err := b.findFile(ctx, fileName)
	if err != nil {
		return nil, err
	}
	if fileID == "" {
		return nil, fmt.Errorf("file not found: %s", key)
	}

	u := fmt.Sprintf("%s/drive/v3/files/%s?alt=media", b.apiBase, fileID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("download failed (%d): %s", resp.StatusCode, body)
	}
	return io.ReadAll(resp.Body)
}

func (b *Backend) Delete(ctx context.Context, key string) error {
	if b.store != nil {
		return b.store.Delete(ctx, key)
	}

	fileName := keyToFileName(key)
	fileID, err := b.findFile(ctx, fileName)
	if err != nil {
		return err
	}
	if fileID == "" {
		return nil // already gone
	}
	return b.deleteByID(ctx, fileID)
}

func (b *Backend) deleteByID(ctx context.Context, fileID string) error {
	u := fmt.Sprintf("%s/drive/v3/files/%s", b.apiBase, fileID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u, nil)
	if err != nil {
		return err
	}
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("delete: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("delete failed (%d)", resp.StatusCode)
	}
	return nil
}

func (b *Backend) Exists(ctx context.Context, key string) (bool, error) {
	if b.store != nil {
		return b.store.Exists(ctx, key)
	}

	fileName := keyToFileName(key)
	fileID, err := b.findFile(ctx, fileName)
	if err != nil {
		return false, err
	}
	return fileID != "", nil
}

func (b *Backend) List(ctx context.Context, prefix string) ([]string, error) {
	if b.store != nil {
		return b.store.List(ctx, prefix)
	}

	folderID, err := b.ensureBaseFolder(ctx)
	if err != nil {
		return nil, err
	}

	filePrefix := keyToFileName(prefix)
	q := fmt.Sprintf("'%s' in parents and trashed = false", folderID)
	if filePrefix != "" {
		q += fmt.Sprintf(" and name contains '%s'", filePrefix)
	}
	u := fmt.Sprintf("%s/drive/v3/files?q=%s&pageSize=1000", b.apiBase, url.QueryEscape(q))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Files []struct {
			Name string `json:"name"`
		} `json:"files"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	var keys []string
	for _, f := range result.Files {
		key := fileNameToKey(f.Name)
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

func (b *Backend) Available(ctx context.Context) (int64, error) {
	if b.store != nil {
		return b.store.Available(ctx)
	}

	u := fmt.Sprintf("%s/drive/v3/about?fields=storageQuota", b.apiBase)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, err
	}
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("about: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		StorageQuota struct {
			Limit string `json:"limit"`
			Usage string `json:"usage"`
		} `json:"storageQuota"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}

	limit, err := strconv.ParseInt(result.StorageQuota.Limit, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse limit: %w", err)
	}
	usage, err := strconv.ParseInt(result.StorageQuota.Usage, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse usage: %w", err)
	}
	return limit - usage, nil
}

func (b *Backend) Close() error {
	if b.store != nil {
		return b.store.Close()
	}
	return nil
}

// Profile returns Google Drive's constraint profile.
func (b *Backend) Profile() provider.ProviderProfile {
	return provider.ProviderProfile{
		DailyEgressLimit: 0, // Google Drive has generous limits
		MaxFileSize:      0,
	}
}

var _ provider.StorageBackend = (*Backend)(nil)
