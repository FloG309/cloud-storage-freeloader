# Cloud Storage Freeloader — Implementation Plan

## Overview

A unified filesystem that aggregates free-tier cloud storage providers, distributes
files across them using Reed-Solomon erasure coding, and exposes the result as a
native OS drive (FUSE/WinFsp) and web UI. Providers are tiered by quality (hot/warm/cold)
so bandwidth-limited free tiers serve as parity/archive backends while unrestricted
providers handle active reads/writes.

The system also includes a live sync layer for applications like Obsidian that need
near-real-time multi-device synchronization. Instead of erasure-coding every 2-second
file save, the sync layer captures file-level diffs as lightweight patches, batches
them, and replicates to a few hot providers. Periodic erasure-coded snapshots provide
the durability backbone. This two-tier approach (fast replicated patches + slow
resilient snapshots) keeps API usage low while enabling ~10-30 second cross-device sync.

## Architecture

```
┌──────────────────────────────────────────────────────┐
│                    User Interfaces                    │
│  ┌──────────────┐  ┌──────────────┐  ┌────────────┐ │
│  │  FUSE/WinFsp │  │   Web UI     │  │  CLI       │ │
│  │  (drive X:)  │  │  (React SPA) │  │  (cobra)   │ │
│  └──────┬───────┘  └──────┬───────┘  └─────┬──────┘ │
│         └─────────────┬───┴────────────────┘         │
│                       ▼                               │
│  ┌────────────────────────────────────────────────┐   │
│  │              Virtual File System               │   │
│  │  (POSIX-like: open, read, write, readdir...)   │   │
│  └────────────────────┬───────────────────────────┘   │
│                       ▼                               │
│  ┌────────────────────────────────────────────────┐   │
│  │           Erasure Coding Engine                │   │
│  │  (Reed-Solomon encode/decode via klauspost)    │   │
│  └────────────────────┬───────────────────────────┘   │
│                       ▼                               │
│  ┌────────────────────────────────────────────────┐   │
│  │        Shard Placement Engine (Tiered)         │   │
│  │  data shards → hot | parity shards → cold      │   │
│  └────────────────────┬───────────────────────────┘   │
│                       ▼                               │
│  ┌────────────────────────────────────────────────┐   │
│  │          Provider Abstraction Layer            │   │
│  │  ┌─────┐ ┌──────┐ ┌──────┐ ┌──────┐ ┌──────┐ │   │
│  │  │ S3  │ │WebDAV│ │GDrive│ │OneDr.│ │ MEGA │ │   │
│  │  └─────┘ └──────┘ └──────┘ └──────┘ └──────┘ │   │
│  └────────────────────────────────────────────────┘   │
│                       ▼                               │
│  ┌────────────────────────────────────────────────┐   │
│  │             Metadata & Sync Layer              │   │
│  │  local SQLite + ops log replicated to providers│   │
│  └────────────────────────────────────────────────┘   │
│                       ▼                               │
│  ┌────────────────────────────────────────────────┐   │
│  │            Live Sync Layer                     │   │
│  │  file diffs → patch batches → replicate to hot │   │
│  │  periodic snapshots → erasure-coded to all     │   │
│  └────────────────────────────────────────────────┘   │
└──────────────────────────────────────────────────────┘
```

## Project Structure

```
cloud-storage-freeloader/
├── cmd/
│   └── freeloader/
│       └── main.go                 # CLI entrypoint (cobra)
├── internal/
│   ├── provider/
│   │   ├── provider.go             # StorageBackend interface + registry
│   │   ├── profile.go              # ProviderProfile, StorageTier, BandwidthTracker
│   │   ├── s3/
│   │   │   └── s3.go               # S3-compatible backend
│   │   ├── webdav/
│   │   │   └── webdav.go           # WebDAV backend
│   │   ├── gdrive/
│   │   │   └── gdrive.go           # Google Drive backend
│   │   ├── onedrive/
│   │   │   └── onedrive.go         # OneDrive (Graph API) backend
│   │   └── memory/
│   │       └── memory.go           # In-memory backend (for testing)
│   ├── erasure/
│   │   ├── encoder.go              # RS encode/decode wrapper
│   │   └── chunker.go              # File → fixed-size segments
│   ├── placement/
│   │   ├── engine.go               # Tiered shard placement logic
│   │   └── bandwidth.go            # Per-provider bandwidth tracking
│   ├── metadata/
│   │   ├── store.go                # Local SQLite metadata store
│   │   ├── ops.go                  # Operation log (create/delete/rename/update)
│   │   ├── sync.go                 # Multi-device sync protocol
│   │   └── conflict.go             # Conflict detection & resolution
│   ├── vfs/
│   │   ├── fs.go                   # Virtual filesystem (POSIX operations)
│   │   ├── file.go                 # File handle (open, read, write, close)
│   │   ├── dir.go                  # Directory handle
│   │   └── cache.go                # Local segment cache (LRU)
│   ├── fuse/
│   │   └── mount.go                # cgofuse bridge to vfs
│   ├── livesync/
│   │   ├── differ.go               # File-level diff engine
│   │   ├── patch.go                # Patch format, serialization, batch compression
│   │   ├── patchlog.go             # Per-file ordered patch log with version vectors
│   │   ├── batcher.go              # Accumulate patches, flush on timer/threshold
│   │   ├── snapshot.go             # Periodic full-file snapshots (erasure-coded)
│   │   ├── syncer.go               # Push/pull patch batches, merge, apply
│   │   ├── merger.go               # Three-way merge for text files
│   │   └── watcher.go              # Filesystem watcher for local vault directories
│   └── api/
│       ├── server.go               # HTTP API server (gin)
│       └── handlers.go             # REST endpoints for web UI
├── web/                            # React SPA (embedded via go:embed)
│   └── ...
├── go.mod
├── go.sum
├── PLAN.md
└── README.md
```

## Provider Isolation Rules

All operations on providers that support folder structures (Google Drive, OneDrive)
MUST be scoped to a dedicated `cloud-storage-freeloader/` folder. This folder is
created automatically on first use. The system must NEVER read, modify, or delete
anything outside this folder. The user has existing personal data on these providers.

For S3-compatible providers (Backblaze B2, etc.), isolation is achieved via a
dedicated bucket (`freeloader-test-bucket`).

---

## Red-Green TDD Approach

Every phase follows strict red-green-refactor:

1. **RED**: Write a failing test that defines the expected behavior
2. **GREEN**: Write the minimum code to make the test pass
3. **REFACTOR**: Clean up while keeping tests green

Tests are written FIRST. No production code without a failing test driving it.

---

## Phase 1: Provider Abstraction Layer

The foundation. Everything else depends on being able to store and retrieve
opaque blobs from cloud providers.

### 1.1 — StorageBackend interface & in-memory provider

The in-memory provider is the test double for all higher layers. It also
validates the interface design before we invest in real provider adapters.

