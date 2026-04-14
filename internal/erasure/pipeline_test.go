package erasure

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func pipelineRoundtrip(t *testing.T, data []byte, k, m, segmentSize int) {
	t.Helper()
	chunker := NewChunker(segmentSize)
	enc, err := NewEncoder(k, m)
	if err != nil {
		t.Fatalf("NewEncoder(%d,%d): %v", k, m, err)
	}

	// Chunk
	segments := chunker.Chunk(bytes.NewReader(data), int64(len(data)))

	// Encode each segment and simulate shard loss
	type shardSet struct {
		shards []Shard
	}
	allShards := make([]shardSet, len(segments))

	for i, seg := range segments {
		shards, err := enc.Encode(seg.Data)
		if err != nil {
			t.Fatalf("Encode segment %d: %v", i, err)
		}
		allShards[i] = shardSet{shards: shards}
	}

	// Lose up to m shards per segment, then decode
	decodedSegments := make([]Segment, len(segments))
	for i, ss := range allShards {
		// Drop up to m shards (different ones per segment for variety)
		subset := make([]Shard, len(ss.shards))
		copy(subset, ss.shards)
		for j := 0; j < m; j++ {
			dropIdx := (i + j) % len(subset)
			subset[dropIdx].Data = nil
		}

		decoded, err := enc.Decode(subset)
		if err != nil {
			t.Fatalf("Decode segment %d: %v", i, err)
		}
		decodedSegments[i] = Segment{Index: i, Data: decoded}
	}

	// Reassemble
	result := chunker.Reassemble(decodedSegments, int64(len(data)))
	if !bytes.Equal(result, data) {
		t.Fatalf("pipeline roundtrip failed: data mismatch")
	}
}

func TestPipeline_SmallFile(t *testing.T) {
	data := make([]byte, 1024)
	rand.Read(data)
	pipelineRoundtrip(t, data, 4, 3, 256)
}

func TestPipeline_LargeFile(t *testing.T) {
	data := make([]byte, 50*1024) // 50KB (scaled down for speed)
	rand.Read(data)
	pipelineRoundtrip(t, data, 4, 3, 4096)
}

func TestPipeline_BinaryFile(t *testing.T) {
	data := make([]byte, 8192)
	rand.Read(data)
	pipelineRoundtrip(t, data, 4, 3, 2048)
}

func TestPipeline_DifferentShardLossPerSegment(t *testing.T) {
	data := make([]byte, 4096)
	rand.Read(data)

	chunker := NewChunker(512)
	enc, _ := NewEncoder(4, 3)
	segments := chunker.Chunk(bytes.NewReader(data), int64(len(data)))

	decodedSegments := make([]Segment, len(segments))
	for i, seg := range segments {
		shards, _ := enc.Encode(seg.Data)

		// Each segment loses a different single shard
		subset := make([]Shard, len(shards))
		copy(subset, shards)
		subset[i%len(subset)].Data = nil

		decoded, err := enc.Decode(subset)
		if err != nil {
			t.Fatalf("Decode segment %d: %v", i, err)
		}
		decodedSegments[i] = Segment{Index: i, Data: decoded}
	}

	result := chunker.Reassemble(decodedSegments, int64(len(data)))
	if !bytes.Equal(result, data) {
		t.Fatal("different shard loss roundtrip failed")
	}
}
