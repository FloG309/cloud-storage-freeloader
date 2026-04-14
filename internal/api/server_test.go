package api

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/FloG309/cloud-storage-freeloader/internal/erasure"
	"github.com/FloG309/cloud-storage-freeloader/internal/metadata"
	"github.com/FloG309/cloud-storage-freeloader/internal/placement"
	"github.com/FloG309/cloud-storage-freeloader/internal/provider"
	"github.com/FloG309/cloud-storage-freeloader/internal/provider/memory"
	"github.com/FloG309/cloud-storage-freeloader/internal/vfs"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()

	providers := make([]placement.ProviderInfo, 4)
	backends := make(map[string]provider.StorageBackend)
	for i := 0; i < 4; i++ {
		id := string(rune('A' + i))
		providers[i] = placement.ProviderInfo{
			ID:        id,
			Profile:   provider.ProviderProfile{},
			Tracker:   provider.NewBandwidthTracker(0, 0),
			Available: 10 * 1024 * 1024,
		}
		backends[id] = memory.New(10 * 1024 * 1024)
	}

	store, _ := metadata.NewStore(":memory:")
	opLog, _ := metadata.NewOpLog(store.DB())
	t.Cleanup(func() { store.Close() })

	enc, _ := erasure.NewEncoder(2, 2)
	chunker := erasure.NewChunker(1024)
	engine := placement.NewEngine(providers)
	cache := vfs.NewSegmentCache(1024 * 1024)

	v := vfs.NewVFS(store, opLog, engine, enc, chunker, cache, backends)

	return NewServer(v)
}

func TestAPI_ListFiles(t *testing.T) {
	s := newTestServer(t)
	s.vfs.Write(nil, "/hello.txt", bytes.NewReader([]byte("hello content here!!")), 20)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/files?path=/", nil)
	s.Router().ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("got status %d, want 200", w.Code)
	}

	var entries []map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &entries)
	if len(entries) < 1 {
		t.Fatalf("got %d entries, want at least 1", len(entries))
	}
}

func TestAPI_UploadFile(t *testing.T) {
	s := newTestServer(t)

	// Create multipart upload
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "upload.txt")
	part.Write([]byte("uploaded content data for testing!!"))
	writer.WriteField("path", "/upload.txt")
	writer.Close()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/files", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	s.Router().ServeHTTP(w, req)

	if w.Code != 201 {
		t.Fatalf("got status %d, want 201. Body: %s", w.Code, w.Body.String())
	}
}

func TestAPI_DownloadFile(t *testing.T) {
	s := newTestServer(t)

	data := []byte("download me! this is test file content!!")
	s.vfs.Write(nil, "/download.txt", bytes.NewReader(data), int64(len(data)))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/files/download?path=/download.txt", nil)
	s.Router().ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("got status %d, want 200", w.Code)
	}
	got, _ := io.ReadAll(w.Body)
	if !bytes.Equal(got, data) {
		t.Fatalf("download data mismatch")
	}
}

func TestAPI_DeleteFile(t *testing.T) {
	s := newTestServer(t)

	data := []byte("to be deleted soon in this test!!!!!!")
	s.vfs.Write(nil, "/delete_me.txt", bytes.NewReader(data), int64(len(data)))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("DELETE", "/api/files?path=/delete_me.txt", nil)
	s.Router().ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("got status %d, want 200", w.Code)
	}
}

func TestAPI_Rename(t *testing.T) {
	s := newTestServer(t)

	data := []byte("rename test content for api test!!!!!")
	s.vfs.Write(nil, "/old_api.txt", bytes.NewReader(data), int64(len(data)))

	body, _ := json.Marshal(map[string]string{"newPath": "/new_api.txt"})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PATCH", "/api/files?path=/old_api.txt", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	s.Router().ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("got status %d, want 200. Body: %s", w.Code, w.Body.String())
	}
}

func TestAPI_Status(t *testing.T) {
	s := newTestServer(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/status", nil)
	s.Router().ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("got status %d, want 200", w.Code)
	}

	var status map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &status)
	if _, ok := status["status"]; !ok {
		t.Fatal("expected 'status' field in response")
	}
}

func TestAPI_Providers(t *testing.T) {
	s := newTestServer(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/providers", nil)
	s.Router().ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("got status %d, want 200", w.Code)
	}
}
