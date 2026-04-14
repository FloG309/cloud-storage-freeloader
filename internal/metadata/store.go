package metadata

import (
	"database/sql"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

// FileMeta holds metadata about a stored file.
type FileMeta struct {
	Path string
	Size int64
}

// ShardMap maps segmentIndex -> shardIndex -> providerID.
type ShardMap map[int]map[int]string

// Store is the local SQLite metadata store.
type Store struct {
	db *sql.DB
}

// NewStore creates a metadata store backed by SQLite at the given path.
// Use ":memory:" for an in-memory database.
func NewStore(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS files (
			path TEXT PRIMARY KEY,
			size INTEGER NOT NULL
		);
		CREATE TABLE IF NOT EXISTS shards (
			file_path TEXT NOT NULL,
			segment_index INTEGER NOT NULL,
			shard_index INTEGER NOT NULL,
			provider_id TEXT NOT NULL,
			PRIMARY KEY (file_path, segment_index, shard_index)
		);
		CREATE TABLE IF NOT EXISTS provider_usage (
			provider_id TEXT NOT NULL,
			used_bytes INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (provider_id)
		);
	`)
	return err
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// CreateFile inserts file metadata.
func (s *Store) CreateFile(f *FileMeta) error {
	_, err := s.db.Exec("INSERT INTO files (path, size) VALUES (?, ?)", f.Path, f.Size)
	return err
}

// GetFile retrieves file metadata by path.
func (s *Store) GetFile(path string) (*FileMeta, error) {
	f := &FileMeta{}
	err := s.db.QueryRow("SELECT path, size FROM files WHERE path = ?", path).Scan(&f.Path, &f.Size)
	if err != nil {
		return nil, fmt.Errorf("file not found: %s", path)
	}
	return f, nil
}

// ListDirectory lists files whose path starts with the given directory prefix.
func (s *Store) ListDirectory(dir string) ([]*FileMeta, error) {
	rows, err := s.db.Query("SELECT path, size FROM files WHERE path LIKE ?", dir+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []*FileMeta
	for rows.Next() {
		f := &FileMeta{}
		if err := rows.Scan(&f.Path, &f.Size); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, nil
}

// DeleteFile removes a file and its shards.
func (s *Store) DeleteFile(path string) error {
	// Delete shards first (foreign key may not cascade with modernc/sqlite)
	s.db.Exec("DELETE FROM shards WHERE file_path = ?", path)
	_, err := s.db.Exec("DELETE FROM files WHERE path = ?", path)
	return err
}

// RenameFile renames a file path.
func (s *Store) RenameFile(oldPath, newPath string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec("UPDATE files SET path = ? WHERE path = ?", newPath, oldPath)
	if err != nil {
		return err
	}
	_, err = tx.Exec("UPDATE shards SET file_path = ? WHERE file_path = ?", newPath, oldPath)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// AddShard records a shard location.
func (s *Store) AddShard(filePath string, segmentIndex, shardIndex int, providerID string) error {
	_, err := s.db.Exec(
		"INSERT INTO shards (file_path, segment_index, shard_index, provider_id) VALUES (?, ?, ?, ?)",
		filePath, segmentIndex, shardIndex, providerID,
	)
	return err
}

// UpdateShardLocation changes a shard's provider.
func (s *Store) UpdateShardLocation(filePath string, segmentIndex, shardIndex int, providerID string) error {
	_, err := s.db.Exec(
		"UPDATE shards SET provider_id = ? WHERE file_path = ? AND segment_index = ? AND shard_index = ?",
		providerID, filePath, segmentIndex, shardIndex,
	)
	return err
}

// GetShardMap returns the full segment -> shard -> provider mapping for a file.
func (s *Store) GetShardMap(filePath string) (ShardMap, error) {
	rows, err := s.db.Query(
		"SELECT segment_index, shard_index, provider_id FROM shards WHERE file_path = ? ORDER BY segment_index, shard_index",
		filePath,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sm := make(ShardMap)
	for rows.Next() {
		var segIdx, shardIdx int
		var pid string
		if err := rows.Scan(&segIdx, &shardIdx, &pid); err != nil {
			return nil, err
		}
		if sm[segIdx] == nil {
			sm[segIdx] = make(map[int]string)
		}
		sm[segIdx][shardIdx] = pid
	}
	return sm, nil
}

// RecordProviderUsage adds bytes to a provider's usage total.
func (s *Store) RecordProviderUsage(providerID string, bytes int64) error {
	_, err := s.db.Exec(`
		INSERT INTO provider_usage (provider_id, used_bytes) VALUES (?, ?)
		ON CONFLICT(provider_id) DO UPDATE SET used_bytes = used_bytes + excluded.used_bytes`,
		providerID, bytes,
	)
	return err
}

// GetProviderUsage returns the total bytes used by a provider.
func (s *Store) GetProviderUsage(providerID string) (int64, error) {
	var used int64
	err := s.db.QueryRow("SELECT used_bytes FROM provider_usage WHERE provider_id = ?", providerID).Scan(&used)
	if err != nil {
		return 0, err
	}
	return used, nil
}

// DeleteShards removes all shards for a file.
func (s *Store) DeleteShards(filePath string) error {
	_, err := s.db.Exec("DELETE FROM shards WHERE file_path = ?", filePath)
	return err
}

// ListAllFiles returns all file paths matching the optional prefix.
func (s *Store) ListAllFiles(prefix string) ([]string, error) {
	query := "SELECT path FROM files"
	var args []interface{}
	if prefix != "" {
		query += " WHERE path LIKE ?"
		args = append(args, prefix+"%")
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var paths []string
	for rows.Next() {
		var p string
		rows.Scan(&p)
		paths = append(paths, p)
	}
	return paths, nil
}

// Mkdir creates a directory marker.
func (s *Store) Mkdir(path string) error {
	if !strings.HasSuffix(path, "/") {
		path += "/"
	}
	_, err := s.db.Exec("INSERT OR IGNORE INTO files (path, size) VALUES (?, 0)", path)
	return err
}