```
RED   → internal/provider/provider_test.go
        - TestRegistry_RegisterAndCreate: register a provider factory,
          create an instance via the registry, assert it implements the interface
        - TestRegistry_UnknownProvider: request unknown provider, assert error

GREEN → internal/provider/provider.go
        - Define StorageBackend interface (Put, Get, Delete, Exists, List, Available, Close)
        - Define Registry with Register() and New() functions

RED   → internal/provider/memory/memory_test.go
        - TestMemory_PutAndGet: put a blob, get it back, assert bytes match
        - TestMemory_GetNotFound: get non-existent key, assert error
        - TestMemory_Delete: put then delete, assert Exists returns false
        - TestMemory_DeleteNotFound: delete non-existent key, assert error
        - TestMemory_List: put 3 blobs with prefix, list with prefix, assert correct keys
        - TestMemory_ListEmpty: list empty store, assert empty slice
        - TestMemory_Available: assert returns configured capacity minus used
        - TestMemory_AvailableAfterPut: put a blob, assert Available decreased
        - TestMemory_PutExceedsCapacity: put blob larger than capacity, assert error

GREEN → internal/provider/memory/memory.go
        - In-memory map[string][]byte implementation
        - Configurable capacity for Available()
```

### 1.2 — Provider profiles & tiering

```
RED   → internal/provider/profile_test.go
        - TestProfile_ClassifyHot: profile with unlimited bandwidth → TierHot
        - TestProfile_ClassifyWarm: profile with moderate egress limit → TierWarm
        - TestProfile_ClassifyCold: profile with severe bandwidth limit → TierCold
        - TestProfile_ClassifyByFileSize: profile with small max file size → TierWarm
        - TestBandwidthTracker_CanDownload: within daily limit → true
        - TestBandwidthTracker_CanDownloadExceeded: over daily limit → false
        - TestBandwidthTracker_Reset: after reset time, counter resets
        - TestBandwidthTracker_Record: record usage, assert remaining decreases

GREEN → internal/provider/profile.go
        - ProviderProfile struct with all constraint fields
        - StorageTier enum (Hot, Warm, Cold)
        - Classify() method deriving tier from constraints
        - BandwidthTracker with CanDownload/CanUpload/Record/Reset
```

### 1.3 — S3 backend

```
RED   → internal/provider/s3/s3_test.go
        (Uses a local MinIO container via testcontainers-go, OR mock S3 responses)
        - TestS3_PutAndGet: upload blob, download, assert match
        - TestS3_Delete: upload, delete, assert gone
        - TestS3_List: upload several, list with prefix, assert correct
        - TestS3_PutLargeFile: upload >5MB (multipart), download, assert match
        - TestS3_GetNotFound: get non-existent, assert specific error type
        - TestS3_Profile: assert provider profile reports correct tier

GREEN → internal/provider/s3/s3.go
        - Implement StorageBackend using aws-sdk-go-v2
        - Configurable endpoint, region, credentials, bucket
        - init() registers "s3" in the global registry
```

### 1.4 — WebDAV backend

```
RED   → internal/provider/webdav/webdav_test.go
        (Uses a local WebDAV server or httptest mock)
        - TestWebDAV_PutAndGet
        - TestWebDAV_Delete
        - TestWebDAV_List
        - TestWebDAV_Exists

GREEN → internal/provider/webdav/webdav.go
        - Implement StorageBackend using gowebdav client
        - Configurable URL, username, password
        - init() registers "webdav" in the global registry
```

### 1.5 — Google Drive backend

```
RED   → internal/provider/gdrive/gdrive_test.go
        (Mock HTTP responses using httptest, test against recorded API payloads)
        - TestGDrive_PutAndGet: upload, download, assert bytes match
        - TestGDrive_Delete
        - TestGDrive_List: assert folder listing maps to flat key namespace
        - TestGDrive_Available: assert parses storageQuota response
        - TestGDrive_OAuthTokenRefresh: assert token refresh on 401

GREEN → internal/provider/gdrive/gdrive.go
        - Implement StorageBackend using Google Drive API v3
        - OAuth2 token management (store tokens, auto-refresh)
        - Map flat key namespace to folder structure
        - init() registers "gdrive" in the global registry
```

### 1.6 — OneDrive backend

```
RED   → internal/provider/onedrive/onedrive_test.go
        (Same pattern as GDrive — mock HTTP responses)
        - TestOneDrive_PutAndGet
        - TestOneDrive_PutLargeFile: >4MB triggers upload session
        - TestOneDrive_Delete
        - TestOneDrive_List
        - TestOneDrive_Available

GREEN → internal/provider/onedrive/onedrive.go
        - Implement StorageBackend using Microsoft Graph API
        - OAuth2 via Microsoft identity platform
        - Small files: direct PUT; large files: upload session
        - init() registers "onedrive" in the global registry
```

---

## Phase 2: Erasure Coding Engine

Pure data transformation layer. No I/O, no providers — just bytes in, shards out.

### 2.1 — File chunker

```
RED   → internal/erasure/chunker_test.go
        - TestChunker_ExactMultiple: 32MB file with 8MB segments → 4 segments
        - TestChunker_WithRemainder: 35MB file → 4 full + 1 padded segment
        - TestChunker_SmallFile: 100 bytes → 1 padded segment
        - TestChunker_EmptyFile: 0 bytes → 0 segments
        - TestChunker_SegmentSizeRoundtrip: chunk then reassemble, assert
          original bytes match (padding stripped by original file size)
        - TestChunker_StreamingChunk: chunk from io.Reader (not []byte),
          assert segments produced incrementally

GREEN → internal/erasure/chunker.go
        - Chunker struct with configurable SegmentSize (default 8MB)
        - Chunk(reader, fileSize) → []Segment
        - Reassemble([]Segment, originalSize) → []byte
        - Segment: {Index int, Data []byte}
```

### 2.2 — RS encoder/decoder

```
RED   → internal/erasure/encoder_test.go
        - TestEncoder_EncodeAndDecode: encode segment with RS(4,3),
          assert 7 shards produced. Decode from first 4, assert original data
        - TestEncoder_DecodeFromParityShards: drop all data shards, decode
          from 4 shards including parity, assert original data recovered
        - TestEncoder_DecodeFromMixed: drop 2 random shards, decode from
          remaining 5, assert original data
        - TestEncoder_TooFewShards: provide only 3 of required 4, assert error
        - TestEncoder_CorruptShard: flip bytes in one shard, verify detection
          (via checksum, not RS — RS itself is not error-detecting)
        - TestEncoder_DifferentParameters: test RS(6,3), RS(10,4), assert
          all produce correct shard counts and decode correctly
        - TestEncoder_LargeSegment: encode 8MB segment, decode, assert match
        - TestEncoder_ShardMetadata: each shard carries its index, total count,
          k, m, and checksum

GREEN → internal/erasure/encoder.go
        - Encoder struct wrapping klauspost/reedsolomon
        - Configurable k (data shards) and m (parity shards)
        - Encode(segment []byte) → []Shard
        - Decode(shards []Shard) → []byte (accepts any k of n shards)
        - Shard: {Index, Total, K, M int; Data []byte; Checksum [32]byte}
        - Checksums via SHA-256
```

### 2.3 — End-to-end pipeline (chunk → encode → decode → reassemble)

```
RED   → internal/erasure/pipeline_test.go
        - TestPipeline_SmallFile: 1KB file → chunk → encode → lose m shards
          per segment → decode → reassemble → assert bytes match
        - TestPipeline_LargeFile: 50MB file → same flow
        - TestPipeline_BinaryFile: random bytes (simulating video) → same flow
        - TestPipeline_DifferentShardLossPerSegment: each segment loses
          different shards, assert full reconstruction

GREEN → No new production code if 2.1 and 2.2 are correctly composed.
        These tests validate the integration. If they fail, fix encoder/chunker.
```

---

## Phase 3: Shard Placement Engine

