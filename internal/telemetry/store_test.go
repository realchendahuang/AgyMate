package telemetry

import (
	"context"
	"database/sql"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/realchendahuang/AgyMate/internal/instances"
)

func encodeVarintField(fieldNum uint64, val uint64) []byte {
	var buf []byte
	buf = binary.AppendUvarint(buf, (fieldNum<<3)|0)
	buf = binary.AppendUvarint(buf, val)
	return buf
}

func encodeBytesField(fieldNum uint64, data []byte) []byte {
	var buf []byte
	buf = binary.AppendUvarint(buf, (fieldNum<<3)|2)
	buf = binary.AppendUvarint(buf, uint64(len(data)))
	buf = append(buf, data...)
	return buf
}

func makeStepMetadataBlob(ts int64, input, cache, output uint64, botID string) []byte {
	tsMsg := encodeVarintField(1, uint64(ts))
	var u9 []byte
	u9 = append(u9, encodeVarintField(1, 1)...)
	u9 = append(u9, encodeVarintField(2, input)...)
	u9 = append(u9, encodeVarintField(3, output)...)
	u9 = append(u9, encodeVarintField(5, cache)...)
	u9 = append(u9, encodeBytesField(7, []byte(botID))...)

	var meta []byte
	meta = append(meta, encodeBytesField(1, tsMsg)...)
	meta = append(meta, encodeBytesField(9, u9)...)
	return meta
}

func createTestConvDB(t *testing.T, dbPath string, metaBlob []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatal(err)
	}
	con, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer con.Close()
	if _, err := con.Exec(`
		CREATE TABLE steps (idx INTEGER PRIMARY KEY, step_type INTEGER, metadata BLOB);
		CREATE TABLE gen_metadata (idx INTEGER PRIMARY KEY, data BLOB);
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := con.Exec(`INSERT INTO steps(idx, step_type, metadata) VALUES(1, 1, ?)`, metaBlob); err != nil {
		t.Fatal(err)
	}
}

func createTestSummaryDB(t *testing.T, sumDB, cid, title, ws, modified string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(sumDB), 0o755); err != nil {
		t.Fatal(err)
	}
	con, err := sql.Open("sqlite", sumDB)
	if err != nil {
		t.Fatal(err)
	}
	defer con.Close()
	if _, err := con.Exec(`
		CREATE TABLE conversation_summaries (
			conversation_id TEXT PRIMARY KEY,
			parent_conversation_id TEXT,
			title TEXT,
			workspace_uris TEXT,
			status TEXT,
			step_count INTEGER,
			last_modified_time TEXT
		);
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := con.Exec(
		`INSERT INTO conversation_summaries VALUES(?, '', ?, ?, 'IDLE', 3, ?)`,
		cid, title, ws, modified,
	); err != nil {
		t.Fatal(err)
	}
}

