package livesync

import (
	"bytes"
	"crypto/rand"
	"testing"
	"time"
)

func TestPatch_Serialize(t *testing.T) {
	p := FilePatches{
		FileHash: "abc123",
		Seq:      1,
		DeviceID: "dev1",
		Time:     time.Now(),
		Patches:  []Patch{{Type: PatchInsert, Offset: 5, Data: []byte("hello")}},
	}

	data, err := SerializePatches([]FilePatches{p})
	if err != nil {
		t.Fatalf("Serialize failed: %v", err)
	}

	got, err := DeserializePatches(data)
	if err != nil {
		t.Fatalf("Deserialize failed: %v", err)
	}
	if len(got) != 1 || got[0].FileHash != "abc123" {
		t.Fatalf("roundtrip failed: %+v", got)
	}
}

func TestPatch_BatchSerialize(t *testing.T) {
	var batch []FilePatches
	for i := 0; i < 10; i++ {
		batch = append(batch, FilePatches{
			FileHash: "hash",
			Seq:      int64(i),
			DeviceID: "dev1",
			Time:     time.Now(),
			Patches:  []Patch{{Type: PatchInsert, Offset: i, Data: []byte("data")}},
		})
	}

	data, _ := SerializePatches(batch)
	got, _ := DeserializePatches(data)
	if len(got) != 10 {
		t.Fatalf("got %d patches, want 10", len(got))
	}
}

func TestPatch_Compress(t *testing.T) {
	var batch []FilePatches
	for i := 0; i < 50; i++ {
		batch = append(batch, FilePatches{
			FileHash: "hash",
			Seq:      int64(i),
			DeviceID: "dev1",
			Time:     time.Now(),
			Patches:  []Patch{{Type: PatchInsert, Offset: 0, Data: []byte("repeated data content")}},
		})
	}

	raw, _ := SerializePatches(batch)
	compressed, err := CompressBatch(raw)
	if err != nil {
		t.Fatalf("Compress failed: %v", err)
	}
	if len(compressed) >= len(raw) {
		t.Fatalf("compressed (%d) should be smaller than raw (%d)", len(compressed), len(raw))
	}

	decompressed, err := DecompressBatch(compressed)
	if err != nil {
		t.Fatalf("Decompress failed: %v", err)
	}
	if !bytes.Equal(decompressed, raw) {
		t.Fatal("decompress roundtrip failed")
	}
}

func TestPatch_Encrypt(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)

	data := []byte("secret patch data")
	encrypted, err := EncryptBatch(data, key)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	decrypted, err := DecryptBatch(encrypted, key)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}
	if !bytes.Equal(decrypted, data) {
		t.Fatal("encrypt/decrypt roundtrip failed")
	}
}

func TestPatch_EncryptWrongKey(t *testing.T) {
	key1 := make([]byte, 32)
	key2 := make([]byte, 32)
	rand.Read(key1)
	rand.Read(key2)

	encrypted, _ := EncryptBatch([]byte("secret"), key1)
	_, err := DecryptBatch(encrypted, key2)
	if err == nil {
		t.Fatal("expected error with wrong key")
	}
}

func TestPatch_Metadata(t *testing.T) {
	p := FilePatches{
		FileHash: "deadbeef",
		Seq:      42,
		DeviceID: "laptop",
		Time:     time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
		Patches:  []Patch{{Type: PatchInsert, Offset: 0, Data: []byte("x")}},
	}

	data, _ := SerializePatches([]FilePatches{p})
	got, _ := DeserializePatches(data)

	if got[0].FileHash != "deadbeef" {
		t.Fatalf("FileHash mismatch: %s", got[0].FileHash)
	}
	if got[0].Seq != 42 {
		t.Fatalf("Seq mismatch: %d", got[0].Seq)
	}
	if got[0].DeviceID != "laptop" {
		t.Fatalf("DeviceID mismatch: %s", got[0].DeviceID)
	}
	if got[0].Time.Year() != 2026 {
		t.Fatalf("Time mismatch: %v", got[0].Time)
	}
}