Decides WHICH provider stores WHICH shard, respecting tiers and bandwidth limits.

### 3.1 — Placement engine

```
RED   → internal/placement/engine_test.go
        - TestPlacement_DataShardsOnHotProviders: given 4 hot + 3 cold providers,
          RS(4,3) → assert all 4 data shards placed on hot providers
        - TestPlacement_ParityShardsOnColdOK: assert parity shards placed on
          cold providers
        - TestPlacement_FallbackWhenNotEnoughHot: only 2 hot providers,
          RS(4,3) → data shards spill to warm, never cold
        - TestPlacement_RespectsFileSizeLimit: provider with 250MB max,
          shard is 300MB → skip that provider
        - TestPlacement_RespectsBandwidthBudget: provider at daily limit →
          skip for reads, still OK for writes
        - TestPlacement_SpreadAcrossProviders: no two shards of the same
          segment on the same provider
        - TestPlacement_ReturnsPlacementMap: result maps shard_index → provider_id

GREEN → internal/placement/engine.go
        - PlacementEngine with list of providers and their profiles
        - Place(k, m int, shardSizes []int64) → PlacementMap
        - PlacementMap: map[shardIndex]providerID
        - Sorting: hot first for data shards, cold for parity
        - Constraint checking: file size, bandwidth, capacity
```

### 3.2 — Read path provider selection

```
RED   → internal/placement/engine_test.go (continued)
        - TestReadPath_PreferHotProviders: given placement map, select
          providers for read → assert hot providers chosen for data shards
        - TestReadPath_DegradedMode: one hot provider down, assert falls back
          to parity shard on cold provider
        - TestReadPath_SkipBandwidthExhausted: cold provider at daily limit,
          choose a different cold provider for the needed parity shard
        - TestReadPath_AllProvidersDown: fewer than k available → assert error

GREEN → internal/placement/engine.go
        - SelectForRead(placementMap, unavailable []providerID) → []readTarget
        - readTarget: {ShardIndex, ProviderID, IsParity bool}
```

---

## Phase 4: Metadata Store

Local SQLite + operation log for multi-device sync.

### 4.1 — Local metadata store

```
RED   → internal/metadata/store_test.go
        - TestStore_CreateFile: insert file metadata, retrieve by path, assert match
        - TestStore_CreateFileWithSegments: file with 4 segments, each with 7
          shard locations → retrieve, assert full tree intact
        - TestStore_ListDirectory: create files in /docs/, list /docs/, assert correct
        - TestStore_DeleteFile: create then delete, assert not found
        - TestStore_RenameFile: rename, assert old path gone, new path exists
        - TestStore_UpdateShardLocation: move shard from provider A to B, assert updated
        - TestStore_GetShardMap: for a file, return segment→shard→provider mapping
        - TestStore_FreeSpaceAccounting: track used space per provider

GREEN → internal/metadata/store.go
        - MetadataStore backed by SQLite (via modernc.org/sqlite, pure Go)
        - Schema: files, segments, shards, providers tables
        - CRUD methods matching the test cases
```

### 4.2 — Operation log

```
RED   → internal/metadata/ops_test.go
        - TestOps_AppendAndRead: append 3 ops, read all, assert order preserved
        - TestOps_AppendGeneratesID: each op gets a UUID
        - TestOps_ReadSince: append 5 ops, read since seq=3, assert only last 2
        - TestOps_OpTypes: create ops for FileCreate, FileDelete, FileRename,
          FileUpdate → assert all serialize/deserialize correctly
        - TestOps_PerDeviceSequence: ops from device A have monotonic seq,
          device B has independent seq

GREEN → internal/metadata/ops.go
        - MetadataOperation struct (OpID, DeviceID, Timestamp, SeqNum, Type,
          Path, Payload)
        - OpLog backed by SQLite table
        - Append(op) → assigns UUID + increments device seq
        - ReadSince(deviceID, seq) → []MetadataOperation
        - ReadAll() → []MetadataOperation
```

### 4.3 — Multi-device sync protocol

```
RED   → internal/metadata/sync_test.go
        - TestSync_PushOpsToProviders: append local ops, push to 3 memory
          providers, assert ops file exists on each
        - TestSync_PullOpsFromProviders: pre-populate ops on providers,
          pull, assert local log updated
        - TestSync_MergeNonConflicting: device A creates file X, device B
          creates file Y → merge → both files exist in metadata
        - TestSync_HighWaterMark: after sync, pulling again returns no new ops
        - TestSync_ProviderDown: one provider unreachable during push,
          assert other providers still updated (best-effort)
        - TestSync_SnapshotCreateAndRestore: create snapshot of metadata,
          upload to providers, restore on a "new device" (empty store),
          assert metadata matches original

GREEN → internal/metadata/sync.go
        - SyncEngine with list of providers
        - Push(): serialize new ops, upload to all providers
        - Pull(): download ops from all providers, merge into local store
        - Snapshot(): serialize full SQLite DB, upload to all providers
        - Restore(providerID): download snapshot, hydrate local store
```

### 4.4 — Conflict detection & resolution

```
RED   → internal/metadata/conflict_test.go
        - TestConflict_NoConflict: two ops on different files → no conflict detected
        - TestConflict_EditEdit: two devices edit same file → conflict detected,
          both versions preserved (latest wins path, other becomes conflict copy)
        - TestConflict_EditDelete: one edits, one deletes same file → conflict
          detected, edit wins (safer default), delete is logged but not applied
        - TestConflict_DeleteDelete: both delete same file → no conflict, file deleted
        - TestConflict_CreateCreate: both create same path → conflict, both versions
          kept with one renamed to "name (conflict from DeviceX date).ext"
        - TestConflict_ConflictCopyNaming: assert conflict copy name format

GREEN → internal/metadata/conflict.go
        - DetectConflicts(opsA, opsB []MetadataOperation) → []Conflict
        - Conflict: {OpA, OpB MetadataOperation; Type ConflictType}
        - ResolveConflicts(conflicts []Conflict, store *MetadataStore) → []Resolution
        - Resolution: {Applied MetadataOperation; ConflictCopy *string}
```

---

## Phase 5: Virtual File System

Composes erasure, placement, metadata, and providers into POSIX-like operations.

### 5.1 — Local segment cache

```
RED   → internal/vfs/cache_test.go
        - TestCache_PutAndGet: cache a decoded segment, retrieve, assert match
        - TestCache_Eviction: fill cache to max size, add one more, assert
          oldest entry evicted
        - TestCache_HitDoesNotEvict: access cached segment, assert it survives
          subsequent evictions (LRU)
        - TestCache_Size: assert cache reports current size correctly
        - TestCache_Clear: clear cache, assert empty

GREEN → internal/vfs/cache.go
        - SegmentCache with configurable max size (bytes)
        - LRU eviction
        - Key: (fileID, segmentIndex)
```

### 5.2 — VFS read path

```
RED   → internal/vfs/fs_test.go
        (Uses memory providers + real erasure engine + real metadata store)
        - TestVFS_ReadFile: write file through VFS, read back, assert bytes match
        - TestVFS_ReadFilePartial: read byte range [100:200], assert correct slice
        - TestVFS_ReadFileDegraded: take down 1 provider (memory provider returns
          errors), read file, assert still succeeds (parity reconstruction)
        - TestVFS_ReadFileNotFound: read non-existent path, assert ENOENT
        - TestVFS_ReadDir: create 3 files in /docs/, readdir /docs/, assert
          3 entries with correct names and sizes

GREEN → internal/vfs/fs.go
        - VFS struct composing MetadataStore, PlacementEngine, Encoder,
          SegmentCache, []StorageBackend
        - Open(path) → FileHandle
        - Read(handle, offset, size) → []byte
        - ReadDir(path) → []DirEntry
        - Stat(path) → FileInfo
```

