package livesync

import (
	"database/sql"
	"encoding/json"

	_ "modernc.org/sqlite"
)

// VersionVector tracks per-device sequence numbers.
type VersionVector map[string]uint64

// PatchLog stores per-file ordered patches with version vectors.
type PatchLog struct {
	db *sql.DB
}

// NewPatchLog creates a patch log backed by SQLite.
func NewPatchLog(dbPath string) (*PatchLog, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS patch_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			file_path TEXT NOT NULL,
			device_id TEXT NOT NULL,
			seq INTEGER NOT NULL,
			patches_json BLOB NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_patchlog_file ON patch_log(file_path);
		CREATE INDEX IF NOT EXISTS idx_patchlog_device_seq ON patch_log(device_id, seq);
	`)
	if err != nil {
		db.Close()
		return nil, err
	}
	return &PatchLog{db: db}, nil
}

// Close closes the database.
func (pl *PatchLog) Close() error {
	return pl.db.Close()
}

// Append adds patches to the log for a file.
func (pl *PatchLog) Append(filePath, deviceID string, patches []Patch) error {
	// Get next seq for this device
	var maxSeq sql.NullInt64
	pl.db.QueryRow("SELECT MAX(seq) FROM patch_log WHERE device_id = ?", deviceID).Scan(&maxSeq)
	nextSeq := int64(1)
	if maxSeq.Valid {
		nextSeq = maxSeq.Int64 + 1
	}

	patchJSON, err := json.Marshal(patches)
	if err != nil {
		return err
	}

	_, err = pl.db.Exec(
		"INSERT INTO patch_log (file_path, device_id, seq, patches_json) VALUES (?, ?, ?, ?)",
		filePath, deviceID, nextSeq, patchJSON,
	)
	return err
}

// PatchesSince returns patches for a file since the given version vector.
func (pl *PatchLog) PatchesSince(filePath string, since VersionVector) ([]StoredPatch, error) {
	rows, err := pl.db.Query(
		"SELECT id, file_path, device_id, seq, patches_json FROM patch_log WHERE file_path = ? ORDER BY id",
		filePath,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []StoredPatch
	for rows.Next() {
		var sp StoredPatch
		var patchJSON []byte
		if err := rows.Scan(&sp.ID, &sp.FilePath, &sp.DeviceID, &sp.Seq, &patchJSON); err != nil {
			return nil, err
		}
		json.Unmarshal(patchJSON, &sp.Patches)

		// Filter by version vector
		if sinceSeq, ok := since[sp.DeviceID]; ok && uint64(sp.Seq) <= sinceSeq {
			continue
		}
		result = append(result, sp)
	}
	return result, nil
}

// GetVersionVector returns the current version vector across all devices.
func (pl *PatchLog) GetVersionVector() VersionVector {
	rows, _ := pl.db.Query("SELECT device_id, MAX(seq) FROM patch_log GROUP BY device_id")
	if rows == nil {
		return VersionVector{}
	}
	defer rows.Close()

	vv := make(VersionVector)
	for rows.Next() {
		var device string
		var seq uint64
		rows.Scan(&device, &seq)
		vv[device] = seq
	}
	return vv
}

// GarbageCollect removes patches covered by a snapshot at the given version.
func (pl *PatchLog) GarbageCollect(filePath string, snapshotVersion VersionVector) error {
	for deviceID, seq := range snapshotVersion {
		_, err := pl.db.Exec(
			"DELETE FROM patch_log WHERE file_path = ? AND device_id = ? AND seq <= ?",
			filePath, deviceID, seq,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

// MergeVersionVectors combines two version vectors, taking the max for each device.
func MergeVersionVectors(a, b VersionVector) VersionVector {
	result := make(VersionVector)
	for k, v := range a {
		result[k] = v
	}
	for k, v := range b {
		if existing, ok := result[k]; !ok || v > existing {
			result[k] = v
		}
	}
	return result
}

// StoredPatch is a patch entry from the log.
type StoredPatch struct {
	ID       int64
	FilePath string
	DeviceID string
	Seq      int64
	Patches  []Patch
}