func TestAccountAggregationAndFilterAcrossTelemetrySearchAndCache(t *testing.T) {
	tmp := t.TempDir()
	mainGDir := filepath.Join(tmp, "main-gemini")
	ideGDir := filepath.Join(tmp, "ide-gemini")
	p1GDir := filepath.Join(tmp, "p1-gemini")

	now := time.Now()
	nowTS := now.Unix()
	uuidMainOld := "11111111-1111-1111-1111-111111111111"
	uuidMainNew := "22222222-2222-2222-2222-222222222222"
	uuidIDE := "33333333-3333-3333-3333-333333333333"
	uuidP1Old := "44444444-4444-4444-4444-444444444444"
	uuidP1New := "55555555-5555-5555-5555-555555555555"

	createTestConvDB(t, filepath.Join(mainGDir, "conversations", uuidMainNew+".db"), makeStepMetadataBlob(nowTS-60, 1000, 2000, 300, "b-main"))
	createTestConvDB(t, filepath.Join(mainGDir, "conversations", uuidMainOld+".db"), makeStepMetadataBlob(nowTS-300, 100, 0, 50, "b-main-old"))
	createTestConvDB(t, filepath.Join(ideGDir, "conversations", uuidIDE+".db"), makeStepMetadataBlob(nowTS-30, 500, 1500, 200, "b-ide"))
	createTestConvDB(t, filepath.Join(p1GDir, "conversations", uuidP1New+".db"), makeStepMetadataBlob(nowTS-20, 400, 400, 100, "b-p1"))
	createTestConvDB(t, filepath.Join(p1GDir, "conversations", uuidP1Old+".db"), makeStepMetadataBlob(nowTS-400, 100, 0, 20, "b-p1-old"))

	// Make old UUID DB files clearly older than new ones so keepCount=1 protects new and exposes old.
	oldTime := now.Add(-2 * time.Hour)
	for _, oldPath := range []string{
		filepath.Join(mainGDir, "conversations", uuidMainOld+".db"),
		filepath.Join(p1GDir, "conversations", uuidP1Old+".db"),
	} {
		_ = os.Chtimes(oldPath, oldTime, oldTime)
	}

	createTestSummaryDB(t, filepath.Join(mainGDir, "conversation_summaries.db"), uuidMainNew, "Shared Workspace Task", "file:///Users/dev/projA", now.Format(time.RFC3339))
	createTestSummaryDB(t, filepath.Join(ideGDir, "conversation_summaries.db"), uuidIDE, "IDE Followup Task", "file:///Users/dev/projB", now.Add(-time.Minute).Format(time.RFC3339))
	createTestSummaryDB(t, filepath.Join(p1GDir, "conversation_summaries.db"), uuidP1New, "Isolated Alt Task", "file:///Users/dev/projC", now.Add(-2*time.Minute).Format(time.RFC3339))

	instList := []instances.Instance{
		{ID: "main", Label: "Antigravity", Account: "real@gmail.com", GeminiDirs: []string{mainGDir}},
		{ID: "ide", Label: "Antigravity IDE", Account: "REAL@gmail.com", GeminiDirs: []string{ideGDir}},
		{ID: "p1", Label: "P1", Account: "alt@gmail.com", GeminiDirs: []string{p1GDir}},
	}

	store := NewStore(nil)
	rep := store.QueryTelemetry(instList, "today", "std", "all", 249.99)
	if len(rep.ByAccount) != 2 {
		t.Fatalf("expected 2 merged accounts in ByAccount, got %d: %+v", len(rep.ByAccount), rep.ByAccount)
	}
	var realAcct, altAcct *AccountAggregate
	for i := range rep.ByAccount {
		switch rep.ByAccount[i].Account {
		case "real@gmail.com", "REAL@gmail.com":
			realAcct = &rep.ByAccount[i]
		case "alt@gmail.com":
			altAcct = &rep.ByAccount[i]
		}
	}
	if realAcct == nil || altAcct == nil {
		t.Fatalf("missing expected accounts in ByAccount: %+v", rep.ByAccount)
	}
	if realAcct.Calls != 3 || realAcct.Input != 1600 || realAcct.Cache != 3500 || len(realAcct.InstanceIDs) != 2 {
		t.Fatalf("unexpected merged realAcct aggregate: %+v", realAcct)
	}
	if len(rep.RecentCalls) == 0 || rep.RecentCalls[0].Account == "" {
		t.Fatalf("expected RecentCall.Account populated, got %+v", rep.RecentCalls)
	}

	// Verify acct:<email> filter merges main+ide only.
	filtered := store.QueryTelemetry(instList, "today", "std", "acct:real@gmail.com", 0)
	if filtered.Summary.Calls != 3 {
		t.Fatalf("expected 3 calls for acct:real@gmail.com, got %d", filtered.Summary.Calls)
	}
	if len(filtered.ByInstance) != 2 {
		t.Fatalf("expected main+ide in filtered ByInstance, got %+v", filtered.ByInstance)
	}
	for _, ia := range filtered.ByInstance {
		if ia.Input == 0 {
			t.Fatalf("expected InstAggregate.Input populated, got %+v", ia)
		}
	}

	// Verify slot-level filter still works.
	ideOnly := store.QueryTelemetry(instList, "today", "std", "ide", 0)
	if ideOnly.Summary.Calls != 1 {
		t.Fatalf("expected 1 call for slot filter ide, got %d", ideOnly.Summary.Calls)
	}

	// SearchConversations honors acct:<email>.
	hits := store.SearchConversations(context.Background(), instList, "", "acct:real@gmail.com", 20)
	if len(hits) != 2 {
		t.Fatalf("expected 2 search hits for acct:real@gmail.com, got %d: %+v", len(hits), hits)
	}
	for _, h := range hits {
		if h.Account == "" || h.InstanceID == "p1" {
			t.Fatalf("unexpected search hit: %+v", h)
		}
	}

	// ScanCache & CleanCache with acct filter only cleans target account's non-protected items.
	scan := store.ScanCache(instList, 1, nil)
	if scan.ByAccount["real@gmail.com"] == 0 || scan.ByAccount["alt@gmail.com"] == 0 {
		t.Fatalf("expected ByAccount byte totals in ScanCache, got %+v", scan.ByAccount)
	}
	clean := store.CleanCache(instList, 1, "acct:real@gmail.com", nil)
	if clean.DeletedCount != 1 {
		t.Fatalf("expected exactly 1 deleted item for acct:real@gmail.com, got %+v", clean)
	}
	if _, err := os.Stat(filepath.Join(p1GDir, "conversations", uuidP1Old+".db")); err != nil {
		t.Fatalf("expected p1 old cache DB untouched by acct:real clean, got err=%v", err)
	}
}

