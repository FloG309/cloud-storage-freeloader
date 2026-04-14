package erasure

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestChunker_ExactMultiple(t *testing.T) {
	c := NewChunker(8)
	data := make([]byte, 32) // 32 bytes / 8 = 4 segments
	segments := c.Chunk(bytes.NewReader(data), int64(len(data)))
	if len(segments) != 4 {
		t.Fatalf("got %d segments, want 4", len(segments))
	}
	for i, seg := range segments {
		if seg.Index != i {
			t.Fatalf("segment %d has index %d", i, seg.Index)
		}
		if len(seg.Data) != 8 {
			t.Fatalf("segment %d has size %d, want 8", i, len(seg.Data))
		}
	}
}

func TestChunker_WithRemainder(t *testing.T) {
	c := NewChunker(8)
	data := make([]byte, 35) // 4 full + 1 padded
	segments := c.Chunk(bytes.NewReader(data), int64(len(data)))
	if len(segments) != 5 {
		t.Fatalf("got %d segments, want 5", len(segments))
	}
	// Last segment should be padded to segment size
	if len(segments[4].Data) != 8 {
		t.Fatalf("last segment size %d, want 8 (padded)", len(segments[4].Data))
	}
}

func TestChunker_SmallFile(t *testing.T) {
	c := NewChunker(1024)
	data := []byte("hello") // 5 bytes < 1024
	segments := c.Chunk(bytes.NewReader(data), int64(len(data)))
	if len(segments) != 1 {
		t.Fatalf("got %d segments, want 1", len(segments))
	}
	if len(segments[0].Data) != 1024 {
		t.Fatalf("segment size %d, want 1024 (padded)", len(segments[0].Data))
	}
}

func TestChunker_EmptyFile(t *testing.T) {
	c := NewChunker(1024)
	segments := c.Chunk(bytes.NewReader(nil), 0)
	if len(segments) != 0 {
		t.Fatalf("got %d segments, want 0", len(segments))
	}
}

func TestChunker_SegmentSizeRoundtrip(t *testing.T) {
	c := NewChunker(8)
	original := []byte("Hello, this is a test of chunking and reassembly!!")
	segments := c.Chunk(bytes.NewReader(original), int64(len(original)))
	reassembled := c.Reassemble(segments, int64(len(original)))
	if !bytes.Equal(reassembled, original) {
		t.Fatalf("reassembled %q does not match original %q", reassembled, original)
	}
}

func TestChunker_StreamingChunk(t *testing.T) {
	c := NewChunker(8)
	// Use a non-seekable reader
	reader := io.NopCloser(strings.NewReader("abcdefghijklmnop")) // 16 bytes
	segments := c.Chunk(reader.(io.Reader), 16)
	if len(segments) != 2 {
		t.Fatalf("got %d segments, want 2", len(segments))
	}
}
