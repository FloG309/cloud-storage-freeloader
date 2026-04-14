package metadata

import (
	"testing"
)

func newTestOpLog(t *testing.T) *OpLog {
	t.Helper()
	s := newTestStore(t)
	ol, err := NewOpLog(s.db)
	if err != nil {
		t.Fatalf("NewOpLog failed: %v", err)
	}
	return ol
}

func TestOps_AppendAndRead(t *testing.T) {
	ol := newTestOpLog(t)

	ol.Append("device1", OpFileCreate, "/a.txt", nil)
	ol.Append("device1", OpFileCreate, "/b.txt", nil)
	ol.Append("device1", OpFileCreate, "/c.txt", nil)

	ops, err := ol.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}
	if len(ops) != 3 {
		t.Fatalf("got %d ops, want 3", len(ops))
	}
	// Order preserved
	if ops[0].Path != "/a.txt" || ops[2].Path != "/c.txt" {
		t.Fatal("order not preserved")
	}
}

func TestOps_AppendGeneratesID(t *testing.T) {
	ol := newTestOpLog(t)

	op, err := ol.Append("device1", OpFileCreate, "/a.txt", nil)
	if err != nil {
		t.Fatalf("Append failed: %v", err)
	}
	if op.OpID == "" {
		t.Fatal("expected non-empty OpID")
	}
}

func TestOps_ReadSince(t *testing.T) {
	ol := newTestOpLog(t)

	for i := 0; i < 5; i++ {
		ol.Append("device1", OpFileCreate, "/"+string(rune('a'+i))+".txt", nil)
	}

	ops, err := ol.ReadSince("device1", 3)
	if err != nil {
		t.Fatalf("ReadSince failed: %v", err)
	}
	if len(ops) != 2 {
		t.Fatalf("got %d ops, want 2", len(ops))
	}
}

func TestOps_OpTypes(t *testing.T) {
	ol := newTestOpLog(t)

	types := []OpType{OpFileCreate, OpFileDelete, OpFileRename, OpFileUpdate}
	for _, typ := range types {
		ol.Append("dev1", typ, "/test.txt", nil)
	}

	ops, _ := ol.ReadAll()
	if len(ops) != 4 {
		t.Fatalf("got %d ops, want 4", len(ops))
	}
	for i, op := range ops {
		if op.Type != types[i] {
			t.Fatalf("op %d: got type %v, want %v", i, op.Type, types[i])
		}
	}
}

func TestOps_PerDeviceSequence(t *testing.T) {
	ol := newTestOpLog(t)

	ol.Append("deviceA", OpFileCreate, "/a1.txt", nil)
	ol.Append("deviceA", OpFileCreate, "/a2.txt", nil)
	ol.Append("deviceB", OpFileCreate, "/b1.txt", nil)
	ol.Append("deviceA", OpFileCreate, "/a3.txt", nil)
	ol.Append("deviceB", OpFileCreate, "/b2.txt", nil)

	opsA, _ := ol.ReadSince("deviceA", 0)
	if len(opsA) != 3 {
		t.Fatalf("got %d ops for deviceA, want 3", len(opsA))
	}
	// Verify monotonic sequence
	for i := 1; i < len(opsA); i++ {
		if opsA[i].SeqNum <= opsA[i-1].SeqNum {
			t.Fatalf("non-monotonic seq: %d <= %d", opsA[i].SeqNum, opsA[i-1].SeqNum)
		}
	}

	opsB, _ := ol.ReadSince("deviceB", 0)
	if len(opsB) != 2 {
		t.Fatalf("got %d ops for deviceB, want 2", len(opsB))
	}
}
