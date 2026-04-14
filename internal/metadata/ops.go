package metadata

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
)

// OpType represents the type of metadata operation.
type OpType string

const (
	OpFileCreate OpType = "file_create"
	OpFileDelete OpType = "file_delete"
	OpFileRename OpType = "file_rename"
	OpFileUpdate OpType = "file_update"
)

// MetadataOperation represents a single metadata change.
type MetadataOperation struct {
	OpID      string
	DeviceID  string
	Timestamp time.Time
	SeqNum    int64
	Type      OpType
	Path      string
	Payload   []byte
}

// OpLog is an append-only log of metadata operations.
type OpLog struct {
	db *sql.DB
}

// NewOpLog creates an operation log using the given database connection.
func NewOpLog(db *sql.DB) (*OpLog, error) {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS ops (
			op_id TEXT PRIMARY KEY,
			device_id TEXT NOT NULL,
			timestamp TEXT NOT NULL,
			seq_num INTEGER NOT NULL,
			type TEXT NOT NULL,
			path TEXT NOT NULL,
			payload BLOB
		);
		CREATE INDEX IF NOT EXISTS idx_ops_device_seq ON ops(device_id, seq_num);
	`)
	if err != nil {
		return nil, err
	}
	return &OpLog{db: db}, nil
}

// Append adds an operation to the log, assigning a UUID and incrementing the device sequence.
func (ol *OpLog) Append(deviceID string, opType OpType, path string, payload []byte) (*MetadataOperation, error) {
	// Get next seq for this device
	var maxSeq sql.NullInt64
	ol.db.QueryRow("SELECT MAX(seq_num) FROM ops WHERE device_id = ?", deviceID).Scan(&maxSeq)
	nextSeq := int64(1)
	if maxSeq.Valid {
		nextSeq = maxSeq.Int64 + 1
	}

	op := &MetadataOperation{
		OpID:      uuid.New().String(),
		DeviceID:  deviceID,
		Timestamp: time.Now(),
		SeqNum:    nextSeq,
		Type:      opType,
		Path:      path,
		Payload:   payload,
	}

	_, err := ol.db.Exec(
		"INSERT INTO ops (op_id, device_id, timestamp, seq_num, type, path, payload) VALUES (?, ?, ?, ?, ?, ?, ?)",
		op.OpID, op.DeviceID, op.Timestamp.Format(time.RFC3339Nano), op.SeqNum, string(op.Type), op.Path, op.Payload,
	)
	if err != nil {
		return nil, err
	}
	return op, nil
}

// ReadAll returns all operations in insertion order.
func (ol *OpLog) ReadAll() ([]MetadataOperation, error) {
	rows, err := ol.db.Query("SELECT op_id, device_id, timestamp, seq_num, type, path, payload FROM ops ORDER BY rowid")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanOps(rows)
}

// ReadSince returns operations for a device with seq > since.
func (ol *OpLog) ReadSince(deviceID string, since int64) ([]MetadataOperation, error) {
	rows, err := ol.db.Query(
		"SELECT op_id, device_id, timestamp, seq_num, type, path, payload FROM ops WHERE device_id = ? AND seq_num > ? ORDER BY seq_num",
		deviceID, since,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanOps(rows)
}

func scanOps(rows *sql.Rows) ([]MetadataOperation, error) {
	var ops []MetadataOperation
	for rows.Next() {
		var op MetadataOperation
		var ts string
		var opType string
		if err := rows.Scan(&op.OpID, &op.DeviceID, &ts, &op.SeqNum, &opType, &op.Path, &op.Payload); err != nil {
			return nil, err
		}
		op.Type = OpType(opType)
		op.Timestamp, _ = time.Parse(time.RFC3339Nano, ts)
		ops = append(ops, op)
	}
	return ops, nil
}