### 5.3 — VFS write path

```
RED   → internal/vfs/fs_test.go (continued)
        - TestVFS_WriteNewFile: write bytes, assert file appears in metadata,
          shards distributed across providers per placement rules
        - TestVFS_WriteUpdatesAvailableSpace: write file, assert provider
          Available() decreased
        - TestVFS_Overwrite: write file, overwrite with different content,
          read back, assert new content; assert old shards cleaned up
        - TestVFS_Delete: write then delete, assert metadata removed,
          shards removed from providers
        - TestVFS_Mkdir: create directory, assert readdir shows it
        - TestVFS_Rename: create file, rename, assert old path gone, new exists
        - TestVFS_WriteGeneratesOps: write a file, assert an ops log entry
          of type FileCreate was recorded

GREEN → internal/vfs/fs.go (continued)
        - Write(path, reader, size) → error
        - Delete(path) → error
        - Mkdir(path) → error
        - Rename(old, new) → error
        - Each mutation appends to the ops log
```

### 5.4 — VFS write path with tiered placement verification

```
RED   → internal/vfs/fs_test.go (continued)
        - TestVFS_WriteDataShardsOnHotProviders: configure 3 hot + 2 cold
          memory providers with RS(3,2). Write file. Inspect which providers
          hold data shards (index 0,1,2) → assert all on hot providers.
          Inspect parity shards (3,4) → assert on cold providers.
        - TestVFS_WriteSkipsBandwidthExhausted: exhaust a cold provider's
          daily upload budget. Write file. Assert that provider was skipped
          and a different one used for the parity shard.

GREEN → Should pass if placement engine and VFS are correctly integrated.
        If not, fix the integration wiring.
```

---

## Phase 6: FUSE / WinFsp Mount

Thin bridge between the OS filesystem interface and our VFS.

### 6.1 — FUSE bridge

```
RED   → internal/fuse/mount_test.go
        (Integration test — mounts a real FUSE filesystem using memory providers,
        performs OS-level file operations, asserts results)
        - TestFUSE_MountAndUnmount: mount, assert mount point exists, unmount,
          assert mount point empty
        - TestFUSE_WriteAndReadViaOS: mount, use os.WriteFile to create a file,
          use os.ReadFile to read it back, assert bytes match
        - TestFUSE_ListDirViaOS: mount, create files, use os.ReadDir, assert entries
        - TestFUSE_DeleteViaOS: mount, create file, os.Remove, assert gone
        - TestFUSE_LargeFileViaOS: mount, write 50MB file, read back, assert match
        - TestFUSE_StatViaOS: mount, create file, os.Stat, assert size and modtime

GREEN → internal/fuse/mount.go
        - Implement cgofuse.FileSystemInterface
        - Bridge each FUSE callback (Getattr, Readdir, Open, Read, Write,
          Create, Unlink, Rename, Mkdir, Rmdir, Statfs) to VFS methods
        - Mount(mountPoint string) / Unmount()
```

NOTE: These tests require FUSE (libfuse on Linux, WinFsp on Windows) installed.
They should be gated behind a build tag: `//go:build integration`

---

## Phase 7: CLI

### 7.1 — CLI commands

```
RED   → cmd/freeloader/main_test.go
        - TestCLI_Mount: invoke "freeloader mount X:" with test config,
          assert no error (unit test with mocked VFS)
        - TestCLI_ProviderAdd: invoke "freeloader provider add s3 --endpoint=...",
          assert config file updated
        - TestCLI_ProviderList: invoke "freeloader provider list", assert table output
        - TestCLI_Status: invoke "freeloader status", assert shows provider health,
          used/free space, shard integrity
        - TestCLI_Sync: invoke "freeloader sync", assert sync engine triggered

GREEN → cmd/freeloader/main.go
        - cobra CLI with subcommands:
          - mount <path>         — mount the virtual drive
          - unmount              — unmount
          - provider add <type>  — add a new provider (interactive OAuth or config)
          - provider list        — show configured providers + health
          - provider remove <id> — remove a provider (triggers shard migration)
          - status               — overall system health
          - sync                 — force metadata sync
          - repair               — check and repair degraded segments
```

---

## Phase 8: Metadata Sync Integration

### 8.1 — Background sync loop

```
RED   → internal/metadata/sync_integration_test.go
        - TestSync_TwoDevicesConverge: simulate two devices (two VFS instances
          with separate metadata stores but shared memory providers).
          Device A writes file X. Device B writes file Y.
          Both sync. Assert both devices see both files.
        - TestSync_ConflictResolution_Integration: both devices write to
          same path. Both sync. Assert conflict copy created on both devices.
        - TestSync_NewDeviceBootstrap: device C starts with empty metadata.
          Restores from snapshot on a provider. Asserts it sees all files.

GREEN → internal/metadata/sync.go (extended)
        - StartBackgroundSync(interval time.Duration)
        - Periodic push/pull loop
        - Snapshot creation on schedule (e.g., every 100 ops)
```

---

## Phase 9: Web UI

### 9.1 — REST API

```
RED   → internal/api/server_test.go
        - TestAPI_ListFiles: GET /api/files?path=/ → assert JSON array of entries
        - TestAPI_UploadFile: POST /api/files with multipart body → assert 201,
          file appears in subsequent list
        - TestAPI_DownloadFile: GET /api/files/download?path=/test.txt → assert
          correct bytes in response body
        - TestAPI_DeleteFile: DELETE /api/files?path=/test.txt → assert 200,
          file gone from list
        - TestAPI_Rename: PATCH /api/files?path=/old.txt with body {newPath: "/new.txt"}
          → assert renamed
        - TestAPI_Status: GET /api/status → assert JSON with provider health,
          space usage, shard integrity summary
        - TestAPI_Providers: GET /api/providers → assert list with tier, health,
          space used/free

GREEN → internal/api/server.go, internal/api/handlers.go
        - gin router with the above endpoints
        - Handlers delegate to VFS
        - Serve embedded SPA for all non-/api/ routes
```

### 9.2 — React SPA (skeleton)

```
This is not TDD in the traditional sense (UI tests are integration/E2E).
Implement a minimal file browser:
- File list with icons, sizes, dates
- Upload button (drag and drop)
- Download on click
- Delete, rename context menu
- Provider status sidebar
- Used/free space bar per provider with tier color coding

Tech: React + Vite + TailwindCSS, embedded via go:embed
```

---

## Phase 10: Provider Migration & Repair

### 10.1 — Shard repair

```
RED   → internal/placement/repair_test.go
        - TestRepair_DetectDegraded: one provider permanently down, scan all
          segments, assert list of degraded segments returned
        - TestRepair_RebuildShard: degraded segment with 1 missing shard,
          download k shards from healthy providers, re-encode, upload
          replacement shard to a new provider, assert segment fully healthy
        - TestRepair_NoHealthyReplacement: all providers full, assert
          repair returns error with "no capacity available"

GREEN → internal/placement/repair.go
        - Scan() → []DegradedSegment
        - Repair(segment) → error
```

### 10.2 — Provider removal / migration

