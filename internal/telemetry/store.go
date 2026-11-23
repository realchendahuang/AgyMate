package telemetry

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"github.com/realchendahuang/AgyMate/internal/instances"
)

const imageGenUnitCost = 0.04

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type fileSig struct {
	dbMtime  int64
	dbSize   int64
	walMtime int64
	walSize  int64
}

type cachedDB struct {
	sig     fileSig
	records []CallRecord
	images  []ImageEvent
}

type summaryMeta struct {
	InstanceID string
	CascadeID  string
	Title      string
	ParentID   string
	Workspace  string
	Status     string
	StepCount  int
	ModifiedAt string
}

// Store maintains an incremental read-only SQLite WAL cache across all instances.
type Store struct {
	mu          sync.Mutex
	dbCache     map[string]cachedDB
	summarySigs map[string]fileSig
	summaryMeta map[string]summaryMeta // key: "<instID>:<cid>"
	labelFn     func(string) string
}

// NewStore constructs a read-only SQLite telemetry store.
func NewStore(labelFn func(string) string) *Store {
	return &Store{
		dbCache:     make(map[string]cachedDB),
		summarySigs: make(map[string]fileSig),
		summaryMeta: make(map[string]summaryMeta),
		labelFn:     labelFn,
	}
}

func statSig(dbPath string) fileSig {
	var s fileSig
	if info, err := os.Stat(dbPath); err == nil {
		s.dbMtime = info.ModTime().UnixNano()
		s.dbSize = info.Size()
	}
	if info, err := os.Stat(dbPath + "-wal"); err == nil {
		s.walMtime = info.ModTime().UnixNano()
		s.walSize = info.Size()
	}
	return s
}

func openReadOnlySQLite(dbPath string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?mode=ro&_pragma=query_only(1)&_pragma=busy_timeout(1500)", dbPath)
	return sql.Open("sqlite", dsn)
}

func (s *Store) refreshSummaries(instList []instances.Instance) {
	for _, inst := range instList {
		for _, gdir := range inst.GeminiDirs {
			sumDB := filepath.Join(gdir, "conversation_summaries.db")
			if _, err := os.Stat(sumDB); err != nil {
				continue
			}
			sig := statSig(sumDB)
			if prev, ok := s.summarySigs[sumDB]; ok && prev == sig {
				continue
			}
			con, err := openReadOnlySQLite(sumDB)
			if err != nil {
				continue
			}
			rows, err := con.Query(`SELECT conversation_id, title, parent_conversation_id, workspace_uris, status, step_count, last_modified_time FROM conversation_summaries`)
			if err == nil {
				for rows.Next() {
					var cid, title, pid, ws, status, mod string
					var steps int
					if rows.Scan(&cid, &title, &pid, &ws, &status, &steps, &mod) == nil {
						s.summaryMeta[inst.ID+":"+cid] = summaryMeta{
							InstanceID: inst.ID,
							CascadeID:  cid,
							Title:      title,
							ParentID:   pid,
							Workspace:  cleanWorkspaceURI(ws),
							Status:     status,
							StepCount:  steps,
							ModifiedAt: mod,
						}
					}
				}
				rows.Close()
				s.summarySigs[sumDB] = sig
			}
			con.Close()
		}
	}
}

func cleanWorkspaceURI(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" {
		return ""
	}
	if strings.HasPrefix(raw, "[") {
		var arr []string
		if json.Unmarshal([]byte(raw), &arr) == nil && len(arr) > 0 {
			raw = arr[0]
		}
	}
	raw = strings.TrimPrefix(raw, "file://")
	return raw
}

