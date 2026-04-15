package onedrive

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

// mockGraphServer creates an httptest server that simulates the Microsoft Graph API.
func mockGraphServer(t *testing.T) (*httptest.Server, *Backend) {
	t.Helper()

	mu := &sync.Mutex{}
	files := map[string][]byte{}       // name -> data
	fileIDs := map[string]string{}     // name -> id
	nextID := 0
	folderCreated := false

	mux := http.NewServeMux()

	// GET /me/drive — quota
	mux.HandleFunc("GET /me/drive", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"quota": map[string]interface{}{
				"remaining": 5368709120, // 5GB
			},
		})
	})

	// GET /me/drive/root:/cloud-storage-freeloader — check folder exists
	// GET /me/drive/root:/cloud-storage-freeloader/{name}:/content — download
	// GET /me/drive/root:/cloud-storage-freeloader/{name}: — get item metadata
	// GET /me/drive/root:/cloud-storage-freeloader:/children — list children
	mux.HandleFunc("GET /me/drive/root:/cloud-storage-freeloader", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		// Exact folder check
		if path == "/me/drive/root:/cloud-storage-freeloader" {
			if !folderCreated {
				http.Error(w, `{"error":{"code":"itemNotFound"}}`, http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id":   "folder-id",
				"name": "cloud-storage-freeloader",
			})
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	})

	mux.HandleFunc("GET /me/drive/root:/cloud-storage-freeloader:/children", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		var children []map[string]interface{}
		for name := range files {
			children = append(children, map[string]interface{}{
				"name": name,
				"id":   fileIDs[name],
			})
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"value": children,
		})
	})

	mux.HandleFunc("GET /me/drive/root:/cloud-storage-freeloader/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		// /me/drive/root:/cloud-storage-freeloader/{name}:/content
		if strings.HasSuffix(path, ":/content") {
			name := path[len("/me/drive/root:/cloud-storage-freeloader/"):]
			name = strings.TrimSuffix(name, ":/content")
			mu.Lock()
			data, ok := files[name]
			mu.Unlock()
			if !ok {
				http.Error(w, `{"error":{"code":"itemNotFound"}}`, http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Write(data)
			return
		}
		// /me/drive/root:/cloud-storage-freeloader/{name}: — metadata
		if strings.HasSuffix(path, ":") {
			name := path[len("/me/drive/root:/cloud-storage-freeloader/"):]
			name = strings.TrimSuffix(name, ":")
			mu.Lock()
			id, ok := fileIDs[name]
			mu.Unlock()
			if !ok {
				http.Error(w, `{"error":{"code":"itemNotFound"}}`, http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id":   id,
				"name": name,
			})
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	})

	// POST /me/drive/root/children — create folder
	mux.HandleFunc("POST /me/drive/root/children", func(w http.ResponseWriter, r *http.Request) {
		folderCreated = true
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":   "folder-id",
			"name": "cloud-storage-freeloader",
		})
	})

	// PUT /me/drive/root:/cloud-storage-freeloader/{name}:/content — upload small file
	mux.HandleFunc("PUT /me/drive/root:/cloud-storage-freeloader/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if !strings.HasSuffix(path, ":/content") {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		name := path[len("/me/drive/root:/cloud-storage-freeloader/"):]
		name = strings.TrimSuffix(name, ":/content")
		data, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read error", http.StatusInternalServerError)
			return
		}
		mu.Lock()
		files[name] = data
		nextID++
		fileIDs[name] = fmt.Sprintf("item-%d", nextID)
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":   fileIDs[name],
			"name": name,
		})
	})

	// DELETE /me/drive/items/{id}
	mux.HandleFunc("DELETE /me/drive/items/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/me/drive/items/")
		mu.Lock()
		defer mu.Unlock()
		for name, fid := range fileIDs {
			if fid == id {
				delete(files, name)
				delete(fileIDs, name)
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		http.Error(w, `{"error":{"code":"itemNotFound"}}`, http.StatusNotFound)
	})

	srv := httptest.NewServer(mux)

	b := &Backend{
		httpClient: srv.Client(),
		graphBase:  srv.URL,
		baseFolder: "cloud-storage-freeloader",
	}

	return srv, b
}

func TestOneDriveReal_NewFromConfig(t *testing.T) {
	cfg := map[string]string{
		"client_id":   "test-client-id",
		"tokens_file": "nonexistent_tokens.json",
		"base_folder": "cloud-storage-freeloader",
	}
	// Should fail because tokens file doesn't exist
	_, err := NewFromConfig(cfg)
	if err == nil {
		t.Fatal("expected error for missing tokens file")
	}
}

func TestOneDriveReal_NewFromConfig_MissingClientID(t *testing.T) {
	cfg := map[string]string{
		"tokens_file": "tokens.json",
	}
	_, err := NewFromConfig(cfg)
	if err == nil {
		t.Fatal("expected error for missing client_id")
	}
}

func TestOneDriveReal_PutAndGet(t *testing.T) {
	srv, b := mockGraphServer(t)
	defer srv.Close()
	ctx := context.Background()

	data := []byte("hello onedrive real")
	if err := b.Put(ctx, "test/file.txt", data); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	got, err := b.Get(ctx, "test/file.txt")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("got %q, want %q", got, data)
	}
}

func TestOneDriveReal_KeyMapping(t *testing.T) {
	// Verify that "/" in keys gets mapped to "__" for flat filenames
	mapped := mapKey("shards/file/seg0/shard1")
	if mapped != "shards__file__seg0__shard1" {
		t.Fatalf("mapKey = %q, want %q", mapped, "shards__file__seg0__shard1")
	}
	unmapped := unmapKey("shards__file__seg0__shard1")
	if unmapped != "shards/file/seg0/shard1" {
		t.Fatalf("unmapKey = %q, want %q", unmapped, "shards/file/seg0/shard1")
	}
}

func TestOneDriveReal_Delete(t *testing.T) {
	srv, b := mockGraphServer(t)
	defer srv.Close()
	ctx := context.Background()

	b.Put(ctx, "del-key", []byte("data"))

	if err := b.Delete(ctx, "del-key"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	exists, _ := b.Exists(ctx, "del-key")
	if exists {
		t.Fatal("should not exist after delete")
	}
}

func TestOneDriveReal_Exists(t *testing.T) {
	srv, b := mockGraphServer(t)
	defer srv.Close()
	ctx := context.Background()

	exists, _ := b.Exists(ctx, "nope")
	if exists {
		t.Fatal("should not exist")
	}

	b.Put(ctx, "yes-key", []byte("data"))
	exists, _ = b.Exists(ctx, "yes-key")
	if !exists {
		t.Fatal("should exist")
	}
}

func TestOneDriveReal_List(t *testing.T) {
	srv, b := mockGraphServer(t)
	defer srv.Close()
	ctx := context.Background()

	b.Put(ctx, "prefix/a", []byte("a"))
	b.Put(ctx, "prefix/b", []byte("b"))
	b.Put(ctx, "other/c", []byte("c"))

	keys, err := b.List(ctx, "prefix/")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("got %d keys, want 2: %v", len(keys), keys)
	}
}

func TestOneDriveReal_Available(t *testing.T) {
	srv, b := mockGraphServer(t)
	defer srv.Close()

	avail, err := b.Available(context.Background())
	if err != nil {
		t.Fatalf("Available failed: %v", err)
	}
	if avail != 5368709120 {
		t.Fatalf("got %d, want 5368709120", avail)
	}
}

func TestOneDriveReal_GetNotFound(t *testing.T) {
	srv, b := mockGraphServer(t)
	defer srv.Close()

	_, err := b.Get(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent key")
	}
}