```
RED   → internal/placement/migration_test.go
        - TestMigration_RemoveProvider: remove a provider, assert all its
          shards migrated to remaining providers, metadata updated
        - TestMigration_AddProvider: add a new provider, rebalance hot
          data shards onto it if it is a hot-tier provider
        - TestMigration_ProgressCallback: assert migration reports progress
          (X of Y shards migrated)

GREEN → internal/placement/migration.go
        - Migrate(fromProvider, toProvider) → error
        - Rebalance() → error
        - Both report progress via callback
```

---

## Phase 11: Live Sync Layer

Near-real-time file synchronization for apps like Obsidian. Uses a two-tier
approach: frequent lightweight patches (replicated) for speed, and periodic
erasure-coded snapshots for durability. Designed for single-user multi-device
scenarios, not real-time collaborative editing.

### Storage strategy

```
┌──────────────────────────────────────────────────────────┐
│                                                          │
│  PATCHES (tiny, frequent, time-critical)                 │
│  - File-level diffs (not keystroke-level)                │
│  - Batched every 10-30 seconds                          │
│  - Compressed (gzip) + encrypted (AES-256-GCM)          │
│  - Replicated to 2-3 hot providers (NOT erasure-coded)  │
│  - Ephemeral: garbage-collected after snapshot covers    │
│                                                          │
│  SNAPSHOTS (medium, infrequent, durable)                 │
│  - Full file content at a point in time                  │
│  - Created every 50 patches or 10 minutes               │
│  - Erasure-coded RS(k,m) across all providers           │
│  - Permanent until superseded by newer snapshot          │
│  - Recovery = latest snapshot + patches since            │
│                                                          │
└──────────────────────────────────────────────────────────┘

Why not erasure-code patches?
  A 200-byte patch RS(4,3)-encoded = 7 API calls for 350 bytes of data
  with ~3,500 bytes of HTTP overhead. 17x amplification.
  Replicating to 3 providers = 3 API calls, 600 bytes. Much cheaper.
  Patches are small enough that replication overhead < erasure overhead.
```

### Write flow

```
 App writes file to local vault (e.g., Obsidian saves after 2s debounce)
        │
        ▼
 Filesystem watcher detects change
        │
        ▼
 Differ computes diff against cached previous version
        │
        ▼
 Patch {op, offset, data, fileHash, seq, deviceID} appended to local buffer
        │
        ▼
 Batch timer fires (10-30s of accumulated patches)
        │
        ├──→ Compress batch (gzip)
        ├──→ Encrypt (AES-256-GCM with vault key)
        ├──→ Upload to 2-3 hot providers (replicated, NOT erasure-coded)
        └──→ Update version vector
        │
        ▼
 Snapshot timer fires (every 50 patches or 10 minutes)
        │
        ├──→ Read current file content
        ├──→ RS encode → distribute shards across ALL providers (erasure-coded)
        ├──→ Record snapshot version in metadata
        └──→ Mark covered patches as garbage-collectible
```

### Read/sync flow on another device

```
 Sync timer fires (every 10-30s)
        │
        ▼
 Pull new patch batches from hot providers
        │
        ├──→ Decrypt + decompress
        ├──→ Check version vector: any new patches from other devices?
        │       │
        │       ├── No → done
        │       └── Yes ↓
        │
        ▼
 Conflict check against local uncommitted patches
        │
        ├── Different files → apply both (no conflict)
        ├── Same file, different regions → three-way merge (auto-resolve)
        ├── Same file, overlapping regions → conflict copy created
        │
        ▼
 Write updated file to local vault directory
        │
        ▼
 OS filesystem watcher fires → app (Obsidian) reloads note
```

### 11.1 — Diff engine

```
RED   → internal/livesync/differ_test.go
        - TestDiff_Insert: "Hello world" → "Hello beautiful world"
          assert patch: {Insert, offset=6, data="beautiful "}
        - TestDiff_Delete: "Hello beautiful world" → "Hello world"
          assert patch: {Delete, offset=6, length=10}
        - TestDiff_Replace: "Hello world" → "Hello earth"
          assert patch captures the replacement
        - TestDiff_NoChange: identical files → empty patch list
        - TestDiff_NewFile: old=nil, new="content" → patch is full content
        - TestDiff_DeletedFile: old="content", new=nil → delete patch
        - TestDiff_BinaryFile: random bytes changed → produces valid patch
        - TestDiff_LargeFile: 1MB file with 10 byte change → patch is small
          (not a full-file copy)
        - TestDiff_ApplyPatch: compute diff, apply patch to original,
          assert result matches new version

GREEN → internal/livesync/differ.go
        - Diff(old, new []byte) → []Patch
        - ApplyPatches(base []byte, patches []Patch) → []byte
        - Uses sergi/go-diff or similar for efficient Myers diff
        - Patch: {Type (Insert/Delete/Replace), Offset, Data, Length}
```

### 11.2 — Patch format & serialization

```
RED   → internal/livesync/patch_test.go
        - TestPatch_Serialize: create patch, serialize to bytes, deserialize,
          assert roundtrip match
        - TestPatch_BatchSerialize: create 10 patches, serialize as batch,
          deserialize, assert all 10 intact and ordered
        - TestPatch_Compress: serialize batch, compress, assert compressed
          size < uncompressed
        - TestPatch_Encrypt: compress then encrypt with AES-256-GCM,
          decrypt, decompress, assert original patches recovered
        - TestPatch_EncryptWrongKey: encrypt with key A, decrypt with key B,
          assert error (authentication failure)
        - TestPatch_Metadata: each patch carries fileHash, seq, deviceID,
          timestamp — assert all preserved through serialization

GREEN → internal/livesync/patch.go
        - Patch struct with all fields
        - PatchBatch: ordered list of patches with version vector
        - Serialize/Deserialize (protobuf or msgpack)
        - Compress/Decompress (gzip)
        - Encrypt/Decrypt (AES-256-GCM)
```

### 11.3 — Patch log with version vectors

```
RED   → internal/livesync/patchlog_test.go
        - TestPatchLog_Append: append patches, read back, assert order
        - TestPatchLog_PerFileLog: patches for file A and file B stored
          separately, queryable by path
        - TestPatchLog_VersionVector: after appending 5 patches from
          device "desktop", version vector shows {desktop: 5}
        - TestPatchLog_MergeVectors: merge vectors from two devices,
          assert combined vector correct
        - TestPatchLog_PatchesSince: request patches since {desktop: 3},
          get patches 4 and 5 only
        - TestPatchLog_GarbageCollect: mark snapshot at seq=50, garbage
          collect, assert patches 1-50 deleted, 51+ remain

GREEN → internal/livesync/patchlog.go
        - PatchLog backed by SQLite (same DB as metadata, separate table)
        - Append(patch) → increments device seq
        - PatchesSince(versionVector) → []Patch
        - GarbageCollect(snapshotSeq) → deletes covered patches
        - VersionVector: map[deviceID]uint64
```

### 11.4 — Batcher