func (s *Store) parseSingleDB(instID, dbPath string) ([]CallRecord, []ImageEvent) {
	base := filepath.Base(dbPath)
	cid := strings.TrimSuffix(base, ".db")

	con, err := openReadOnlySQLite(dbPath)
	if err != nil {
		return nil, nil
	}
	defer con.Close()

	type stepEntry struct {
		ts    int64
		sidx  int
		stype int
		u9    protoDict
	}
	stepMap := make(map[string]stepEntry)
	stepIdxTS := make(map[int]int64)
	var imgEvents []ImageEvent

	rows, err := con.Query(`SELECT idx, step_type, metadata FROM steps WHERE metadata IS NOT NULL`)
	if err == nil {
		for rows.Next() {
			var idx, stype int
			var meta []byte
			if rows.Scan(&idx, &stype, &meta) != nil {
				continue
			}
			d := parseProtoDict(meta)
			var ts int64
			if b := d.firstBytes(1); len(b) > 0 {
				ts = int64(parseProtoDict(b).firstVarint(1))
			}
			stepIdxTS[idx] = ts
			if b4 := d.firstBytes(4); len(b4) > 0 {
				t4 := parseProtoDict(b4)
				if t4.firstString(2) == "generate_image" {
					imgEvents = append(imgEvents, ImageEvent{
						Timestamp:  ts,
						InstanceID: instID,
						CascadeID:  cid,
					})
				}
			}
			if b9 := d.firstBytes(9); len(b9) > 0 {
				u9 := parseProtoDict(b9)
				botID := u9.firstString(7)
				if botID == "" {
					botID = fmt.Sprintf("step_%d", idx)
				}
				stepMap[botID] = stepEntry{ts: ts, sidx: idx, stype: stype, u9: u9}
			}
		}
		rows.Close()
	}

	var records []CallRecord
	seenBots := make(map[string]bool)

	grows, err := con.Query(`SELECT idx, data FROM gen_metadata`)
	if err == nil {
		for grows.Next() {
			var gidx int
			var blob []byte
			if grows.Scan(&gidx, &blob) != nil {
				continue
			}
			d := parseProtoDict(blob)
			b1 := d.firstBytes(1)
			if len(b1) == 0 {
				continue
			}
			m1 := parseProtoDict(b1)
			var u4 protoDict
			if b4 := m1.firstBytes(4); len(b4) > 0 {
				u4 = parseProtoDict(b4)
			} else {
				u4 = make(protoDict)
			}
			botID := u4.firstString(7)
			if botID == "" {
				botID = fmt.Sprintf("gen_%d", gidx)
			}
			seenBots[botID] = true

			me := m1.firstVarint(3)
			if me == 0 {
				me = u4.firstVarint(1)
			}
			mid := m1.firstString(19)
			mn := m1.firstString(21)

			kv := make(map[string]string)
			for _, f := range m1[20] {
				kvd := parseProtoDict(f.bytes)
				k := kvd.firstString(1)
				v := kvd.firstString(2)
				if k != "" {
					kv[k] = v
				}
			}

			var ts int64
			if se, ok := stepMap[botID]; ok {
				ts = se.ts
			} else if lsiStr, ok := kv["last_step_index"]; ok {
				if lsi, err := strconv.Atoi(lsiStr); err == nil {
					if t, ok := stepIdxTS[lsi]; ok {
						ts = t
					} else if t, ok := stepIdxTS[lsi+1]; ok {
						ts = t
					}
				}
			}

			inp := int64(u4.firstVarint(2))
			out := int64(u4.firstVarint(3))
			cache := int64(u4.firstVarint(5))
			think := int64(u4.firstVarint(9))
			resp := int64(u4.firstVarint(10))
			if inp == 0 && cache == 0 && out == 0 {
				continue
			}

			ttftMs := parseProtoDurationMs(m1.firstBytes(11))
			streamMs := parseProtoDurationMs(m1.firstBytes(12))

			records = append(records, CallRecord{
				GenIndex:    gidx,
				Timestamp:   ts,
				InstanceID:  instID,
				CascadeID:   cid,
				ModelEnum:   me,
				ModelID:     mid,
				ModelName:   resolveModelName(me, mid, mn, s.labelFn),
				Input:       inp,
				CacheRead:   cache,
				Output:      out,
				Thinking:    think,
				ResponseOut: resp,
				TTFTMs:      ttftMs,
				StreamMs:    streamMs,
			})
		}
		grows.Close()
	}

	for botID, se := range stepMap {
		if seenBots[botID] {
			continue
		}
		me := se.u9.firstVarint(1)
		inp := int64(se.u9.firstVarint(2))
		out := int64(se.u9.firstVarint(3))
		cache := int64(se.u9.firstVarint(5))
		think := int64(se.u9.firstVarint(9))
		resp := int64(se.u9.firstVarint(10))
		if inp == 0 && cache == 0 && out == 0 {
			continue
		}
		records = append(records, CallRecord{
			Timestamp:   se.ts,
			InstanceID:  instID,
			CascadeID:   cid,
			ModelEnum:   me,
			ModelName:   resolveModelName(me, "", "", s.labelFn),
			Input:       inp,
			CacheRead:   cache,
			Output:      out,
			Thinking:    think,
			ResponseOut: resp,
		})
	}

	return records, imgEvents
}

func (s *Store) collect(instList []instances.Instance) ([]CallRecord, []ImageEvent, map[string]summaryMeta, float64) {
	t0 := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()

	s.refreshSummaries(instList)

	type dbTarget struct {
		instID string
		path   string
	}
	var targets []dbTarget
	seenPaths := make(map[string]bool)
	seenCIDs := make(map[string]bool)

	for _, inst := range instList {
		for _, gdir := range inst.GeminiDirs {
			matches, err := filepath.Glob(filepath.Join(gdir, "conversations", "*.db"))
			if err != nil {
				continue
			}
			for _, p := range matches {
				cid := strings.TrimSuffix(filepath.Base(p), ".db")
				dedupKey := inst.ID + ":" + cid
				if seenPaths[p] || seenCIDs[dedupKey] {
					continue
				}
				seenPaths[p] = true
				seenCIDs[dedupKey] = true
				targets = append(targets, dbTarget{instID: inst.ID, path: p})
			}
		}
	}

	for stale := range s.dbCache {
		if !seenPaths[stale] {
			delete(s.dbCache, stale)
		}
	}

	var allRecords []CallRecord
	var allImages []ImageEvent
	for _, tgt := range targets {
		sig := statSig(tgt.path)
		if c, ok := s.dbCache[tgt.path]; ok && c.sig == sig {
			allRecords = append(allRecords, c.records...)
			allImages = append(allImages, c.images...)
			continue
		}
		recs, imgs := s.parseSingleDB(tgt.instID, tgt.path)
		s.dbCache[tgt.path] = cachedDB{sig: sig, records: recs, images: imgs}
		allRecords = append(allRecords, recs...)
		allImages = append(allImages, imgs...)
	}

	metaCopy := make(map[string]summaryMeta, len(s.summaryMeta))
	for k, v := range s.summaryMeta {
		metaCopy[k] = v
	}
	elapsedMs := float64(time.Since(t0).Microseconds()) / 1000.0
	return allRecords, allImages, metaCopy, elapsedMs
}

