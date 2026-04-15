package gdrive

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// mockDriveServer simulates the Google Drive API v3 for testing.
type mockDriveServer struct {
	mu    sync.Mutex
	files map[string]*mockFile // id -> file
	nextID int
}

type mockFile struct {
	ID      string
	Name    string
	Parents []string
	Data    []byte
	MimeType string
}

func newMockDriveServer() *mockDriveServer {
	return &mockDriveServer{
		files:  make(map[string]*mockFile),
		nextID: 1,
	}
}

func (m *mockDriveServer) genID() string {
	m.nextID++
	return fmt.Sprintf("file-%d", m.nextID-1)
}

func (m *mockDriveServer) handler() http.Handler {
	mux := http.NewServeMux()

	// GET /drive/v3/about — quota info
	mux.HandleFunc("/drive/v3/about", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"storageQuota": map[string]interface{}{
				"limit": "16106127360",
				"usage": "1073741824",
			},
		})
	})

	// GET /drive/v3/files — list/search
	mux.HandleFunc("/drive/v3/files", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			m.mu.Lock()
			defer m.mu.Unlock()

			q := r.URL.Query().Get("q")
			var result []map[string]string
			for _, f := range m.files {
				if f.MimeType == "application/vnd.google-apps.folder" {
					if strings.Contains(q, "mimeType") && strings.Contains(q, f.Name) {
						result = append(result, map[string]string{"id": f.ID, "name": f.Name})
					}
					continue
				}
				if q == "" {
					result = append(result, map[string]string{"id": f.ID, "name": f.Name})
					continue
				}
				// Match name = 'xxx' queries
				if strings.Contains(q, fmt.Sprintf("name = '%s'", f.Name)) {
					// Also check parent if specified
					if strings.Contains(q, "in parents") {
						for _, p := range f.Parents {
							if strings.Contains(q, fmt.Sprintf("'%s' in parents", p)) {
								result = append(result, map[string]string{"id": f.ID, "name": f.Name})
								break
							}
						}
					} else {
						result = append(result, map[string]string{"id": f.ID, "name": f.Name})
					}
				} else if strings.Contains(q, "in parents") && !strings.Contains(q, "name = ") {
					// List with parent filter and optional name prefix
					for _, p := range f.Parents {
						if strings.Contains(q, fmt.Sprintf("'%s' in parents", p)) {
							// Check name prefix if contains "name contains"
							if strings.Contains(q, "name contains") {
								// Extract prefix from: name contains 'prefix'
								idx := strings.Index(q, "name contains '")
								if idx >= 0 {
									rest := q[idx+len("name contains '"):]
									end := strings.Index(rest, "'")
									prefix := rest[:end]
									if strings.HasPrefix(f.Name, prefix) {
										result = append(result, map[string]string{"id": f.ID, "name": f.Name})
									}
								}
							} else {
								result = append(result, map[string]string{"id": f.ID, "name": f.Name})
							}
							break
						}
					}
				}
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"files": result,
			})
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	})

	// POST /upload/drive/v3/files — create file (multipart upload)
	mux.HandleFunc("/upload/drive/v3/files", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		m.mu.Lock()
		defer m.mu.Unlock()

		ct := r.Header.Get("Content-Type")
		if !strings.Contains(ct, "multipart/related") {
			http.Error(w, "expected multipart/related", http.StatusBadRequest)
			return
		}
		// Extract boundary
		idx := strings.Index(ct, "boundary=")
		if idx < 0 {
			http.Error(w, "no boundary", http.StatusBadRequest)
			return
		}
		boundary := ct[idx+len("boundary="):]

		body, _ := io.ReadAll(r.Body)
		parts := splitMultipart(string(body), boundary)
		if len(parts) < 2 {
			http.Error(w, "need at least 2 parts", http.StatusBadRequest)
			return
		}

		var meta struct {
			Name     string   `json:"name"`
			Parents  []string `json:"parents"`
			MimeType string   `json:"mimeType"`
		}
		json.Unmarshal([]byte(parts[0]), &meta)

		id := m.genID()
		m.files[id] = &mockFile{
			ID:       id,
			Name:     meta.Name,
			Parents:  meta.Parents,
			Data:     []byte(parts[1]),
			MimeType: meta.MimeType,
		}

		json.NewEncoder(w).Encode(map[string]string{"id": id, "name": meta.Name})
	})

	// GET/DELETE /drive/v3/files/{id}
	mux.HandleFunc("/drive/v3/files/", func(w http.ResponseWriter, r *http.Request) {
		// Extract file ID from path
		path := strings.TrimPrefix(r.URL.Path, "/drive/v3/files/")
		fileID := path

		m.mu.Lock()
		defer m.mu.Unlock()

		if r.Method == http.MethodGet {
			f, ok := m.files[fileID]
			if !ok {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			if r.URL.Query().Get("alt") == "media" {
				w.Write(f.Data)
				return
			}
			json.NewEncoder(w).Encode(map[string]string{"id": f.ID, "name": f.Name})
			return
		}

		if r.Method == http.MethodDelete {
			if _, ok := m.files[fileID]; !ok {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			delete(m.files, fileID)
			w.WriteHeader(http.StatusNoContent)
			return
		}

		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	})

	return mux
}

// splitMultipart is a simple multipart splitter for testing.
func splitMultipart(body, boundary string) []string {
	// Split by boundary markers
	sep := "--" + boundary
	parts := strings.Split(body, sep)
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || p == "--" {
			continue
		}
		// Skip headers (separated by double newline)
		if idx := strings.Index(p, "\r\n\r\n"); idx >= 0 {
			result = append(result, strings.TrimSpace(p[idx+4:]))
		} else if idx := strings.Index(p, "\n\n"); idx >= 0 {
			result = append(result, strings.TrimSpace(p[idx+2:]))
		} else {
			result = append(result, p)
		}
	}
	return result
}