```
RED   → internal/livesync/batcher_test.go
        - TestBatcher_AccumulatesPatches: add 5 patches, assert no flush
          before timer
        - TestBatcher_FlushOnTimer: add patches, advance clock by 15s,
          assert batch flushed (callback invoked with compressed+encrypted
          batch)
        - TestBatcher_FlushOnThreshold: add 100 patches (exceeds count
          threshold), assert immediate flush without waiting for timer
        - TestBatcher_FlushOnSizeThreshold: add patches totaling >64KB,
          assert immediate flush
        - TestBatcher_EmptyFlush: timer fires with no patches → no flush,
          no callback
        - TestBatcher_ConfigurableInterval: set interval to 30s, assert
          flush happens at 30s not 15s

GREEN → internal/livesync/batcher.go
        - Batcher with configurable FlushInterval (default 15s),
          MaxPatches (default 100), MaxBytes (default 64KB)
        - Add(patch) — may trigger immediate flush if threshold exceeded
        - Background goroutine flushes on timer
        - OnFlush callback: func(batch PatchBatch) error
```

### 11.5 — Snapshot manager

```
RED   → internal/livesync/snapshot_test.go
        - TestSnapshot_Create: given current file content, create snapshot,
          assert snapshot stored as erasure-coded shards via the existing
          VFS write path
        - TestSnapshot_Restore: create snapshot, restore from shards,
          assert content matches original
        - TestSnapshot_TriggeredByPatchCount: configure threshold=50,
          append 50 patches, assert snapshot created automatically
        - TestSnapshot_TriggeredByTime: configure interval=10min,
          advance clock, assert snapshot created
        - TestSnapshot_RecoveryFlow: create snapshot at seq=100, add
          patches 101-120, simulate "new device" → restore snapshot,
          apply patches 101-120, assert file matches current state
        - TestSnapshot_GarbageCollectsOldPatches: after snapshot at
          seq=100, assert patches ≤100 are garbage-collected
        - TestSnapshot_SupersedesOldSnapshot: create snapshot v1 and v2,
          assert v1 shards cleaned up from providers

GREEN → internal/livesync/snapshot.go
        - SnapshotManager with configurable PatchThreshold (default 50),
          TimeInterval (default 10min)
        - CreateSnapshot(path, content) → uses erasure engine + placement
        - RestoreSnapshot(path, version) → []byte
        - Hooks into PatchLog to trigger on count/time thresholds
        - Coordinates with GarbageCollect after snapshot creation
```

### 11.6 — Syncer (push/pull patch batches between devices)

```
RED   → internal/livesync/syncer_test.go
        (Uses memory providers throughout)
        - TestSyncer_PushBatch: flush a batch, push to 3 providers,
          assert batch file exists on each
        - TestSyncer_PullBatch: pre-populate batches on providers from
          "another device", pull, assert patches applied to local log
        - TestSyncer_PullOnlyNew: pull once (gets all), pull again
          (gets nothing new — high-water mark works)
        - TestSyncer_ProviderDown: one of 3 providers down during push,
          assert other 2 still receive the batch
        - TestSyncer_PullFromAnyProvider: batch exists on 3 providers,
          one is down, pull succeeds from remaining 2
        - TestSyncer_TwoDevicesConverge: device A edits file X,
          device B edits file Y. Both push. Both pull. Assert both
          devices have both files updated.

GREEN → internal/livesync/syncer.go
        - Syncer with list of hot providers for patch replication
        - Push(batch) → replicate to N hot providers
        - Pull() → download new batches from providers, apply to local log
        - Tracks per-device high-water marks to avoid re-downloading
        - Provider storage layout:
            provider:/.cloudfs/patches/{deviceID}/{seqStart}-{seqEnd}.batch
```

### 11.7 — Three-way text merge

```
RED   → internal/livesync/merger_test.go
        - TestMerge_NonOverlapping: base="Line1\nLine2\nLine3",
          A edits Line1, B edits Line3 → auto-merge succeeds, both edits present
        - TestMerge_SameEditBothSides: both A and B make the same edit
          → merge succeeds, edit appears once (not duplicated)
        - TestMerge_ConflictOverlapping: base="Hello world",
          A→"Hello beautiful world", B→"Hello cruel world"
          → merge returns conflict with both versions
        - TestMerge_InsertDifferentPositions: A inserts at line 2,
          B inserts at line 5 → auto-merge succeeds, both insertions present
        - TestMerge_OneEditsOneDeletes: A edits line 3, B deletes line 3
          → conflict detected, A's edit preserved (safer default)
        - TestMerge_EmptyBase: base is empty, both A and B create content
          → conflict, both versions kept

GREEN → internal/livesync/merger.go
        - ThreeWayMerge(base, ours, theirs []byte) → (merged []byte, conflicts []MergeConflict, err error)
        - MergeConflict: {BaseContent, OursContent, TheirsContent string; LineRange [2]int}
        - Uses line-level diff3 algorithm
        - On conflict: returns both versions, caller decides (conflict copy or user prompt)
```

### 11.8 — Filesystem watcher (for local vault mode)

```
RED   → internal/livesync/watcher_test.go
        - TestWatcher_DetectsNewFile: create a file in watched dir,
          assert event received with path and type=Created
        - TestWatcher_DetectsModification: modify existing file,
          assert event received with type=Modified
        - TestWatcher_DetectsDeletion: delete file, assert event
          received with type=Deleted
        - TestWatcher_DetectsRename: rename file, assert event(s) received
        - TestWatcher_IgnoresPatterns: configure ignore patterns
          (e.g., "*.tmp", ".git/"), assert those changes not reported
        - TestWatcher_Debounce: rapid-fire 10 modifications to same file
          within 500ms, assert only 1 event reported (debounced)
        - TestWatcher_Recursive: create file in subdirectory, assert detected
        - TestWatcher_SelfWriteFilter: watcher ignores writes made by the
          syncer itself (to avoid echo loops)

GREEN → internal/livesync/watcher.go
        - Watcher using fsnotify/fsnotify (cross-platform Go library)
        - Watch(dir string, ignore []glob) → chan FileEvent
        - FileEvent: {Path, Type (Created/Modified/Deleted/Renamed), Timestamp}
        - Built-in debounce (configurable, default 500ms)
        - Self-write detection: maintain set of "expected writes" from syncer,
          filter them out of reported events
```

### 11.9 — Integration: end-to-end live sync

```
RED   → internal/livesync/integration_test.go
        (Full integration test with two simulated devices, memory providers)

        - TestLiveSync_EndToEnd:
          1. Device A writes "Hello" to notes.md
          2. Batcher flushes, syncer pushes batch
          3. Device B pulls, applies patch
          4. Assert device B's notes.md contains "Hello"

        - TestLiveSync_BidirectionalEditing:
          1. Device A writes to notes.md
          2. Device B writes to todo.md
          3. Both sync
          4. Assert both devices have both files

        - TestLiveSync_ConflictProducesConflictCopy:
          1. Both devices edit notes.md concurrently (no sync between edits)
          2. Both sync
          3. Assert one device has notes.md + conflict copy
          4. Assert no data lost

        - TestLiveSync_SnapshotAndRecovery:
          1. Device A makes 60 edits (exceeds snapshot threshold of 50)
          2. Assert snapshot created (erasure-coded)
          3. Device C bootstraps from scratch: restores snapshot,
             applies patches 51-60
          4. Assert device C sees current file state

        - TestLiveSync_OfflineDevice:
          1. Device A makes 200 edits over "days" (multiple snapshots)
          2. Device B comes online after being offline
          3. Device B pulls latest snapshot + patches since
          4. Assert device B fully caught up

GREEN → Validates integration of all livesync components.
        If tests fail, fix wiring between components.
```

---

## Implementation Order & Dependencies