// TelemetryReport is the JSON/CLI payload returned by QueryTelemetry.
type TelemetryReport struct {
	Preset      string             `json:"preset"`
	PresetLabel string             `json:"presetLabel"`
	Mode        string             `json:"mode"`
	Instance    string             `json:"instance"`
	Granularity string             `json:"granularity"`
	ScanMs      float64            `json:"scanMs"`
	Summary     TelemetrySummary   `json:"summary"`
	BurnRate15m BurnRate           `json:"burnRate15m"`
	ByModel     []ModelAggregate   `json:"byModel"`
	ByInstance  []InstAggregate    `json:"byInstance"`
	ByAccount   []AccountAggregate `json:"byAccount"`
	Trend       []TrendBucket      `json:"trend"`
	RecentCalls []RecentCall       `json:"recentCalls"`
	MonthROI    MonthlyROI         `json:"monthRoi"`
}

// TelemetrySummary totals for the selected time window.
type TelemetrySummary struct {
	Calls           int            `json:"calls"`
	InputTokens     int64          `json:"inputTokens"`
	CacheTokens     int64          `json:"cacheTokens"`
	OutputTokens    int64          `json:"outputTokens"`
	TotalTokens     int64          `json:"totalTokens"`
	CacheHitRate    float64        `json:"cacheHitRate"`
	TotalUSD        float64        `json:"totalUsd"`
	SavedByCacheUSD float64        `json:"savedByCacheUsd"`
	ImageCalls      int            `json:"imageCalls"`
	ImageUSD        float64        `json:"imageUsd"`
	CostBreakdown   CostComponents `json:"costBreakdown"`
}

// BurnRate estimates live consumption speed over the past 15 minutes.
type BurnRate struct {
	TokensPerMin float64 `json:"tokensPerMin"`
	USDPerHour   float64 `json:"usdPerHour"`
	Calls15m     int     `json:"calls15m"`
}

// MonthlyROI tracks monthly API value equivalent against an optional subscription baseline.
type MonthlyROI struct {
	MainMonthUSD float64 `json:"mainMonthUsd"`
	AllMonthUSD  float64 `json:"allMonthUsd"`
	SubUSD       float64 `json:"subUsd"`
	ROIMultiple  float64 `json:"roiMultiple"`
}

// ModelAggregate aggregates token & cost per model.
type ModelAggregate struct {
	ModelName string  `json:"modelName"`
	Calls     int     `json:"calls"`
	Input     int64   `json:"inputTokens"`
	Cache     int64   `json:"cacheTokens"`
	Output    int64   `json:"outputTokens"`
	Total     int64   `json:"totalTokens"`
	TotalUSD  float64 `json:"totalUsd"`
	SharePct  float64 `json:"sharePct"`
}

// InstAggregate aggregates token & cost per instance.
type InstAggregate struct {
	InstanceID string  `json:"instanceId"`
	Account    string  `json:"account,omitempty"`
	Calls      int     `json:"calls"`
	Input      int64   `json:"inputTokens"`
	Cache      int64   `json:"cacheTokens"`
	Total      int64   `json:"totalTokens"`
	CacheRate  float64 `json:"cacheRate"`
	TotalUSD   float64 `json:"totalUsd"`
}

// AccountAggregate aggregates token & cost per Google account across its client slots.
type AccountAggregate struct {
	Account     string   `json:"account"`
	InstanceIDs []string `json:"instanceIds,omitempty"`
	Calls       int      `json:"calls"`
	Input       int64    `json:"inputTokens"`
	Cache       int64    `json:"cacheTokens"`
	Output      int64    `json:"outputTokens"`
	Total       int64    `json:"totalTokens"`
	CacheRate   float64  `json:"cacheRate"`
	TotalUSD    float64  `json:"totalUsd"`
}

// TrendBucket represents one chart bar in the selected time preset.
type TrendBucket struct {
	Key      string  `json:"key"`
	Label    string  `json:"label"`
	Input    int64   `json:"inputTokens"`
	Cache    int64   `json:"cacheTokens"`
	Output   int64   `json:"outputTokens"`
	TotalUSD float64 `json:"totalUsd"`
	Calls    int     `json:"calls"`
}

// RecentCall describes one recent LLM call for the live feed table.
type RecentCall struct {
	Timestamp  int64   `json:"timestamp"`
	TimeStr    string  `json:"timeStr"`
	InstanceID string  `json:"instanceId"`
	Account    string  `json:"account,omitempty"`
	CascadeID  string  `json:"cascadeId"`
	Title      string  `json:"title"`
	ModelName  string  `json:"modelName"`
	Input      int64   `json:"inputTokens"`
	Cache      int64   `json:"cacheTokens"`
	Output     int64   `json:"outputTokens"`
	Thinking   int64   `json:"thinkingTokens,omitempty"`
	Response   int64   `json:"responseTokens,omitempty"`
	TTFTMs     int64   `json:"ttftMs,omitempty"`
	StreamMs   int64   `json:"streamMs,omitempty"`
	TotalMs    int64   `json:"totalMs,omitempty"`
	TPS        float64 `json:"tps,omitempty"`
	PrefillTPS float64 `json:"prefillTps,omitempty"`
	USD        float64 `json:"usd"`
}

func buildAccountLookup(instList []instances.Instance) (map[string]string, map[string]string) {
	acctByInst := make(map[string]string, len(instList))
	labelByInst := make(map[string]string, len(instList))
	for _, inst := range instList {
		acctByInst[inst.ID] = strings.TrimSpace(inst.Account)
		labelByInst[inst.ID] = strings.TrimSpace(inst.Label)
	}
	return acctByInst, labelByInst
}

