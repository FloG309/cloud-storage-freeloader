package metadata

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/FloG309/cloud-storage-freeloader/internal/provider"
)

// SyncEngine handles pushing/pulling ops and snapshots across providers.
type SyncEngine struct {
	opLog     *OpLog
	store     *Store
	providers []provider.StorageBackend
	deviceID  string
	// Track what we've already pushed per device
	pushedSeq map[string]int64
	// Track what we've already pulled per device
	pulledSeq map[string]int64
}

// NewSyncEngine creates a sync engine.
func NewSyncEngine(opLog *OpLog, store *Store, providers []provider.StorageBackend, deviceID string) *SyncEngine {
	return &SyncEngine{
		opLog:     opLog,
		store:     store,
		providers: providers,
		deviceID:  deviceID,
		pushedSeq: make(map[string]int64),
		pulledSeq: make(map[string]int64),
	}
}

type opsPayload struct {
	Ops []opRecord `json:"ops"`
}

type opRecord struct {
	OpID     string `json:"op_id"`
	DeviceID string `json:"device_id"`
	SeqNum   int64  `json:"seq_num"`
	Type     string `json:"type"`
	Path     string `json:"path"`
}

// Push serializes new ops and uploads to all providers (best-effort).
func (se *SyncEngine) Push(ctx context.Context) error {
	lastPushed := se.pushedSeq[se.deviceID]
	ops, err := se.opLog.ReadSince(se.deviceID, lastPushed)
	if err != nil {
		return err
	}
	if len(ops) == 0 {
		return nil
	}

	records := make([]opRecord, len(ops))
	for i, op := range ops {
		records[i] = opRecord{
			OpID:     op.OpID,
			DeviceID: op.DeviceID,
			SeqNum:   op.SeqNum,
			Type:     string(op.Type),
			Path:     op.Path,
		}
	}

	data, err := json.Marshal(opsPayload{Ops: records})
	if err != nil {
		return err
	}

	maxSeq := ops[len(ops)-1].SeqNum
	key := fmt.Sprintf(".cloudfs/ops/%s/%d.json", se.deviceID, maxSeq)

	for _, p := range se.providers {
		p.Put(ctx, key, data) // best-effort, ignore errors
	}

	se.pushedSeq[se.deviceID] = maxSeq
	return nil
}

// Pull downloads ops from all providers and merges into local log.
func (se *SyncEngine) Pull(ctx context.Context) error {
	for _, p := range se.providers {
		keys, err := p.List(ctx, ".cloudfs/ops/")
		if err != nil {
			continue // best-effort
		}

		for _, key := range keys {
			// Extract device ID from key
			parts := strings.Split(key, "/")
			if len(parts) < 4 {
				continue
			}
			deviceID := parts[2]
			if deviceID == se.deviceID {
				continue // skip own ops
			}

			data, err := p.Get(ctx, key)
			if err != nil {
				continue
			}

			var payload opsPayload
			if err := json.Unmarshal(data, &payload); err != nil {
				continue
			}

			for _, rec := range payload.Ops {
				if rec.SeqNum <= se.pulledSeq[rec.DeviceID] {
					continue // already pulled
				}

				// Check if this op already exists in our log
				existing, _ := se.opLog.ReadSince(rec.DeviceID, rec.SeqNum-1)
				alreadyHave := false
				for _, e := range existing {
					if e.OpID == rec.OpID {
						alreadyHave = true
						break
					}
				}
				if alreadyHave {
					if rec.SeqNum > se.pulledSeq[rec.DeviceID] {
						se.pulledSeq[rec.DeviceID] = rec.SeqNum
					}
					continue
				}

				se.opLog.Append(rec.DeviceID, OpType(rec.Type), rec.Path, nil)
				if rec.SeqNum > se.pulledSeq[rec.DeviceID] {
					se.pulledSeq[rec.DeviceID] = rec.SeqNum
				}
			}
		}
	}
	return nil
}

// Snapshot serializes the full metadata database and uploads to all providers.
func (se *SyncEngine) Snapshot(ctx context.Context) error {
	// Export all files as JSON
	files, err := se.store.ListAllFiles("")
	if err != nil {
		return err
	}

	type snapshotData struct {
		Files []FileMeta `json:"files"`
	}

	var snap snapshotData
	for _, path := range files {
		f, err := se.store.GetFile(path)
		if err != nil {
			continue
		}
		snap.Files = append(snap.Files, *f)
	}

	data, err := json.Marshal(snap)
	if err != nil {
		return err
	}

	key := ".cloudfs/snapshot/latest.json"
	for _, p := range se.providers {
		p.Put(ctx, key, data)
	}
	return nil
}

// Restore downloads the latest snapshot and hydrates the local store.
func (se *SyncEngine) Restore(ctx context.Context) error {
	type snapshotData struct {
		Files []FileMeta `json:"files"`
	}

	for _, p := range se.providers {
		data, err := p.Get(ctx, ".cloudfs/snapshot/latest.json")
		if err != nil {
			continue
		}

		var snap snapshotData
		if err := json.Unmarshal(data, &snap); err != nil {
			continue
		}

		for _, f := range snap.Files {
			fc := f
			// Check if file already exists
			if _, err := se.store.GetFile(fc.Path); err == nil {
				continue
			}
			if err := se.store.CreateFile(&fc); err != nil {
				continue
			}
		}
		return nil
	}
	return fmt.Errorf("no snapshot available from any provider")
}

// DB returns the underlying database connection (for sharing with OpLog).
func (s *Store) DB() *sql.DB {
	return s.db
}