func makeGenDurationBlob(sec, nanos uint64) []byte {
	var b []byte
	if sec > 0 {
		b = append(b, encodeVarintField(1, sec)...)
	}
	if nanos > 0 {
		b = append(b, encodeVarintField(2, nanos)...)
	}
	return b
}

func makeGenMetadataBlob(botID string, modelName string, stepIdx int, inp, cache, out, think, resp, ttftSec, ttftNano, streamSec, streamNano uint64) []byte {
	var u4 []byte
	u4 = append(u4, encodeVarintField(1, 1525)...)
	u4 = append(u4, encodeVarintField(2, inp)...)
	u4 = append(u4, encodeVarintField(3, out)...)
	u4 = append(u4, encodeVarintField(5, cache)...)
	u4 = append(u4, encodeBytesField(7, []byte(botID))...)
	u4 = append(u4, encodeVarintField(9, think)...)
	u4 = append(u4, encodeVarintField(10, resp)...)

	var kv []byte
	kv = append(kv, encodeBytesField(1, []byte("last_step_index"))...)
	kv = append(kv, encodeBytesField(2, []byte(string(rune('0'+stepIdx))))...)

	var m1 []byte
	m1 = append(m1, encodeVarintField(3, 1525)...)
	m1 = append(m1, encodeBytesField(4, u4)...)
	m1 = append(m1, encodeBytesField(11, makeGenDurationBlob(ttftSec, ttftNano))...)
	m1 = append(m1, encodeBytesField(12, makeGenDurationBlob(streamSec, streamNano))...)
	m1 = append(m1, encodeBytesField(20, kv)...)
	m1 = append(m1, encodeBytesField(21, []byte(modelName))...)

	return encodeBytesField(1, m1)
}