func matchesFilter(instID, account, filter string) bool {
	filter = strings.TrimSpace(filter)
	if filter == "" || filter == "all" {
		return true
	}
	if strings.HasPrefix(filter, "acct:") {
		target := strings.TrimSpace(strings.TrimPrefix(filter, "acct:"))
		if target == "" {
			return true
		}
		return account != "" && strings.EqualFold(strings.TrimSpace(account), target)
	}
	return instID == filter
}

// QueryTelemetry computes aggregated token telemetry and API valuation.
func (s *Store) QueryTelemetry(instList []instances.Instance, preset, mode, instanceFilter string, ultraSubUSD float64) TelemetryReport {
	if preset == "" {
		preset = "today"
	}
	if mode == "" {
		mode = "std"
	}
	if ultraSubUSD < 0 {
		ultraSubUSD = 0
	}

	records, imgs, metas, scanMs := s.collect(instList)
	now := time.Now()
	startTS, endTS, presetLabel, gran := resolveBounds(preset, now)

	// Month ROI calculation across current calendar month
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).Unix()
	var mainMonthUSD, allMonthUSD float64
	// 15m burn rate
	cut15m := now.Unix() - 900
	var tokens15m int64
	var usd15m float64
	var calls15m int

	type modelBucket struct {
		calls int
		in    int64
		cache int64
		out   int64
		usd   float64
	}
	modelMap := make(map[string]*modelBucket)
	instMap := make(map[string]*InstAggregate)
	acctMap := make(map[string]*AccountAggregate)
	acctInstSet := make(map[string]map[string]bool)
	trendMap := make(map[string]*TrendBucket)
	acctByInst, labelByInst := buildAccountLookup(instList)

	var summary TelemetrySummary
	var recent []RecentCall

	for _, r := range records {
		acct := acctByInst[r.InstanceID]
		if r.Timestamp >= monthStart && r.Timestamp <= now.Unix() {
			mc := ComputeCallCost(r.ModelName, r.ModelID, r.Input, r.CacheRead, r.Output, r.Thinking, r.ResponseOut, mode)
			allMonthUSD += mc.TotalUSD
			if r.InstanceID == "main" {
				mainMonthUSD += mc.TotalUSD
			}
		}

		if !matchesFilter(r.InstanceID, acct, instanceFilter) {
			continue
		}

		if r.Timestamp >= cut15m && r.Timestamp <= now.Unix() {
			cc15 := ComputeCallCost(r.ModelName, r.ModelID, r.Input, r.CacheRead, r.Output, r.Thinking, r.ResponseOut, mode)
			tokens15m += r.Input + r.CacheRead + r.Output
			usd15m += cc15.TotalUSD
			calls15m++
		}

		if r.Timestamp < startTS || r.Timestamp > endTS {
			continue
		}

		cc := ComputeCallCost(r.ModelName, r.ModelID, r.Input, r.CacheRead, r.Output, r.Thinking, r.ResponseOut, mode)
		ncMode := "nc_std"
		if mode == "promo" || mode == "nc_promo" {
			ncMode = "nc_promo"
		}
		ccNoCache := ComputeCallCost(r.ModelName, r.ModelID, r.Input, r.CacheRead, r.Output, r.Thinking, r.ResponseOut, ncMode)

		summary.Calls++
		summary.InputTokens += r.Input
		summary.CacheTokens += r.CacheRead
		summary.OutputTokens += r.Output
		summary.TotalTokens += r.Input + r.CacheRead + r.Output
		summary.TotalUSD += cc.TotalUSD
		summary.SavedByCacheUSD += (ccNoCache.TotalUSD - cc.TotalUSD)
		summary.CostBreakdown.InputUSD += cc.InputUSD
		summary.CostBreakdown.CacheUSD += cc.CacheUSD
		summary.CostBreakdown.OutputUSD += cc.OutputUSD
		summary.CostBreakdown.ThinkingUSD += cc.ThinkingUSD
		summary.CostBreakdown.TotalUSD += cc.TotalUSD

		mb := modelMap[r.ModelName]
		if mb == nil {
			mb = &modelBucket{}
			modelMap[r.ModelName] = mb
		}
		mb.calls++
		mb.in += r.Input
		mb.cache += r.CacheRead
		mb.out += r.Output
		mb.usd += cc.TotalUSD

		ia := instMap[r.InstanceID]
		if ia == nil {
			ia = &InstAggregate{
				InstanceID: r.InstanceID,
				Account:    acct,
			}
			instMap[r.InstanceID] = ia
		}
		ia.Calls++
		ia.Input += r.Input
		ia.Cache += r.CacheRead
		ia.Total += r.Input + r.CacheRead + r.Output
		ia.TotalUSD += cc.TotalUSD

		acctDisplay := acct
		if acctDisplay == "" {
			if lbl := labelByInst[r.InstanceID]; lbl != "" {
				acctDisplay = lbl
			} else {
				acctDisplay = r.InstanceID
			}
		}
		acctKey := strings.ToLower(acct)
		if acctKey == "" {
			acctKey = "slot:" + r.InstanceID
		}
		aa := acctMap[acctKey]
		if aa == nil {
			aa = &AccountAggregate{Account: acctDisplay}
			acctMap[acctKey] = aa
			acctInstSet[acctKey] = make(map[string]bool)
		}
		aa.Calls++
		aa.Input += r.Input
		aa.Cache += r.CacheRead
		aa.Output += r.Output
		aa.Total += r.Input + r.CacheRead + r.Output
		aa.TotalUSD += cc.TotalUSD
		acctInstSet[acctKey][r.InstanceID] = true

		bKey, bLabel := bucketForTimestamp(r.Timestamp, gran, now.Location())
		tb := trendMap[bKey]
		if tb == nil {
			tb = &TrendBucket{Key: bKey, Label: bLabel}
			trendMap[bKey] = tb
		}
		tb.Input += r.Input
		tb.Cache += r.CacheRead
		tb.Output += r.Output
		tb.TotalUSD += cc.TotalUSD
		tb.Calls++

		title := metas[r.InstanceID+":"+r.CascadeID].Title
		if title == "" {
			title = r.CascadeID
			if len(title) > 8 {
				title = title[:8]
			}
		}
		sm := computeSpeedMetrics(r.Input, r.CacheRead, r.Output, r.ResponseOut, r.TTFTMs, r.StreamMs)
		recent = append(recent, RecentCall{
			Timestamp:  r.Timestamp,
			TimeStr:    time.Unix(r.Timestamp, 0).Format("01-02 15:04:05"),
			InstanceID: r.InstanceID,
			Account:    acct,
			CascadeID:  r.CascadeID,
			Title:      title,
			ModelName:  r.ModelName,
			Input:      r.Input,
			Cache:      r.CacheRead,
			Output:     r.Output,
			Thinking:   r.Thinking,
			Response:   r.ResponseOut,
			TTFTMs:     r.TTFTMs,
			StreamMs:   r.StreamMs,
			TotalMs:    sm.TotalMs,
			TPS:        sm.TPS,
			PrefillTPS: sm.PrefillTPS,
			USD:        cc.TotalUSD,
		})
	}

	for _, img := range imgs {
		if !matchesFilter(img.InstanceID, acctByInst[img.InstanceID], instanceFilter) {
			continue
		}
		if img.Timestamp >= monthStart && img.Timestamp <= now.Unix() {
			allMonthUSD += imageGenUnitCost
			if img.InstanceID == "main" {
				mainMonthUSD += imageGenUnitCost
			}
		}
		if img.Timestamp >= startTS && img.Timestamp <= endTS {
			summary.ImageCalls++
			summary.ImageUSD += imageGenUnitCost
			summary.TotalUSD += imageGenUnitCost
		}
	}

	if promptDenom := summary.InputTokens + summary.CacheTokens; promptDenom > 0 {
		summary.CacheHitRate = float64(summary.CacheTokens) * 100.0 / float64(promptDenom)
	}
	if summary.SavedByCacheUSD < 0 {
		summary.SavedByCacheUSD = 0
	}

	byModel := make([]ModelAggregate, 0, len(modelMap))
	for name, mb := range modelMap {
		tot := mb.in + mb.cache + mb.out
		var share float64
		if summary.TotalUSD > 0 {
			share = mb.usd * 100.0 / summary.TotalUSD
		}
		byModel = append(byModel, ModelAggregate{
			ModelName: name,
			Calls:     mb.calls,
			Input:     mb.in,
			Cache:     mb.cache,
			Output:    mb.out,
			Total:     tot,
			TotalUSD:  mb.usd,
			SharePct:  share,
		})
	}
	sort.Slice(byModel, func(i, j int) bool { return byModel[i].TotalUSD > byModel[j].TotalUSD })

	byInst := make([]InstAggregate, 0, len(instMap))
	for _, ia := range instMap {
		if den := ia.Input + ia.Cache; den > 0 {
			ia.CacheRate = float64(ia.Cache) * 100.0 / float64(den)
		}
		byInst = append(byInst, *ia)
	}
	sort.Slice(byInst, func(i, j int) bool { return byInst[i].TotalUSD > byInst[j].TotalUSD })

	byAcct := make([]AccountAggregate, 0, len(acctMap))
	for key, aa := range acctMap {
		if den := aa.Input + aa.Cache; den > 0 {
			aa.CacheRate = float64(aa.Cache) * 100.0 / float64(den)
		}
		for instID := range acctInstSet[key] {
			aa.InstanceIDs = append(aa.InstanceIDs, instID)
		}
		sort.Strings(aa.InstanceIDs)
		byAcct = append(byAcct, *aa)
	}
	sort.Slice(byAcct, func(i, j int) bool { return byAcct[i].TotalUSD > byAcct[j].TotalUSD })

	trend := make([]TrendBucket, 0, len(trendMap))
	for _, tb := range trendMap {
		trend = append(trend, *tb)
	}
	sort.Slice(trend, func(i, j int) bool { return trend[i].Key < trend[j].Key })

	sort.Slice(recent, func(i, j int) bool { return recent[i].Timestamp > recent[j].Timestamp })
	if len(recent) > 30 {
		recent = recent[:30]
	}

	return TelemetryReport{
		Preset:      preset,
		PresetLabel: presetLabel,
		Mode:        mode,
		Instance:    instanceFilter,
		Granularity: gran,
		ScanMs:      scanMs,
		Summary:     summary,
		BurnRate15m: BurnRate{
			TokensPerMin: float64(tokens15m) / 15.0,
			USDPerHour:   usd15m * 4.0,
			Calls15m:     calls15m,
		},
		ByModel:     byModel,
		ByInstance:  byInst,
		ByAccount:   byAcct,
		Trend:       trend,
		RecentCalls: recent,
		MonthROI: func() MonthlyROI {
			roiMult := 0.0
			if ultraSubUSD > 0 {
				roiMult = allMonthUSD / ultraSubUSD
			}
			return MonthlyROI{
				MainMonthUSD: mainMonthUSD,
				AllMonthUSD:  allMonthUSD,
				SubUSD:       ultraSubUSD,
				ROIMultiple:  roiMult,
			}
		}(),
	}
}