```
Phase 1 ──→ Phase 2 ──→ Phase 3 ──→ Phase 5 ──→ Phase 6 ──→ Phase 7
  │                                    ↑            ↑
  │           Phase 4 ─────────────────┘            │
  │             │                                   │
  │             └──→ Phase 8 ───────────────────────┘
  │
  ├──────────────────────────────→ Phase 9
  │                                  │
  │                               Phase 10
  │
  └──→ Phase 11 (11.1-11.4 need no deps; 11.5+ need Phases 1,2,3)
         │
         └──→ 11.5 (snapshots) needs erasure engine (Phase 2) + placement (Phase 3)
         └──→ 11.6 (syncer) needs provider abstraction (Phase 1)
         └──→ 11.9 (integration) needs all of the above
```

- **Phase 1** (providers) has no dependencies — start here
- **Phase 2** (erasure) has no dependencies — can parallelize with Phase 1
- **Phase 3** (placement) depends on Phase 1 (provider profiles)
- **Phase 4** (metadata) has no dependencies — can parallelize with Phases 1-2
- **Phase 5** (VFS) depends on Phases 1, 2, 3, 4
- **Phase 6** (FUSE) depends on Phase 5
- **Phase 7** (CLI) depends on Phases 5, 6
- **Phase 8** (sync integration) depends on Phases 4, 5
- **Phase 9** (web UI) depends on Phase 5
- **Phase 10** (repair/migration) depends on Phases 3, 4, 5
- **Phase 11** (live sync) — sub-phases 11.1-11.4 (differ, patch, patchlog, batcher)
  have no dependencies and can start in parallel with everything. Sub-phases
  11.5+ (snapshots, syncer, integration) depend on Phases 1, 2, 3

## Key Dependencies (Go Modules)

```
github.com/klauspost/reedsolomon    # erasure coding
github.com/winfsp/cgofuse           # cross-platform FUSE
github.com/spf13/cobra              # CLI framework
github.com/gin-gonic/gin            # HTTP framework
modernc.org/sqlite                  # pure-Go SQLite
github.com/aws/aws-sdk-go-v2       # S3 provider
github.com/google/uuid              # operation IDs
golang.org/x/oauth2                 # OAuth2 flows
github.com/sergi/go-diff            # Myers diff algorithm (live sync patches)
github.com/fsnotify/fsnotify        # cross-platform filesystem watcher
gopkg.in/yaml.v3                    # YAML config parsing
```

---

## Phase 12: Config Loading from YAML

Wire `config.local.yaml` into typed, testable Go structs so the CLI and
provider factory can consume provider credentials.

### 12.1 — Config types and YAML parsing

```
RED   → internal/config/config_test.go
        - TestConfig_LoadFromBytes: parse YAML with all 3 providers, assert
          Providers slice length 3, each with correct Name/Type
        - TestConfig_LoadFromFile: write temp YAML file, LoadFile(), assert parsed
        - TestConfig_BackblazeFields: assert Endpoint, Region, Bucket, KeyID,
          ApplicationKey populated
        - TestConfig_GDriveFields: assert ClientID, ClientSecret, TokensFile,
          BaseFolder populated
        - TestConfig_OneDriveFields: assert ClientID, AuthEndpoint, PublicClient=true,
          TokensFile, BaseFolder populated
        - TestConfig_MissingFile: LoadFile("nonexistent") returns error
        - TestConfig_ProviderByName: cfg.Provider("backblaze") returns entry,
          cfg.Provider("unknown") returns nil
        - TestConfig_BaseFolder: assert top-level base_folder parsed
        - TestConfig_TierField: assert tier parsed as "hot"/"warm"/"cold"

GREEN → internal/config/config.go
        - Config struct, ProviderConfig struct with yaml tags
        - LoadFile(path) / LoadBytes(data) → (*Config, error)
        - Provider(name) → *ProviderConfig helper
```

### 12.2 — OAuth2 token persistence

```
RED   → internal/config/token_store_test.go
        - TestTokenStore_LoadTokens: load gdrive_tokens.json format, assert
          AccessToken, RefreshToken, ExpiresIn parsed
        - TestTokenStore_SaveTokens: save to temp file, re-load, assert roundtrip
        - TestTokenStore_MissingFile: assert descriptive error

GREEN → internal/config/token_store.go
        - TokenData struct, LoadTokens(), SaveTokens() (atomic write via
          temp file + rename)
```

---

## Phase 13: Real S3 Backend (aws-sdk-go-v2)

The existing `s3.Backend` delegates to an injected store via `NewWithStore()`.
This phase adds `NewFromConfig()` that creates a real AWS SDK client targeting
Backblaze B2.

### 13.1 — S3 real operations via httptest mock

```
RED   → internal/provider/s3/s3_real_test.go
        - TestS3Real_PutAndGet: httptest server mimicking S3 PutObject/GetObject,
          assert correct bucket/key in URL, body matches
        - TestS3Real_Delete: httptest returns 204 on DeleteObject
        - TestS3Real_Exists_True: httptest returns 200 on HeadObject
        - TestS3Real_Exists_False: httptest returns 404
        - TestS3Real_List: httptest returns ListObjectsV2 XML with 3 keys,
          assert parsed correctly
        - TestS3Real_GetNotFound: httptest returns 404/NoSuchKey, assert error

GREEN → internal/provider/s3/s3.go
        - NewFromConfig(cfg map[string]string) (*Backend, error)
        - Real Put/Get/Delete/Exists/List using aws-sdk-go-v2
        - Dispatches via store (test) or client (real) based on which is set
```

### 13.2 — S3 integration tests (real Backblaze B2)

```
RED   → internal/provider/s3/s3_integration_test.go
        //go:build integration

        - TestS3Integration_PutGetDelete: roundtrip to freeloader-test-bucket
        - TestS3Integration_ListWithPrefix: put 3, list by prefix, assert 3
        - TestS3Integration_LargeObject: 10MB roundtrip

        Config loaded from config.local.yaml. t.Cleanup() deletes test objects.

GREEN → Already implemented in 13.1.
```

---

## Phase 14: Real Google Drive Backend (Drive API v3)

### 14.1 — OAuth2 token source with auto-refresh

```
RED   → internal/provider/gdrive/oauth_test.go
        - TestGDriveOAuth_TokenSource: create from test tokens, assert valid
        - TestGDriveOAuth_RefreshOnExpiry: httptest OAuth server, expired token,
          assert refresh request sent with correct refresh_token
        - TestGDriveOAuth_RefreshPersists: after refresh, tokens file updated

GREEN → internal/provider/gdrive/oauth.go
        - NewTokenSource(clientID, clientSecret, tokensFile) → PersistentTokenSource
        - Wraps oauth2.TokenSource, saves refreshed tokens to disk
```

### 14.2 — Google Drive API operations (httptest mock)

```
RED   → internal/provider/gdrive/gdrive_real_test.go
        - TestGDriveReal_EnsureBaseFolder: folder doesn't exist → created
        - TestGDriveReal_EnsureBaseFolderExists: folder exists → no create
        - TestGDriveReal_PutAndGet: multipart upload, download via alt=media
        - TestGDriveReal_Delete: delete file by ID
        - TestGDriveReal_List: list files in folder with prefix filter
        - TestGDriveReal_Available: parse storageQuota from /about
        - TestGDriveReal_ScopedToFolder: ALL queries include base folder filter

GREEN → internal/provider/gdrive/gdrive.go
        - NewFromConfig(cfg) → Backend with real HTTP client
        - Key mapping: "/" → "__" for flat file names in base folder
        - ensureBaseFolder(): find or create "cloud-storage-freeloader/"
        - Put/Get/Delete/Exists/List/Available via Drive API v3
```

