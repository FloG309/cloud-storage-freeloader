package erasure

import "io"

// Segment is a fixed-size piece of a file.
type Segment struct {
	Index int
	Data  []byte
}

// Chunker splits files into fixed-size segments.
type Chunker struct {
	SegmentSize int
}

// NewChunker creates a chunker with the given segment size in bytes.
func NewChunker(segmentSize int) *Chunker {
	return &Chunker{SegmentSize: segmentSize}
}

// Chunk reads from r and splits the data into fixed-size segments.
// The last segment is zero-padded to SegmentSize.
func (c *Chunker) Chunk(r io.Reader, fileSize int64) []Segment {
	if fileSize == 0 {
		return nil
	}

	var segments []Segment
	buf := make([]byte, c.SegmentSize)
	idx := 0

	for {
		n, err := io.ReadFull(r, buf)
		if n > 0 {
			seg := make([]byte, c.SegmentSize)
			copy(seg, buf[:n])
			segments = append(segments, Segment{Index: idx, Data: seg})
			idx++
		}
		if err != nil {
			break
		}
	}

	return segments
}

// Reassemble combines segments back into the original file data,
// stripping padding based on the original file size.
func (c *Chunker) Reassemble(segments []Segment, originalSize int64) []byte {
	result := make([]byte, 0, originalSize)
	for _, seg := range segments {
		result = append(result, seg.Data...)
	}
	return result[:originalSize]
}
