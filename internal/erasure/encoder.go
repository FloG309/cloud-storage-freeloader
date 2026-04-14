package erasure

import (
	"crypto/sha256"
	"fmt"

	"github.com/klauspost/reedsolomon"
)

// Shard is one piece of an erasure-coded segment.
type Shard struct {
	Index    int
	Total    int
	K        int
	M        int
	Data     []byte
	Checksum [32]byte
}

// Encoder wraps Reed-Solomon encoding/decoding.
type Encoder struct {
	k   int
	m   int
	enc reedsolomon.Encoder
}

// NewEncoder creates an encoder with k data shards and m parity shards.
func NewEncoder(k, m int) (*Encoder, error) {
	enc, err := reedsolomon.New(k, m)
	if err != nil {
		return nil, fmt.Errorf("reedsolomon.New(%d,%d): %w", k, m, err)
	}
	return &Encoder{k: k, m: m, enc: enc}, nil
}

// Encode splits data into k data shards plus m parity shards.
// Data must be a multiple of k in length.
func (e *Encoder) Encode(data []byte) ([]Shard, error) {
	shardSize := len(data) / e.k
	total := e.k + e.m

	// Split data into k shards
	dataShards := make([][]byte, total)
	for i := 0; i < e.k; i++ {
		dataShards[i] = make([]byte, shardSize)
		copy(dataShards[i], data[i*shardSize:(i+1)*shardSize])
	}
	for i := e.k; i < total; i++ {
		dataShards[i] = make([]byte, shardSize)
	}

	if err := e.enc.Encode(dataShards); err != nil {
		return nil, fmt.Errorf("encode: %w", err)
	}

	shards := make([]Shard, total)
	for i := 0; i < total; i++ {
		shards[i] = Shard{
			Index:    i,
			Total:    total,
			K:        e.k,
			M:        e.m,
			Data:     dataShards[i],
			Checksum: sha256.Sum256(dataShards[i]),
		}
	}
	return shards, nil
}

// Decode reconstructs the original data from at least k shards.
// Shards with nil Data are treated as missing.
func (e *Encoder) Decode(shards []Shard) ([]byte, error) {
	total := e.k + e.m
	raw := make([][]byte, total)

	available := 0
	for _, s := range shards {
		if s.Data != nil {
			raw[s.Index] = s.Data
			available++
		}
	}
	if available < e.k {
		return nil, fmt.Errorf("need at least %d shards, have %d", e.k, available)
	}

	if err := e.enc.Reconstruct(raw); err != nil {
		return nil, fmt.Errorf("reconstruct: %w", err)
	}

	// Concatenate data shards
	result := make([]byte, 0, len(raw[0])*e.k)
	for i := 0; i < e.k; i++ {
		result = append(result, raw[i]...)
	}
	return result, nil
}

// VerifyChecksum checks if a shard's data matches its stored checksum.
func VerifyChecksum(s Shard) bool {
	return sha256.Sum256(s.Data) == s.Checksum
}
