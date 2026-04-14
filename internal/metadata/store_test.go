package metadata

import (
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestStore_CreateFile(t *testing.T) {
	s := newTestStore(t)

	f := &FileMeta{
		Path: "/docs/readme.txt",
		Size: 1024,
	}
	if err := s.CreateFile(f); err != nil {
		t.Fatalf("CreateFile failed: %v", err)
	}

	got, err := s.GetFile("/docs/readme.txt")
	if err != nil {
		t.Fatalf("GetFile failed: %v", err)
	}
	if got.Path != f.Path || got.Size != f.Size {
		t.Fatalf("got %+v, want %+v", got, f)
	}
}

func TestStore_CreateFileWithSegments(t *testing.T) {
	s := newTestStore(t)

	f := &FileMeta{Path: "/data/video.mp4", Size: 32 * 1024 * 1024}
	s.CreateFile(f)

	// Add 4 segments, each with 7 shard locations
	for segIdx := 0; segIdx < 4; segIdx++ {
		for shardIdx := 0; shardIdx < 7; shardIdx++ {
			err := s.AddShard(f.Path, segIdx, shardIdx, "provider-"+string(rune('A'+shardIdx)))
			if err != nil {
				t.Fatalf("AddShard(%d,%d) failed: %v", segIdx, shardIdx, err)
			}
		}
	}

	shardMap, err := s.GetShardMap(f.Path)
	if err != nil {
		t.Fatalf("GetShardMap failed: %v", err)
	}
	if len(shardMap) != 4 {
		t.Fatalf("got %d segments, want 4", len(shardMap))
	}
	for segIdx := 0; segIdx < 4; segIdx++ {
		if len(shardMap[segIdx]) != 7 {
			t.Fatalf("segment %d has %d shards, want 7", segIdx, len(shardMap[segIdx]))
		}
	}
}

func TestStore_ListDirectory(t *testing.T) {
	s := newTestStore(t)

	s.CreateFile(&FileMeta{Path: "/docs/a.txt", Size: 10})
	s.CreateFile(&FileMeta{Path: "/docs/b.txt", Size: 20})
	s.CreateFile(&FileMeta{Path: "/docs/c.txt", Size: 30})
	s.CreateFile(&FileMeta{Path: "/other/d.txt", Size: 40})

	entries, err := s.ListDirectory("/docs/")
	if err != nil {
		t.Fatalf("ListDirectory failed: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}
}

func TestStore_DeleteFile(t *testing.T) {
	s := newTestStore(t)

	s.CreateFile(&FileMeta{Path: "/tmp/del.txt", Size: 5})
	if err := s.DeleteFile("/tmp/del.txt"); err != nil {
		t.Fatalf("DeleteFile failed: %v", err)
	}
	_, err := s.GetFile("/tmp/del.txt")
	if err == nil {
		t.Fatal("expected error for deleted file")
	}
}

func TestStore_RenameFile(t *testing.T) {
	s := newTestStore(t)

	s.CreateFile(&FileMeta{Path: "/old.txt", Size: 10})
	if err := s.RenameFile("/old.txt", "/new.txt"); err != nil {
		t.Fatalf("RenameFile failed: %v", err)
	}

	_, err := s.GetFile("/old.txt")
	if err == nil {
		t.Fatal("old path should not exist")
	}
	got, err := s.GetFile("/new.txt")
	if err != nil {
		t.Fatalf("new path not found: %v", err)
	}
	if got.Size != 10 {
		t.Fatalf("got size %d, want 10", got.Size)
	}
}

func TestStore_UpdateShardLocation(t *testing.T) {
	s := newTestStore(t)

	s.CreateFile(&FileMeta{Path: "/f.bin", Size: 100})
	s.AddShard("/f.bin", 0, 0, "providerA")

	if err := s.UpdateShardLocation("/f.bin", 0, 0, "providerB"); err != nil {
		t.Fatalf("UpdateShardLocation failed: %v", err)
	}

	shardMap, _ := s.GetShardMap("/f.bin")
	if shardMap[0][0] != "providerB" {
		t.Fatalf("got provider %s, want providerB", shardMap[0][0])
	}
}

func TestStore_GetShardMap(t *testing.T) {
	s := newTestStore(t)

	s.CreateFile(&FileMeta{Path: "/mapped.bin", Size: 200})
	s.AddShard("/mapped.bin", 0, 0, "p1")
	s.AddShard("/mapped.bin", 0, 1, "p2")
	s.AddShard("/mapped.bin", 1, 0, "p3")

	shardMap, err := s.GetShardMap("/mapped.bin")
	if err != nil {
		t.Fatalf("GetShardMap failed: %v", err)
	}
	if shardMap[0][0] != "p1" || shardMap[0][1] != "p2" || shardMap[1][0] != "p3" {
		t.Fatalf("unexpected shard map: %+v", shardMap)
	}
}

func TestStore_FreeSpaceAccounting(t *testing.T) {
	s := newTestStore(t)

	s.RecordProviderUsage("p1", 1000)
	s.RecordProviderUsage("p1", 500)
	s.RecordProviderUsage("p2", 200)

	used, err := s.GetProviderUsage("p1")
	if err != nil {
		t.Fatalf("GetProviderUsage failed: %v", err)
	}
	if used != 1500 {
		t.Fatalf("got %d, want 1500", used)
	}
}
