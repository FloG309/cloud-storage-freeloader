package vfs

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/FloG309/cloud-storage-freeloader/internal/erasure"
	"github.com/FloG309/cloud-storage-freeloader/internal/metadata"
	"github.com/FloG309/cloud-storage-freeloader/internal/placement"
	"github.com/FloG309/cloud-storage-freeloader/internal/provider"
)

// DirEntry represents a directory listing entry.
type DirEntry struct {
	Path string
	Size int64
}

// FileInfo contains file metadata.
type FileInfo struct {
	Path string
	Size int64
}

// VFS composes metadata, erasure coding, placement, and providers into
// POSIX-like file operations.
type VFS struct {
	store    *metadata.Store
	opLog    *metadata.OpLog
	engine   *placement.Engine
	encoder  *erasure.Encoder
	chunker  *erasure.Chunker
	cache    *SegmentCache
	backends map[string]provider.StorageBackend
	deviceID string
}

// NewVFS creates a virtual filesystem.
func NewVFS(
	store *metadata.Store,
	opLog *metadata.OpLog,
	engine *placement.Engine,
	encoder *erasure.Encoder,
	chunker *erasure.Chunker,
	cache *SegmentCache,
	backends map[string]provider.StorageBackend,
) *VFS {
	return &VFS{
		store:    store,
		opLog:    opLog,
		engine:   engine,
		encoder:  encoder,
		chunker:  chunker,
		cache:    cache,
		backends: backends,
		deviceID: "vfs-default",
	}
}

// Write stores a file, chunking and erasure-coding it across providers.
func (v *VFS) Write(ctx context.Context, path string, r io.Reader, size int64) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("read data: %w", err)
	}

	// Check if overwrite
	existing, _ := v.store.GetFile(path)
	if existing != nil {
		v.deleteShards(ctx, path)
		v.store.DeleteFile(path)
	}

	// Chunk
	segments := v.chunker.Chunk(bytes.NewReader(data), size)

	// Encode each segment
	type shardData struct {
		segIdx   int
		shardIdx int
		data     []byte
	}
	var allShards []shardData

	for _, seg := range segments {
		shards, err := v.encoder.Encode(seg.Data)
		if err != nil {
			return fmt.Errorf("encode segment %d: %w", seg.Index, err)
		}
		for _, shard := range shards {
			allShards = append(allShards, shardData{
				segIdx:   seg.Index,
				shardIdx: shard.Index,
				data:     shard.Data,
			})
		}
	}

	// Place shards
	if len(segments) == 0 {
		// Empty file — just store metadata
		return v.store.CreateFile(&metadata.FileMeta{Path: path, Size: size})
	}

	shardSizes := make([]int64, len(allShards)/len(segments))
	for i := range shardSizes {
		shardSizes[i] = int64(len(allShards[i].data))
	}

	shardsPerSegment := len(allShards) / len(segments)
	pm, err := v.engine.Place(shardsPerSegment-2, 2, shardSizes)
	if err != nil {
		return fmt.Errorf("placement: %w", err)
	}

	// Store file metadata
	if err := v.store.CreateFile(&metadata.FileMeta{Path: path, Size: size}); err != nil {
		return fmt.Errorf("create file meta: %w", err)
	}

	// Upload shards
	for _, sd := range allShards {
		pid := pm[sd.shardIdx]
		backend := v.backends[pid]
		key := fmt.Sprintf("shards/%s/seg%d/shard%d", path, sd.segIdx, sd.shardIdx)
		if err := backend.Put(ctx, key, sd.data); err != nil {
			return fmt.Errorf("put shard: %w", err)
		}
		v.store.AddShard(path, sd.segIdx, sd.shardIdx, pid)
	}

	// Log operation
	opType := metadata.OpFileCreate
	if existing != nil {
		opType = metadata.OpFileUpdate
	}
	v.opLog.Append(v.deviceID, opType, path, nil)

	return nil
}