// ExportCSV renders the telemetry model breakdown & recent calls as a UTF-8 BOM CSV.
func (s *Store) ExportCSV(rep TelemetryReport) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{0xEF, 0xBB, 0xBF})
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"Type", "Name/Time", "Instance", "Calls", "InputTokens", "CacheTokens", "OutputTokens", "TotalUSD"})
	for _, m := range rep.ByModel {
		_ = w.Write([]string{
			"Model",
			m.ModelName,
			rep.Instance,
			strconv.Itoa(m.Calls),
			strconv.FormatInt(m.Input, 10),
			strconv.FormatInt(m.Cache, 10),
			strconv.FormatInt(m.Output, 10),
			fmt.Sprintf("%.4f", m.TotalUSD),
		})
	}
	for _, rc := range rep.RecentCalls {
		_ = w.Write([]string{
			"Call",
			rc.TimeStr + " " + rc.Title,
			rc.InstanceID,
			"1",
			strconv.FormatInt(rc.Input, 10),
			strconv.FormatInt(rc.Cache, 10),
			strconv.FormatInt(rc.Output, 10),
			fmt.Sprintf("%.4f", rc.USD),
		})
	}
	w.Flush()
	return buf.Bytes()
}

func resolveBounds(preset string, now time.Time) (startTS, endTS int64, label, gran string) {
	loc := now.Location()
	endTS = now.Unix()
	switch preset {
	case "last_1h":
		return endTS - 3600, endTS, "最近 1 小时", "5m"
	case "last_5h":
		return endTS - 5*3600, endTS, "最近 5 小时", "15m"
	case "last_24h":
		return endTS - 86400, endTS, "最近 24 小时", "1h"
	case "since_monday":
		wd := (int(now.Weekday()) + 6) % 7
		mon := time.Date(now.Year(), now.Month(), now.Day()-wd, 0, 0, 0, 0, loc)
		return mon.Unix(), endTS, fmt.Sprintf("本周 (%s 起)", mon.Format("01-02")), "1d"
	case "last_7d":
		return endTS - 7*86400, endTS, "最近 7 天", "1d"
	case "last_30d":
		return endTS - 30*86400, endTS, "最近 30 天", "1d"
	case "yesterday":
		day := time.Date(now.Year(), now.Month(), now.Day()-1, 0, 0, 0, 0, loc)
		dayEnd := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		return day.Unix(), dayEnd.Unix() - 1, fmt.Sprintf("昨日 (%s)", day.Format("01-02")), "1h"
	case "this_month":
		mStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
		return mStart.Unix(), endTS, fmt.Sprintf("本月 (%s 起)", mStart.Format("01-02")), "1d"
	case "all", "all_time":
		return 0, endTS, "全部历史", "1d"
	default: // "today"
		day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		return day.Unix(), endTS, fmt.Sprintf("今日 (%s)", day.Format("01-02")), "1h"
	}
}