func TestConversationDetailSpeedCompactionAndHeatmap(t *testing.T) {
	tmp := t.TempDir()
	gdir := filepath.Join(tmp, "gemini")
	cid := "66666666-6666-6666-6666-666666666666"
	dbPath := filepath.Join(gdir, "conversations", cid+".db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatal(err)
	}
	con, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer con.Close()
	if _, err := con.Exec(`
		CREATE TABLE steps (idx INTEGER PRIMARY KEY, step_type INTEGER, metadata BLOB, step_payload BLOB);
		CREATE TABLE gen_metadata (idx INTEGER PRIMARY KEY, data BLOB);
	`); err != nil {
		t.Fatal(err)
	}

	nowTS := time.Now().Unix()

	// Step 1: USER_INPUT linked before gen 0
	userPayload := encodeBytesField(19, encodeBytesField(2, []byte("<USER_REQUEST>Analyze auth bug</USER_REQUEST>")))
	stepMeta := func(ts int64, genIdx int, toolName, argsJSON string, outTok uint64) []byte {
		var md []byte
		md = append(md, encodeBytesField(1, encodeVarintField(1, uint64(ts)))...)
		var link []byte
		link = append(link, encodeVarintField(3, uint64(genIdx))...)
		md = append(md, encodeBytesField(20, link)...)
		if toolName != "" {
			var tc []byte
			tc = append(tc, encodeBytesField(2, []byte(toolName))...)
			tc = append(tc, encodeBytesField(3, []byte(argsJSON))...)
			md = append(md, encodeBytesField(4, tc)...)
			if outTok > 0 {
				md = append(md, encodeVarintField(23, outTok)...)
			}
		}
		return md
	}

	if _, err := con.Exec(`INSERT INTO steps(idx, step_type, metadata, step_payload) VALUES(0, 14, ?, ?)`,
		stepMeta(nowTS-120, 0, "", "", 0), userPayload); err != nil {
		t.Fatal(err)
	}
	agentPayload := encodeBytesField(20, encodeBytesField(1, []byte("Found the issue in server.go")))
	if _, err := con.Exec(`INSERT INTO steps(idx, step_type, metadata, step_payload) VALUES(1, 15, ?, ?)`,
		stepMeta(nowTS-110, 0, "", "", 0), agentPayload); err != nil {
		t.Fatal(err)
	}
	if _, err := con.Exec(`INSERT INTO steps(idx, step_type, metadata, step_payload) VALUES(2, 132, ?, NULL)`,
		stepMeta(nowTS-108, 0, "read_file", `{"Path":"/app/server.go"}`, 420)); err != nil {
		t.Fatal(err)
	}
	if _, err := con.Exec(`INSERT INTO steps(idx, step_type, metadata, step_payload) VALUES(3, 15, ?, NULL)`,
		stepMeta(nowTS-30, 1, "", "", 0)); err != nil {
		t.Fatal(err)
	}

	// Gen 0: 100k prompt -> Gen 1: 40k prompt (compaction drop >5k and <=75%)
	if _, err := con.Exec(`INSERT INTO gen_metadata(idx, data) VALUES(0, ?)`,
		makeGenMetadataBlob("bot-0", "Gemini 4 Hydrogen (High)", 1, 10000, 90000, 300, 120, 180, 2, 0, 3, 0)); err != nil {
		t.Fatal(err)
	}
	if _, err := con.Exec(`INSERT INTO gen_metadata(idx, data) VALUES(1, ?)`,
		makeGenMetadataBlob("bot-1", "Gemini 4 Hydrogen (High)", 3, 5000, 35000, 200, 50, 150, 1, 0, 1, 0)); err != nil {
		t.Fatal(err)
	}

	instList := []instances.Instance{
		{ID: "main", Label: "Antigravity", GeminiDirs: []string{gdir}},
	}
	store := NewStore(nil)
	detail := store.QueryConversationDetail(context.Background(), instList, "main", cid, "std")
	if len(detail.Turns) != 2 {
		t.Fatalf("expected 2 turns, got %d", len(detail.Turns))
	}
	if detail.Turns[0].TTFTMs != 2000 || detail.Turns[0].StreamMs != 3000 || detail.Turns[0].TPS != 60.0 {
		t.Fatalf("unexpected turn 0 timing/tps: %+v", detail.Turns[0])
	}
	if detail.Turns[0].UserSnippet != "Analyze auth bug" || detail.Turns[0].AgentSnippet != "Found the issue in server.go" {
		t.Fatalf("unexpected snippets: %+v", detail.Turns[0])
	}
	if len(detail.Turns[0].Tools) != 1 || detail.Turns[0].Tools[0].Name != "read_file" || detail.Turns[0].Tools[0].Tokens != 420 {
		t.Fatalf("unexpected tools on turn 0: %+v", detail.Turns[0].Tools)
	}
	if !detail.Turns[1].Compacted || detail.CompactedCount != 1 {
		t.Fatalf("expected compaction detected on turn 1, got compacted=%v count=%d", detail.Turns[1].Compacted, detail.CompactedCount)
	}
	if detail.PeakTPS != 100.0 || detail.LastTPS != 100.0 {
		t.Fatalf("expected peak/last TPS 100.0, got peak=%.1f last=%.1f", detail.PeakTPS, detail.LastTPS)
	}

	speedMap := store.SummarizeLatestByCascade(instList, "main")
	if speedMap[cid].LastTPS != 100.0 || speedMap[cid].CompactedTurns != 1 {
		t.Fatalf("unexpected speedMap entry: %+v", speedMap[cid])
	}

	hm := store.BuildHeatmap(instList, "main", "std", 12)
	if len(hm) != 84 {
		t.Fatalf("expected 84 heatmap days for 12 weeks, got %d", len(hm))
	}
	lastDay := hm[len(hm)-1]
	if lastDay.Calls != 2 || lastDay.Level != 4 {
		t.Fatalf("expected today's heatmap day to have 2 calls & level 4, got %+v", lastDay)
	}
}
