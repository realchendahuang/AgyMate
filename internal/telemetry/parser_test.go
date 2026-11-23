package telemetry

import (
	"context"
	"database/sql"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/realchendahuang/AgyMate/internal/instances"
)

func encodeVarint(v uint64) []byte {
	var buf [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(buf[:], v)
	return buf[:n]
}

func pbVarint(fn uint64, val uint64) []byte {
	out := encodeVarint((fn << 3) | 0)
	return append(out, encodeVarint(val)...)
}

func pbBytes(fn uint64, payload []byte) []byte {
	out := encodeVarint((fn << 3) | 2)
	out = append(out, encodeVarint(uint64(len(payload)))...)
	return append(out, payload...)
}

func TestParseSingleDBAndComputeCosts(t *testing.T) {
	tmp := t.TempDir()
	gdir := filepath.Join(tmp, "antigravity")
	convDir := filepath.Join(gdir, "conversations")
	if err := os.MkdirAll(convDir, 0o700); err != nil {
		t.Fatal(err)
	}

	cid := "11111111-1111-1111-1111-111111111111"
	dbPath := filepath.Join(convDir, cid+".db")
	con, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = con.Exec(`
		CREATE TABLE steps (idx integer PRIMARY KEY, step_type integer, metadata blob, permissions blob, step_payload blob);
		CREATE TABLE gen_metadata (idx integer PRIMARY KEY, data blob, size integer);
	`)
	if err != nil {
		t.Fatal(err)
	}

	nowTS := uint64(time.Now().Unix())
	stepMeta := pbBytes(1, pbVarint(1, nowTS))
	_, err = con.Exec(`INSERT INTO steps (idx, step_type, metadata) VALUES (1, 1, ?)`, stepMeta)
	if err != nil {
		t.Fatal(err)
	}

	u4 := append(pbVarint(1, 1427), pbVarint(2, 2000)...)
	u4 = append(u4, pbVarint(3, 500)...)
	u4 = append(u4, pbVarint(5, 10000)...)
	u4 = append(u4, pbBytes(7, []byte("bot-1"))...)
	u4 = append(u4, pbVarint(9, 200)...)
	u4 = append(u4, pbVarint(10, 300)...)

	kv := append(pbBytes(1, []byte("last_step_index")), pbBytes(2, []byte("1"))...)
	m1 := append(pbVarint(3, 1427), pbBytes(4, u4)...)
	m1 = append(m1, pbBytes(19, []byte("gemini-4-carbon-high"))...)
	m1 = append(m1, pbBytes(20, kv)...)
	m1 = append(m1, pbBytes(21, []byte("Gemini 4 Carbon (High)"))...)
	genBlob := pbBytes(1, m1)
	_, err = con.Exec(`INSERT INTO gen_metadata (idx, data, size) VALUES (0, ?, ?)`, genBlob, len(genBlob))
	if err != nil {
		t.Fatal(err)
	}
	con.Close()

	store := NewStore(nil)
	rep := store.QueryTelemetry([]instances.Instance{
		{ID: "main", GeminiDirs: []string{gdir}},
	}, "today", "std", "all", 100)

	if rep.Summary.Calls != 1 {
		t.Fatalf("expected 1 call, got %d", rep.Summary.Calls)
	}
	if rep.Summary.InputTokens != 2000 || rep.Summary.CacheTokens != 10000 || rep.Summary.OutputTokens != 500 {
		t.Fatalf("unexpected token counts: %+v", rep.Summary)
	}
	if rep.Summary.TotalUSD <= 0 {
		t.Fatalf("expected positive USD valuation, got %v", rep.Summary.TotalUSD)
	}
	if len(rep.ByModel) != 1 || rep.ByModel[0].ModelName != "Gemini 4 Carbon (High)" {
		t.Fatalf("unexpected model breakdown: %+v", rep.ByModel)
	}
}

func TestResolveModelNameNewModelsAndDynamicFallback(t *testing.T) {
	labelFn := func(id string) string {
		if id == "MODEL_PLACEHOLDER_M599" {
			return "Gemini 4 Experimental (XHigh)"
		}
		return ""
	}
	if got := resolveModelName(1535, "", "", labelFn); got != "Gemini 4 Neon (XHigh)" {
		t.Fatalf("enum 1535 = %q, want Gemini 4 Neon (XHigh)", got)
	}
	if got := resolveModelName(1537, "", "", labelFn); got != "Orchestrator (Neon)" {
		t.Fatalf("enum 1537 = %q, want Orchestrator (Neon)", got)
	}
	if got := resolveModelName(1403, "", "", labelFn); got != "Claude Sonnet 5.5 (Low)" {
		t.Fatalf("enum 1403 = %q, want Claude Sonnet 5.5 (Low)", got)
	}
	if got := resolveModelName(1599, "", "", labelFn); got != "Gemini 4 Experimental (XHigh)" {
		t.Fatalf("dynamic placeholder M599 = %q, want Gemini 4 Experimental (XHigh)", got)
	}
}

func TestCacheScanPreservesNonUUIDAndKeepCount(t *testing.T) {
	tmp := t.TempDir()
	gdir := filepath.Join(tmp, "antigravity")
	brain := filepath.Join(gdir, "brain")
	convs := filepath.Join(gdir, "conversations")
	_ = os.MkdirAll(filepath.Join(brain, "tempmediaStorage"), 0o700)
	_ = os.WriteFile(filepath.Join(brain, "tempmediaStorage", "keep.bin"), []byte("12345"), 0o600)

	oldID := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	newID := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	_ = os.MkdirAll(filepath.Join(brain, oldID), 0o700)
	_ = os.WriteFile(filepath.Join(brain, oldID, "x.md"), make([]byte, 1024), 0o600)
	_ = os.MkdirAll(filepath.Join(brain, newID), 0o700)
	_ = os.WriteFile(filepath.Join(brain, newID, "y.md"), make([]byte, 512), 0o600)
	_ = os.MkdirAll(convs, 0o700)
	_ = os.WriteFile(filepath.Join(convs, oldID+".db"), make([]byte, 2048), 0o600)

	oldTime := time.Now().Add(-48 * time.Hour)
	_ = os.Chtimes(filepath.Join(brain, oldID, "x.md"), oldTime, oldTime)
	_ = os.Chtimes(filepath.Join(convs, oldID+".db"), oldTime, oldTime)

	store := NewStore(nil)
	res := store.CleanCache([]instances.Instance{{ID: "p1", GeminiDirs: []string{gdir}}}, 1, "all", map[string]bool{})
	if res.DeletedCount != 1 {
		t.Fatalf("expected 1 deleted conversation, got %d", res.DeletedCount)
	}
	if _, err := os.Stat(filepath.Join(brain, "tempmediaStorage", "keep.bin")); err != nil {
		t.Fatalf("non-UUID dir tempmediaStorage was touched: %v", err)
	}
	if _, err := os.Stat(filepath.Join(brain, newID, "y.md")); err != nil {
		t.Fatalf("newest keepCount=1 item was deleted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(brain, oldID)); !os.IsNotExist(err) {
		t.Fatalf("old item should have been removed")
	}
	_ = context.Background()
}