func newTestRealBackend(t *testing.T) (*Backend, *httptest.Server) {
	t.Helper()
	mock := newMockDriveServer()
	srv := httptest.NewServer(mock.handler())

	// Pre-create the base folder in the mock
	mock.mu.Lock()
	folderID := mock.genID()
	mock.files[folderID] = &mockFile{
		ID:       folderID,
		Name:     "cloud-storage-freeloader",
		MimeType: "application/vnd.google-apps.folder",
	}
	mock.mu.Unlock()

	b := NewFromClient(http.DefaultClient, srv.URL, "cloud-storage-freeloader")
	return b, srv
}

func TestReal_PutAndGet(t *testing.T) {
	b, srv := newTestRealBackend(t)
	defer srv.Close()
	ctx := context.Background()

	data := []byte("hello drive")
	if err := b.Put(ctx, "docs/hello.txt", data); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := b.Get(ctx, "docs/hello.txt")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("Get data mismatch: got %q want %q", got, data)
	}
}

func TestReal_KeyMapping(t *testing.T) {
	b, srv := newTestRealBackend(t)
	defer srv.Close()
	ctx := context.Background()

	key := "shards/file/seg0/shard1"
	data := []byte("shard data")
	if err := b.Put(ctx, key, data); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := b.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("data mismatch")
	}
}

func TestReal_Delete(t *testing.T) {
	b, srv := newTestRealBackend(t)
	defer srv.Close()
	ctx := context.Background()

	b.Put(ctx, "del.txt", []byte("data"))
	if err := b.Delete(ctx, "del.txt"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	exists, err := b.Exists(ctx, "del.txt")
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if exists {
		t.Fatal("file should not exist after delete")
	}
}

func TestReal_Exists(t *testing.T) {
	b, srv := newTestRealBackend(t)
	defer srv.Close()
	ctx := context.Background()

	exists, err := b.Exists(ctx, "nope.txt")
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if exists {
		t.Fatal("should not exist")
	}

	b.Put(ctx, "yes.txt", []byte("data"))
	exists, err = b.Exists(ctx, "yes.txt")
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !exists {
		t.Fatal("should exist")
	}
}

func TestReal_List(t *testing.T) {
	b, srv := newTestRealBackend(t)
	defer srv.Close()
	ctx := context.Background()

	b.Put(ctx, "dir/a", []byte("a"))
	b.Put(ctx, "dir/b", []byte("b"))
	b.Put(ctx, "other/c", []byte("c"))

	keys, err := b.List(ctx, "dir/")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	// dir/a -> dir__a, dir/b -> dir__b; prefix "dir/" -> "dir__"
	if len(keys) != 2 {
		t.Fatalf("got %d keys, want 2: %v", len(keys), keys)
	}
	for _, k := range keys {
		if !strings.HasPrefix(k, "dir/") {
			t.Fatalf("key %q should start with dir/", k)
		}
	}
}

func TestReal_Available(t *testing.T) {
	b, srv := newTestRealBackend(t)
	defer srv.Close()

	avail, err := b.Available(context.Background())
	if err != nil {
		t.Fatalf("Available: %v", err)
	}
	// mock returns limit=16106127360, usage=1073741824
	expected := int64(16106127360 - 1073741824)
	if avail != expected {
		t.Fatalf("got %d, want %d", avail, expected)
	}
}

func TestReal_PutOverwrite(t *testing.T) {
	b, srv := newTestRealBackend(t)
	defer srv.Close()
	ctx := context.Background()

	b.Put(ctx, "over.txt", []byte("v1"))
	b.Put(ctx, "over.txt", []byte("v2"))

	got, err := b.Get(ctx, "over.txt")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(got, []byte("v2")) {
		t.Fatalf("got %q, want v2", got)
	}
}

func TestReal_Close(t *testing.T) {
	b, srv := newTestRealBackend(t)
	defer srv.Close()

	if err := b.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
