package erasure

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"testing"
)

func TestEncoder_EncodeAndDecode(t *testing.T) {
	enc, err := NewEncoder(4, 3)
	if err != nil {
		t.Fatalf("NewEncoder failed: %v", err)
	}

	data := []byte("Hello, Reed-Solomon erasure coding test data!!!!!")
	// Pad to multiple of k
	for len(data)%4 != 0 {
		data = append(data, 0)
	}

	shards, err := enc.Encode(data)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}
	if len(shards) != 7 {
		t.Fatalf("got %d shards, want 7", len(shards))
	}

	// Decode using only the first 4 shards (data shards)
	subset := make([]Shard, 7)
	copy(subset, shards)
	// Zero out parity shards to simulate loss
	for i := 4; i < 7; i++ {
		subset[i].Data = nil
	}
	decoded, err := enc.Decode(subset)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if !bytes.Equal(decoded, data) {
		t.Fatalf("decoded data does not match original")
	}
}

func TestEncoder_DecodeFromParityShards(t *testing.T) {
	enc, _ := NewEncoder(4, 3)

	data := make([]byte, 64)
	rand.Read(data)

	shards, _ := enc.Encode(data)

	// Drop all 4 data shards, keep only 3 parity — should fail (need k=4)
	subset := make([]Shard, 7)
	copy(subset, shards)
	for i := 0; i < 4; i++ {
		subset[i].Data = nil
	}
	_, err := enc.Decode(subset)
	if err == nil {
		t.Fatal("expected error: only 3 shards available, need 4")
	}

	// Now keep 1 data + 3 parity = 4 shards (enough)
	subset2 := make([]Shard, 7)
	copy(subset2, shards)
	for i := 1; i < 4; i++ {
		subset2[i].Data = nil
	}
	decoded, err := enc.Decode(subset2)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if !bytes.Equal(decoded, data) {
		t.Fatal("decoded data does not match original")
	}
}

func TestEncoder_DecodeFromMixed(t *testing.T) {
	enc, _ := NewEncoder(4, 3)

	data := make([]byte, 80)
	rand.Read(data)

	shards, _ := enc.Encode(data)

	// Drop 2 random shards (indices 1 and 5)
	subset := make([]Shard, 7)
	copy(subset, shards)
	subset[1].Data = nil
	subset[5].Data = nil

	decoded, err := enc.Decode(subset)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if !bytes.Equal(decoded, data) {
		t.Fatal("decoded data does not match original")
	}
}

func TestEncoder_TooFewShards(t *testing.T) {
	enc, _ := NewEncoder(4, 3)

	data := make([]byte, 64)
	rand.Read(data)
	shards, _ := enc.Encode(data)

	// Keep only 3 of required 4
	subset := make([]Shard, 7)
	copy(subset, shards)
	for i := 0; i < 4; i++ {
		subset[i].Data = nil
	}
	_, err := enc.Decode(subset)
	if err == nil {
		t.Fatal("expected error with too few shards")
	}
}

func TestEncoder_CorruptShard(t *testing.T) {
	enc, _ := NewEncoder(4, 3)

	data := make([]byte, 64)
	rand.Read(data)
	shards, _ := enc.Encode(data)

	// Corrupt a shard's data but keep the checksum
	shards[0].Data[0] ^= 0xFF

	if VerifyChecksum(shards[0]) {
		t.Fatal("expected checksum verification to fail for corrupted shard")
	}
}

func TestEncoder_DifferentParameters(t *testing.T) {
	tests := []struct {
		k, m int
	}{
		{6, 3},
		{10, 4},
	}

	for _, tt := range tests {
		enc, err := NewEncoder(tt.k, tt.m)
		if err != nil {
			t.Fatalf("NewEncoder(%d,%d) failed: %v", tt.k, tt.m, err)
		}

		data := make([]byte, tt.k*16)
		rand.Read(data)

		shards, err := enc.Encode(data)
		if err != nil {
			t.Fatalf("Encode failed: %v", err)
		}
		if len(shards) != tt.k+tt.m {
			t.Fatalf("got %d shards, want %d", len(shards), tt.k+tt.m)
		}

		// Drop m shards and decode
		subset := make([]Shard, len(shards))
		copy(subset, shards)
		for i := 0; i < tt.m; i++ {
			subset[tt.k+i].Data = nil
		}
		decoded, err := enc.Decode(subset)
		if err != nil {
			t.Fatalf("Decode(%d,%d) failed: %v", tt.k, tt.m, err)
		}
		if !bytes.Equal(decoded, data) {
			t.Fatalf("Decode(%d,%d) data mismatch", tt.k, tt.m)
		}
	}
}

func TestEncoder_LargeSegment(t *testing.T) {
	enc, _ := NewEncoder(4, 3)

	data := make([]byte, 8*1024*1024) // 8MB
	rand.Read(data)

	shards, err := enc.Encode(data)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	// Drop 2 shards
	subset := make([]Shard, len(shards))
	copy(subset, shards)
	subset[2].Data = nil
	subset[5].Data = nil

	decoded, err := enc.Decode(subset)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if !bytes.Equal(decoded, data) {
		t.Fatal("large segment decode mismatch")
	}
}

func TestEncoder_ShardMetadata(t *testing.T) {
	enc, _ := NewEncoder(4, 3)

	data := make([]byte, 64)
	rand.Read(data)
	shards, _ := enc.Encode(data)

	for i, s := range shards {
		if s.Index != i {
			t.Fatalf("shard %d has Index %d", i, s.Index)
		}
		if s.Total != 7 {
			t.Fatalf("shard %d has Total %d, want 7", i, s.Total)
		}
		if s.K != 4 {
			t.Fatalf("shard %d has K %d, want 4", i, s.K)
		}
		if s.M != 3 {
			t.Fatalf("shard %d has M %d, want 3", i, s.M)
		}
		expected := sha256.Sum256(s.Data)
		if s.Checksum != expected {
			t.Fatalf("shard %d checksum mismatch", i)
		}
	}
}
