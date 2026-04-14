package livesync

import (
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// FilePatches groups patches with metadata for a single file operation.
type FilePatches struct {
	FileHash string    `json:"file_hash"`
	Seq      int64     `json:"seq"`
	DeviceID string    `json:"device_id"`
	Time     time.Time `json:"time"`
	Patches  []Patch   `json:"patches"`
}

// SerializePatches encodes a batch of file patches to JSON.
func SerializePatches(batch []FilePatches) ([]byte, error) {
	return json.Marshal(batch)
}

// DeserializePatches decodes a batch of file patches from JSON.
func DeserializePatches(data []byte) ([]FilePatches, error) {
	var batch []FilePatches
	err := json.Unmarshal(data, &batch)
	return batch, err
}

// CompressBatch compresses data using gzip.
func CompressBatch(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// DecompressBatch decompresses gzip data.
func DecompressBatch(data []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

// EncryptBatch encrypts data using AES-256-GCM.
func EncryptBatch(data, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, data, nil), nil
}

// DecryptBatch decrypts AES-256-GCM encrypted data.
func DecryptBatch(data, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	return gcm.Open(nil, nonce, ciphertext, nil)
}