func bucketForTimestamp(ts int64, gran string, loc *time.Location) (string, string) {
	t := time.Unix(ts, 0).In(loc)
	switch gran {
	case "5m":
		m := (t.Minute() / 5) * 5
		b := time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), m, 0, 0, loc)
		return b.Format("2006-01-02 15:04"), b.Format("15:04")
	case "15m":
		m := (t.Minute() / 15) * 15
		b := time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), m, 0, 0, loc)
		return b.Format("2006-01-02 15:04"), b.Format("15:04")
	case "1h":
		return t.Format("2006-01-02 15"), t.Format("15:00")
	default:
		return t.Format("2006-01-02"), t.Format("01-02")
	}
}

// SearchHit represents one matched conversation across instances.
type SearchHit struct {
	InstanceID string `json:"instanceId"`
	Account    string `json:"account,omitempty"`
	CascadeID  string `json:"cascadeId"`
	Title      string `json:"title"`
	Workspace  string `json:"workspace"`
	Status     string `json:"status"`
	StepCount  int    `json:"stepCount"`
	ModifiedAt string `json:"modifiedAt"`
	Snippet    string `json:"snippet,omitempty"`
}

// SearchConversations queries FTS5 (and falls back to LIKE) across all instances'
// conversation_summaries.db in strictly read-only mode.
func (s *Store) SearchConversations(ctx context.Context, instList []instances.Instance, query, instFilter string, limit int) []SearchHit {
	if limit <= 0 {
		limit = 50
	}
	query = strings.TrimSpace(query)
	var hits []SearchHit
	seen := make(map[string]bool)

	for _, inst := range instList {
		acct := strings.TrimSpace(inst.Account)
		if !matchesFilter(inst.ID, acct, instFilter) {
			continue
		}
		for _, gdir := range inst.GeminiDirs {
			sumDB := filepath.Join(gdir, "conversation_summaries.db")
			if _, err := os.Stat(sumDB); err != nil {
				continue
			}
			con, err := openReadOnlySQLite(sumDB)
			if err != nil {
				continue
			}

			if query == "" {
				rows, err := con.QueryContext(ctx,
					`SELECT conversation_id, title, workspace_uris, status, step_count, last_modified_time
					 FROM conversation_summaries
					 WHERE parent_conversation_id = ''
					 ORDER BY last_modified_time DESC LIMIT ?`, limit)
				if err == nil {
					for rows.Next() {
						var h SearchHit
						var ws string
						if rows.Scan(&h.CascadeID, &h.Title, &ws, &h.Status, &h.StepCount, &h.ModifiedAt) == nil {
							key := inst.ID + ":" + h.CascadeID
							if seen[key] {
								continue
							}
							seen[key] = true
							h.InstanceID = inst.ID
							h.Account = acct
							h.Workspace = cleanWorkspaceURI(ws)
							hits = append(hits, h)
						}
					}
					rows.Close()
				}
				con.Close()
				continue
			}

			ftsQuery := sanitizeFTS5Query(query)
			snipMap := make(map[string]string)
			var matchedIDs []string
			if ftsQuery != "" {
				rows, err := con.QueryContext(ctx,
					`SELECT conversation_id, snippet(conversations_fts, 13, '', '', '...', 18)
					 FROM conversations_fts
					 WHERE conversations_fts MATCH ?
					 ORDER BY last_modified_time DESC LIMIT ?`, ftsQuery, limit*2)
				if err == nil {
					for rows.Next() {
						var cid, snip string
						if rows.Scan(&cid, &snip) == nil {
							if _, exists := snipMap[cid]; !exists {
								snipMap[cid] = strings.TrimSpace(snip)
								matchedIDs = append(matchedIDs, cid)
							}
						}
					}
					rows.Close()
				}
			}

			// Fallback LIKE search on title / workspace_uris
			likePattern := "%" + query + "%"
			lrows, err := con.QueryContext(ctx,
				`SELECT conversation_id, title, workspace_uris, status, step_count, last_modified_time
				 FROM conversation_summaries
				 WHERE title LIKE ? OR workspace_uris LIKE ?
				 ORDER BY last_modified_time DESC LIMIT ?`, likePattern, likePattern, limit)
			if err == nil {
				for lrows.Next() {
					var h SearchHit
					var ws string
					if lrows.Scan(&h.CascadeID, &h.Title, &ws, &h.Status, &h.StepCount, &h.ModifiedAt) == nil {
						key := inst.ID + ":" + h.CascadeID
						if seen[key] {
							continue
						}
						seen[key] = true
						h.InstanceID = inst.ID
						h.Account = acct
						h.Workspace = cleanWorkspaceURI(ws)
						h.Snippet = snipMap[h.CascadeID]
						hits = append(hits, h)
					}
				}
				lrows.Close()
			}

			for _, cid := range matchedIDs {
				key := inst.ID + ":" + cid
				if seen[key] {
					continue
				}
				var h SearchHit
				var ws, pid string
				err := con.QueryRowContext(ctx,
					`SELECT conversation_id, title, workspace_uris, status, step_count, last_modified_time, parent_conversation_id
					 FROM conversation_summaries WHERE conversation_id = ?`, cid).
					Scan(&h.CascadeID, &h.Title, &ws, &h.Status, &h.StepCount, &h.ModifiedAt, &pid)
				if err == nil && pid == "" {
					seen[key] = true
					h.InstanceID = inst.ID
					h.Account = acct
					h.Workspace = cleanWorkspaceURI(ws)
					h.Snippet = snipMap[cid]
					hits = append(hits, h)
				}
			}
			con.Close()
		}
	}

	sort.Slice(hits, func(i, j int) bool {
		return hits[i].ModifiedAt > hits[j].ModifiedAt
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

func sanitizeFTS5Query(q string) string {
	terms := strings.Fields(q)
	if len(terms) == 0 {
		return ""
	}
	quoted := make([]string, 0, len(terms))
	for _, t := range terms {
		t = strings.ReplaceAll(t, `"`, ``)
		if t != "" {
			quoted = append(quoted, `"`+t+`"`)
		}
	}
	return strings.Join(quoted, " ")
}

// CheckStepWaitingApproval inspects the newest step row of a conversation DB (read-only)
// to tell whether the agent is paused waiting for user permission / question answer.
func CheckStepWaitingApproval(geminiDirs []string, cascadeID string) bool {
	if !uuidRe.MatchString(cascadeID) {
		return false
	}
	for _, gdir := range geminiDirs {
		dbPath := filepath.Join(gdir, "conversations", cascadeID+".db")
		if _, err := os.Stat(dbPath); err != nil {
			continue
		}
		con, err := openReadOnlySQLite(dbPath)
		if err != nil {
			continue
		}
		var perms, payload []byte
		var status int
		err = con.QueryRow(`SELECT status, permissions, step_payload FROM steps ORDER BY idx DESC LIMIT 1`).Scan(&status, &perms, &payload)
		con.Close()
		if err == nil {
			if len(perms) > 0 {
				return true
			}
			low := strings.ToLower(string(payload))
			if strings.Contains(low, "ask_question") || strings.Contains(low, "waiting") {
				return true
			}
		}
	}
	return false
}

// CacheItem describes one cleanable conversation/brain UUID entry.
type CacheItem struct {
	InstanceID string    `json:"instanceId"`
	Account    string    `json:"account,omitempty"`
	UUID       string    `json:"uuid"`
	Title      string    `json:"title,omitempty"`
	BrainBytes int64     `json:"brainBytes"`
	ConvBytes  int64     `json:"convBytes"`
	TotalBytes int64     `json:"totalBytes"`
	ModTime    time.Time `json:"modTime"`
	Protected  bool      `json:"protected"` // kept by keepCount or active
	GeminiDir  string    `json:"geminiDir"`
}

// CacheSummary summarizes Brain + Conversations disk footprint per instance.
type CacheSummary struct {
	KeepCount   int              `json:"keepCount"`
	TotalBytes  int64            `json:"totalBytes"`
	Reclaimable int64            `json:"reclaimableBytes"`
	Items       []CacheItem      `json:"items"`
	ByInstance  map[string]int64 `json:"byInstance"`
	ByAccount   map[string]int64 `json:"byAccount,omitempty"`
}

// CleanResult reports what was removed during CleanCache.
type CleanResult struct {
	DeletedCount int   `json:"deletedCount"`
	FreedBytes   int64 `json:"freedBytes"`
}

// ScanCache inventories UUID-named brain/ and conversations/ entries across instances.
func (s *Store) ScanCache(instList []instances.Instance, keepCount int, activeIDs map[string]bool) CacheSummary {
	if keepCount < 1 {
		keepCount = 20
	}
	s.mu.Lock()
	s.refreshSummaries(instList)
	metas := make(map[string]summaryMeta, len(s.summaryMeta))
	for k, v := range s.summaryMeta {
		metas[k] = v
	}
	s.mu.Unlock()

	summary := CacheSummary{
		KeepCount:  keepCount,
		ByInstance: make(map[string]int64),
		ByAccount:  make(map[string]int64),
	}

	for _, inst := range instList {
		acct := strings.TrimSpace(inst.Account)
		for _, gdir := range inst.GeminiDirs {
			itemMap := make(map[string]*CacheItem)
			ensureItem := func(id string) *CacheItem {
				it := itemMap[id]
				if it == nil {
					title := metas[inst.ID+":"+id].Title
					it = &CacheItem{
						InstanceID: inst.ID,
						Account:    acct,
						UUID:       id,
						Title:      title,
						GeminiDir:  gdir,
					}
					itemMap[id] = it
				}
				return it
			}

			brainDir := filepath.Join(gdir, "brain")
			if ents, err := os.ReadDir(brainDir); err == nil {
				for _, ent := range ents {
					if !ent.IsDir() || !uuidRe.MatchString(ent.Name()) {
						continue
					}
					p := filepath.Join(brainDir, ent.Name())
					sz, mt := dirSizeAndMod(p)
					it := ensureItem(ent.Name())
					it.BrainBytes += sz
					it.TotalBytes += sz
					if mt.After(it.ModTime) {
						it.ModTime = mt
					}
				}
			}

			convDir := filepath.Join(gdir, "conversations")
			if ents, err := os.ReadDir(convDir); err == nil {
				for _, ent := range ents {
					if ent.IsDir() {
						continue
					}
					name := ent.Name()
					id := stripConvExt(name)
					if !uuidRe.MatchString(id) {
						continue
					}
					info, err := ent.Info()
					if err != nil {
						continue
					}
					it := ensureItem(id)
					it.ConvBytes += info.Size()
					it.TotalBytes += info.Size()
					if info.ModTime().After(it.ModTime) {
						it.ModTime = info.ModTime()
					}
				}
			}

			var instItems []CacheItem
			for _, it := range itemMap {
				instItems = append(instItems, *it)
			}
			sort.Slice(instItems, func(i, j int) bool {
				return instItems[i].ModTime.After(instItems[j].ModTime)
			})
			for i := range instItems {
				if i < keepCount || activeIDs[instItems[i].UUID] {
					instItems[i].Protected = true
				} else {
					summary.Reclaimable += instItems[i].TotalBytes
				}
				summary.TotalBytes += instItems[i].TotalBytes
				summary.ByInstance[inst.ID] += instItems[i].TotalBytes
				if acct != "" {
					summary.ByAccount[strings.ToLower(acct)] += instItems[i].TotalBytes
				}
				summary.Items = append(summary.Items, instItems[i])
			}
		}
	}

	sort.Slice(summary.Items, func(i, j int) bool {
		if summary.Items[i].Protected != summary.Items[j].Protected {
			return !summary.Items[i].Protected
		}
		return summary.Items[i].TotalBytes > summary.Items[j].TotalBytes
	})
	return summary
}

// CleanCache directly deletes non-protected UUID caches (no backup files).
func (s *Store) CleanCache(instList []instances.Instance, keepCount int, instFilter string, activeIDs map[string]bool) CleanResult {
	scan := s.ScanCache(instList, keepCount, activeIDs)
	acctByInst, _ := buildAccountLookup(instList)
	var res CleanResult
	for _, it := range scan.Items {
		if it.Protected {
			continue
		}
		if !matchesFilter(it.InstanceID, acctByInst[it.InstanceID], instFilter) {
			continue
		}
		if !uuidRe.MatchString(it.UUID) {
			continue
		}
		var freed int64
		brainTarget := filepath.Join(it.GeminiDir, "brain", it.UUID)
		if info, err := os.Stat(brainTarget); err == nil && info.IsDir() {
			if os.RemoveAll(brainTarget) == nil {
				freed += it.BrainBytes
			}
		}
		convDir := filepath.Join(it.GeminiDir, "conversations")
		dbPath := filepath.Join(convDir, it.UUID+".db")
		dbRemoved := true
		if _, err := os.Stat(dbPath); err == nil {
			if err := os.Remove(dbPath); err != nil {
				dbRemoved = false
			}
		}
		if dbRemoved {
			for _, ext := range []string{".db-wal", ".db-shm", ".pb"} {
				_ = os.Remove(filepath.Join(convDir, it.UUID+ext))
			}
			freed += it.ConvBytes
		}
		if freed > 0 {
			res.DeletedCount++
			res.FreedBytes += freed
		}
	}
	return res
}

func stripConvExt(name string) string {
	for _, ext := range []string{".db-wal", ".db-shm", ".db", ".pb"} {
		if strings.HasSuffix(name, ext) {
			return strings.TrimSuffix(name, ext)
		}
	}
	return ""
}

func dirSizeAndMod(dir string) (int64, time.Time) {
	var total int64
	var latest time.Time
	_ = filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			total += info.Size()
			if info.ModTime().After(latest) {
				latest = info.ModTime()
			}
		}
		return nil
	})
	return total, latest
}