### 14.3 — Google Drive integration tests

```
RED   → internal/provider/gdrive/gdrive_integration_test.go
        //go:build integration

        - TestGDriveIntegration_PutGetDelete: real roundtrip
        - TestGDriveIntegration_ListWithPrefix
        - TestGDriveIntegration_Available: positive space
        - TestGDriveIntegration_ScopeVerification: nothing outside base folder

GREEN → Already implemented in 14.2.
```

---

## Phase 15: Real OneDrive Backend (Microsoft Graph API)

### 15.1 — OneDrive OAuth2 (public client, no secret)

```
RED   → internal/provider/onedrive/oauth_test.go
        - TestOneDriveOAuth_TokenSource: valid token source
        - TestOneDriveOAuth_RefreshPublicClient: no client_secret in refresh
        - TestOneDriveOAuth_RefreshPersists: tokens file updated

GREEN → internal/provider/onedrive/oauth.go
        - Public client refresh: grant_type=refresh_token, client_id only
```

### 15.2 — OneDrive Graph API operations (httptest mock)

```
RED   → internal/provider/onedrive/onedrive_real_test.go
        - TestOneDriveReal_EnsureBaseFolder: create if missing
        - TestOneDriveReal_Put: small file (<4MB) direct PUT
        - TestOneDriveReal_PutLargeFile: >4MB triggers upload session
        - TestOneDriveReal_Get: download via /content
        - TestOneDriveReal_Delete: DELETE by item ID
        - TestOneDriveReal_List: list children with prefix
        - TestOneDriveReal_Available: parse quota.remaining
        - TestOneDriveReal_ScopedToFolder: all paths under base folder

GREEN → internal/provider/onedrive/onedrive.go
        - NewFromConfig(cfg) → Backend with real HTTP client
        - Key mapping: "/" → "__" for flat names
        - Put: small=direct PUT, large=upload session
        - All ops via Graph API /me/drive/root:/cloud-storage-freeloader/...
```

### 15.3 — OneDrive integration tests

```
RED   → internal/provider/onedrive/onedrive_integration_test.go
        //go:build integration

        - TestOneDriveIntegration_PutGetDelete
        - TestOneDriveIntegration_LargeFile: 5MB upload session
        - TestOneDriveIntegration_Available

GREEN → Already implemented in 15.2.
```

---

## Phase 16: Provider Factory (Config → Real Backends)

### 16.1 — Factory wiring

```
RED   → internal/provider/factory_test.go
        - TestFactory_CreateAll: load Config with 3 providers, create all,
          assert 3 backends in map[string]StorageBackend
        - TestFactory_NamesMatchConfig: map keys = provider names
        - TestFactory_UnknownType: type="ftp" → error

GREEN → internal/provider/factory.go
        - CreateBackends(cfg) → (map[string]StorageBackend, []ProviderInfo, error)
        - Switch on Type: "s3"→s3.NewFromConfig, "gdrive"→gdrive.NewFromConfig,
          "onedrive"→onedrive.NewFromConfig
```

### 16.2 — CLI mount command wiring

```
GREEN → cmd/freeloader/main.go
        - Update mount command: load config → create backends → create VFS →
          mount via FUSE
        - Add --config flag (default "config.local.yaml")
        - Add --mount-point flag (default "X:")
```

---

## Phase 17: WinFsp Bridge (cgofuse)

Requires: WinFsp installed (https://winfsp.dev), MinGW gcc for CGO.

### 17.1 — cgofuse FileSystemInterface

```
RED   → internal/fuse/bridge_test.go
        //go:build cgo && windows

        - TestBridge_Getattr_Root: returns directory attributes
        - TestBridge_Getattr_File: correct size after VFS write
        - TestBridge_Readdir: correct entries from VFS
        - TestBridge_CreateWriteRead: Create → Write → Read roundtrip
        - TestBridge_Unlink: file gone after delete
        - TestBridge_Rename: old gone, new exists
        - TestBridge_Statfs: reasonable fs stats

GREEN → internal/fuse/bridge.go
        //go:build cgo && windows

        - Bridge struct implementing cgofuse.FileSystemInterface
        - Write buffering per file handle (flush on Release)
        - Translate FUSE calls → VFS calls
```

### 17.2 — Mount/Unmount with drive letter

```
RED   → internal/fuse/mount_cgo_test.go
        //go:build cgo && windows && integration

        - TestMount_WriteReadViaOS: mount X:, os.WriteFile, os.ReadFile
        - TestMount_ListDirViaOS: create files, os.ReadDir
        - TestMount_DeleteViaOS: os.Remove, assert gone

GREEN → internal/fuse/mount.go
        - Mount(): create Bridge → cgofuse.FileSystemHost → host.Mount()
        - Unmount(): host.Unmount()
```

---

## Phase 18: End-to-End Verification

### 18.1 — Full pipeline integration test

```
RED   → internal/e2e/e2e_test.go
        //go:build integration && cgo && windows

        - TestE2E_WriteFileAndVerifyShards:
          1. Load config, create real backends (Backblaze, GDrive, OneDrive)
          2. RS(2,1) — 2 data + 1 parity for 3 providers
          3. Mount at X:
          4. os.WriteFile("X:\\test-e2e.txt", "Hello from E2E!")
          5. Query each provider: assert shards exist
          6. Unmount, remount, os.ReadFile → assert same content
          7. Clean up all shards

        - TestE2E_ProviderDistribution:
          1. Write file, inspect shard map
          2. Assert data shards on hot providers (Backblaze, GDrive)
          3. Assert parity shard on warm provider (OneDrive)
          4. Assert no provider holds duplicate shards

GREEN → No new production code. Validates full pipeline. Fix wiring if tests fail.
```

---

## Extended Implementation Order

```
Phases 1-11 (done) ──→ Phase 12 (config) ──→ Phase 13 (S3) ─────┐
                                         ├──→ Phase 14 (GDrive) ──┼──→ Phase 16 (factory) ──→ Phase 18 (E2E)
                                         └──→ Phase 15 (OneDrive)─┘          ↑
                                                                   Phase 17 (WinFsp) ──┘
```

## Design Decisions

1. **Key mapping**: GDrive/OneDrive use flat file names with "/" replaced by "__"
   (e.g. `shards__myfile__seg0__shard1`). Avoids deep folder nesting overhead.

2. **Write buffering in FUSE**: VFS.Write() needs full file data for erasure coding,
   so the FUSE bridge buffers all Write() calls per file handle and flushes on Release().

3. **RS(2,1) for 3 providers**: 2 data shards on hot (Backblaze, GDrive), 1 parity
   on warm (OneDrive). Survives any single provider failure.

4. **Build tags**: `integration` for real cloud tests, `cgo && windows` for WinFsp.
   `go test ./...` stays fast and offline by default.

5. **Token persistence**: OAuth2 tokens saved after every refresh (atomic write).
   App survives restarts without re-authorization.

## Prerequisites for Phase 17-18

- Install WinFsp: https://winfsp.dev/rel/ (download .msi installer)
- Install MinGW gcc: `choco install mingw` or https://www.mingw-w64.org/
- Set `CGO_ENABLED=1` in environment