// Read retrieves file data from providers, reconstructing via erasure coding.
func (v *VFS) Read(ctx context.Context, path string, offset, size int64) ([]byte, error) {
	f, err := v.store.GetFile(path)
	if err != nil {
		return nil, fmt.Errorf("file not found: %s", path)
	}

	shardMap, err := v.store.GetShardMap(path)
	if err != nil {
		return nil, err
	}

	// Reconstruct each segment
	var allData []byte
	for segIdx := 0; segIdx < len(shardMap); segIdx++ {
		// Check cache
		if cached, ok := v.cache.Get(path, segIdx); ok {
			allData = append(allData, cached...)
			continue
		}

		shards := shardMap[segIdx]
		shardsPerSeg := len(shards)

		erasureShards := make([]erasure.Shard, shardsPerSeg)
		for shardIdx, pid := range shards {
			backend := v.backends[pid]
			key := fmt.Sprintf("shards/%s/seg%d/shard%d", path, segIdx, shardIdx)
			data, err := backend.Get(ctx, key)
			if err != nil {
				// Mark as missing for reconstruction
				erasureShards[shardIdx] = erasure.Shard{Index: shardIdx, Data: nil}
				continue
			}
			erasureShards[shardIdx] = erasure.Shard{Index: shardIdx, Data: data}
		}

		decoded, err := v.encoder.Decode(erasureShards)
		if err != nil {
			return nil, fmt.Errorf("decode segment %d: %w", segIdx, err)
		}

		v.cache.Put(path, segIdx, decoded)
		allData = append(allData, decoded...)
	}

	// Trim to original file size
	if int64(len(allData)) > f.Size {
		allData = allData[:f.Size]
	}

	// Apply offset and size
	end := offset + size
	if end > int64(len(allData)) {
		end = int64(len(allData))
	}
	return allData[offset:end], nil
}

// ReadDir lists entries under a directory path.
func (v *VFS) ReadDir(ctx context.Context, dirPath string) ([]DirEntry, error) {
	files, err := v.store.ListDirectory(dirPath)
	if err != nil {
		return nil, err
	}
	entries := make([]DirEntry, len(files))
	for i, f := range files {
		entries[i] = DirEntry{Path: f.Path, Size: f.Size}
	}
	return entries, nil
}

// Stat returns file info.
func (v *VFS) Stat(ctx context.Context, path string) (*FileInfo, error) {
	f, err := v.store.GetFile(path)
	if err != nil {
		return nil, err
	}
	return &FileInfo{Path: f.Path, Size: f.Size}, nil
}

// Delete removes a file and its shards.
func (v *VFS) Delete(ctx context.Context, path string) error {
	v.deleteShards(ctx, path)
	v.store.DeleteFile(path)
	v.opLog.Append(v.deviceID, metadata.OpFileDelete, path, nil)
	return nil
}

// Mkdir creates a directory.
func (v *VFS) Mkdir(ctx context.Context, path string) error {
	return v.store.Mkdir(path)
}

// Rename renames a file, moving shard keys on providers.
func (v *VFS) Rename(ctx context.Context, oldPath, newPath string) error {
	// Move physical shards on providers before updating metadata
	shardMap, _ := v.store.GetShardMap(oldPath)
	for segIdx, shards := range shardMap {
		for shardIdx, pid := range shards {
			backend := v.backends[pid]
			oldKey := fmt.Sprintf("shards/%s/seg%d/shard%d", oldPath, segIdx, shardIdx)
			newKey := fmt.Sprintf("shards/%s/seg%d/shard%d", newPath, segIdx, shardIdx)
			data, err := backend.Get(ctx, oldKey)
			if err != nil {
				continue
			}
			backend.Put(ctx, newKey, data)
			backend.Delete(ctx, oldKey)
		}
	}

	if err := v.store.RenameFile(oldPath, newPath); err != nil {
		return err
	}
	v.opLog.Append(v.deviceID, metadata.OpFileRename, oldPath, []byte(newPath))
	return nil
}

func (v *VFS) deleteShards(ctx context.Context, path string) {
	shardMap, _ := v.store.GetShardMap(path)
	for segIdx, shards := range shardMap {
		for shardIdx, pid := range shards {
			backend := v.backends[pid]
			key := fmt.Sprintf("shards/%s/seg%d/shard%d", path, segIdx, shardIdx)
			backend.Delete(ctx, key)
		}
	}
	v.store.DeleteShards(path)
}
