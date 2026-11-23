package telemetry

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/realchendahuang/AgyMate/internal/instances"
)

var xmlTagStripRe = regexp.MustCompile(`</?(?:USER_REQUEST|ADDITIONAL_METADATA|CONTEXT_SUMMARY|SYSTEM_MESSAGE)[^>]*>`)

type speedMetrics struct {
	TotalMs    int64
	TPS        float64
	StreamTPS  float64
	PrefillTPS float64
	CachePct   float64
}

func computeSpeedMetrics(inp, cache, out, resp, ttftMs, streamMs int64) speedMetrics {
	totalMs := ttftMs + streamMs
	var tps, streamTPS, prefillTPS, cachePct float64
	if totalMs > 0 && out > 0 {
		tps = round1(float64(out) / (float64(totalMs) / 1000.0))
	}
	effResp := resp
	if effResp <= 0 {
		effResp = out
	}
	if streamMs >= 100 && effResp > 0 {
		streamTPS = round1(float64(effResp) / (float64(streamMs) / 1000.0))
	} else {
		streamTPS = tps
	}
	promptTok := inp + cache
	if ttftMs > 0 && promptTok > 0 {
		prefillTPS = math.Round(float64(promptTok) / (float64(ttftMs) / 1000.0))
	}
	if promptTok > 0 {
		cachePct = round1(float64(cache) * 100.0 / float64(promptTok))
	}
	return speedMetrics{
		TotalMs:    totalMs,
		TPS:        tps,
		StreamTPS:  streamTPS,
		PrefillTPS: prefillTPS,
		CachePct:   cachePct,
	}
}

func round1(v float64) float64 {
	return math.Round(v*10.0) / 10.0
}

// CascadeSpeedSummary holds lightweight generation speed & latest turn metadata per conversation.
type CascadeSpeedSummary struct {
	CascadeID      string  `json:"cascadeId"`
	ModelName      string  `json:"modelName,omitempty"`
	LastTPS        float64 `json:"lastTps,omitempty"`
	PeakTPS        float64 `json:"peakTps,omitempty"`
	AvgTPS         float64 `json:"avgTps,omitempty"`
	LastTTFTMs     int64   `json:"lastTtftMs,omitempty"`
	LastStreamMs   int64   `json:"lastStreamMs,omitempty"`
	LastPrefillTPS float64 `json:"lastPrefillTps,omitempty"`
	LastOutput     int64   `json:"lastOutputTokens,omitempty"`
	LastPrompt     int64   `json:"lastPromptTokens,omitempty"`
	LastCachePct   float64 `json:"lastCacheHitPct,omitempty"`
	Calls          int     `json:"calls"`
	CompactedTurns int     `json:"compactedTurns,omitempty"`
}

// HeatmapDay summarizes one calendar day of activity for the GitHub-style heatmap.
type HeatmapDay struct {
	Date    string  `json:"date"`
	Calls   int     `json:"calls"`
	Tokens  int64   `json:"tokens"`
	CostUSD float64 `json:"costUsd"`
	Level   int     `json:"level"` // 0..4
}

// TurnTool records one tool invocation linked to a generation turn.
type TurnTool struct {
	Name    string `json:"name"`
	Summary string `json:"summary,omitempty"`
	Tokens  int64  `json:"tokens,omitempty"`
}

// TurnDetail represents one LLM turn for the interactive growth curve and per-turn inspector.
type TurnDetail struct {
	Turn           int        `json:"turn"`
	GenIndex       int        `json:"genIndex"`
	Timestamp      int64      `json:"timestamp"`
	TimeStr        string     `json:"timeStr,omitempty"`
	ModelName      string     `json:"modelName,omitempty"`
	PromptTokens   int64      `json:"promptTokens"`
	InputTokens    int64      `json:"inputTokens"`
	CacheTokens    int64      `json:"cacheTokens"`
	ContextDelta   int64      `json:"contextDelta"`
	OutputTokens   int64      `json:"outputTokens"`
	ThinkingTokens int64      `json:"thinkingTokens"`
	ResponseTokens int64      `json:"responseTokens"`
	TTFTMs         int64      `json:"ttftMs"`
	StreamMs       int64      `json:"streamMs"`
	TotalMs        int64      `json:"totalMs"`
	TPS            float64    `json:"tps"`
	StreamTPS      float64    `json:"streamTps"`
	PrefillTPS     float64    `json:"prefillTps"`
	CacheHitPct    float64    `json:"cacheHitPct"`
	CostUSD        float64    `json:"costUsd"`
	Compacted      bool       `json:"compacted,omitempty"`
	UserSnippet    string     `json:"userSnippet,omitempty"`
	AgentSnippet   string     `json:"agentSnippet,omitempty"`
	Tools          []TurnTool `json:"tools,omitempty"`
}

// CheckpointDetail describes a recorded conversation checkpoint step.
type CheckpointDetail struct {
	Index     int    `json:"index"`
	StartStep int    `json:"startStep,omitempty"`
	EndStep   int    `json:"endStep,omitempty"`
	Intent    string `json:"intent,omitempty"`
}

// ConversationDetail provides per-turn telemetry, growth curve, checkpoints, and step categories for one cascade.
type ConversationDetail struct {
	InstanceID     string             `json:"instanceId,omitempty"`
	CascadeID      string             `json:"cascadeId"`
	Title          string             `json:"title,omitempty"`
	Workspace      string             `json:"workspace,omitempty"`
	ModelName      string             `json:"modelName,omitempty"`
	LastTPS        float64            `json:"lastTps"`
	AvgTPS         float64            `json:"avgTps"`
	PeakTPS        float64            `json:"peakTps"`
	LastTTFTMs     int64              `json:"lastTtftMs"`
	LastPrefillTPS float64            `json:"lastPrefillTps"`
	LastOutput     int64              `json:"lastOutputTokens"`
	TotalInput     int64              `json:"totalInputTokens"`
	TotalCache     int64              `json:"totalCacheTokens"`
	TotalOutput    int64              `json:"totalOutputTokens"`
	TotalThinking  int64              `json:"totalThinkingTokens"`
	TotalResponse  int64              `json:"totalResponseTokens"`
	TotalUSD       float64            `json:"totalUsd"`
	CacheHitPct    float64            `json:"cacheHitPct"`
	CompactedCount int                `json:"compactedCount"`
	Turns          []TurnDetail       `json:"turns"`
	Checkpoints    []CheckpointDetail `json:"checkpoints,omitempty"`
}

// SummarizeLatestByCascade computes fast speed/compaction summaries per conversation from cached telemetry.
func (s *Store) SummarizeLatestByCascade(instList []instances.Instance, instFilter string) map[string]CascadeSpeedSummary {
	records, _, _, _ := s.collect(instList)
	acctByInst, _ := buildAccountLookup(instList)

	type accum struct {
		sum      CascadeSpeedSummary
		lastTS   int64
		lastGIdx int
		tpsSum   float64
		tpsCount int
		prevPrm  int64
	}
	byCID := make(map[string]*accum)

	// Sort chronologically by Timestamp then GenIndex so context drops & latest turns are deterministic.
	filtered := make([]CallRecord, 0, len(records))
	for _, r := range records {
		if matchesFilter(r.InstanceID, acctByInst[r.InstanceID], instFilter) {
			filtered = append(filtered, r)
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].CascadeID != filtered[j].CascadeID {
			return filtered[i].CascadeID < filtered[j].CascadeID
		}
		if filtered[i].GenIndex != filtered[j].GenIndex {
			return filtered[i].GenIndex < filtered[j].GenIndex
		}
		return filtered[i].Timestamp < filtered[j].Timestamp
	})

	for _, r := range filtered {
		a := byCID[r.CascadeID]
		if a == nil {
			a = &accum{sum: CascadeSpeedSummary{CascadeID: r.CascadeID}}
			byCID[r.CascadeID] = a
		}
		a.sum.Calls++
		prompt := r.Input + r.CacheRead
		if a.prevPrm > 0 && a.prevPrm-prompt > 5000 && float64(prompt) <= float64(a.prevPrm)*0.75 {
			a.sum.CompactedTurns++
		}
		a.prevPrm = prompt

		sm := computeSpeedMetrics(r.Input, r.CacheRead, r.Output, r.ResponseOut, r.TTFTMs, r.StreamMs)
		if sm.TPS > 0 {
			a.tpsSum += sm.TPS
			a.tpsCount++
			if sm.TPS > a.sum.PeakTPS {
				a.sum.PeakTPS = sm.TPS
			}
		}
		isNewer := r.Timestamp > a.lastTS || (r.Timestamp == a.lastTS && r.GenIndex >= a.lastGIdx)
		if isNewer {
			a.lastTS = r.Timestamp
			a.lastGIdx = r.GenIndex
			a.sum.ModelName = r.ModelName
			a.sum.LastTTFTMs = r.TTFTMs
			a.sum.LastStreamMs = r.StreamMs
			a.sum.LastOutput = r.Output
			a.sum.LastPrompt = prompt
			a.sum.LastCachePct = sm.CachePct
			if sm.TPS > 0 {
				a.sum.LastTPS = sm.TPS
			}
			if sm.PrefillTPS > 0 {
				a.sum.LastPrefillTPS = sm.PrefillTPS
			}
		}
	}

	out := make(map[string]CascadeSpeedSummary, len(byCID))
	for cid, a := range byCID {
		if a.tpsCount > 0 {
			a.sum.AvgTPS = round1(a.tpsSum / float64(a.tpsCount))
		}
		out[cid] = a.sum
	}
	return out
}

// BuildHeatmap returns chronologically ordered daily token/cost activity over the trailing N weeks.
func (s *Store) BuildHeatmap(instList []instances.Instance, instFilter, mode string, weeks int) []HeatmapDay {
	if weeks <= 0 {
		weeks = 12
	}
	if weeks > 53 {
		weeks = 53
	}
	mode = strings.TrimSpace(mode)
	if mode == "" {
		mode = "std"
	}

	records, imgs, _, _ := s.collect(instList)
	acctByInst, _ := buildAccountLookup(instList)
	loc := time.Now().Location()
	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	totalDays := weeks * 7
	startDay := today.AddDate(0, 0, -(totalDays - 1))
	startTS := startDay.Unix()

	type dayAgg struct {
		calls  int
		tokens int64
		usd    float64
	}
	buckets := make(map[string]*dayAgg)
	for _, r := range records {
		if r.Timestamp < startTS {
			continue
		}
		if !matchesFilter(r.InstanceID, acctByInst[r.InstanceID], instFilter) {
			continue
		}
		dateKey := time.Unix(r.Timestamp, 0).In(loc).Format("2006-01-02")
		d := buckets[dateKey]
		if d == nil {
			d = &dayAgg{}
			buckets[dateKey] = d
		}
		cc := ComputeCallCost(r.ModelName, r.ModelID, r.Input, r.CacheRead, r.Output, r.Thinking, r.ResponseOut, mode)
		d.calls++
		d.tokens += r.Input + r.CacheRead + r.Output
		d.usd += cc.TotalUSD
	}
	for _, img := range imgs {
		if img.Timestamp < startTS {
			continue
		}
		if !matchesFilter(img.InstanceID, acctByInst[img.InstanceID], instFilter) {
			continue
		}
		dateKey := time.Unix(img.Timestamp, 0).In(loc).Format("2006-01-02")
		d := buckets[dateKey]
		if d == nil {
			d = &dayAgg{}
			buckets[dateKey] = d
		}
		d.calls++
		d.usd += imageGenUnitCost
	}

	var maxTokens int64
	for _, d := range buckets {
		if d.tokens > maxTokens {
			maxTokens = d.tokens
		}
	}

	days := make([]HeatmapDay, 0, totalDays)
	for i := 0; i < totalDays; i++ {
		dt := startDay.AddDate(0, 0, i).Format("2006-01-02")
		hd := HeatmapDay{Date: dt}
		if agg, ok := buckets[dt]; ok {
			hd.Calls = agg.calls
			hd.Tokens = agg.tokens
			hd.CostUSD = agg.usd
			if agg.tokens > 0 && maxTokens > 0 {
				ratio := float64(agg.tokens) / float64(maxTokens)
				switch {
				case ratio >= 0.75:
					hd.Level = 4
				case ratio >= 0.45:
					hd.Level = 3
				case ratio >= 0.18:
					hd.Level = 2
				default:
					hd.Level = 1
				}
			} else if agg.calls > 0 {
				hd.Level = 1
			}
		}
		days = append(days, hd)
	}
	return days
}

// QueryConversationDetail parses per-turn timing, snippets, tool token bills, and checkpoints for one cascade.
func (s *Store) QueryConversationDetail(_ context.Context, instList []instances.Instance, instFilter, cascadeID, mode string) ConversationDetail {
	detail := ConversationDetail{
		CascadeID: cascadeID,
	}
	if !uuidRe.MatchString(cascadeID) {
		return detail
	}
	if mode == "" {
		mode = "std"
	}

	// Refresh summaries so title/workspace are available.
	s.mu.Lock()
	s.refreshSummaries(instList)
	metas := make(map[string]summaryMeta, len(s.summaryMeta))
	for k, v := range s.summaryMeta {
		metas[k] = v
	}
	s.mu.Unlock()

	var dbPath string
	var resolvedInst string
	for _, inst := range instList {
		if instFilter != "" && instFilter != "all" && inst.ID != instFilter {
			continue
		}
		for _, gdir := range inst.GeminiDirs {
			p := filepath.Join(gdir, "conversations", cascadeID+".db")
			if _, err := os.Stat(p); err == nil {
				dbPath = p
				resolvedInst = inst.ID
				break
			}
		}
		if dbPath != "" {
			break
		}
	}
	if dbPath == "" && instFilter != "" && instFilter != "all" {
		// Retry without instance filter in case cascade belongs to a companion slot.
		for _, inst := range instList {
			for _, gdir := range inst.GeminiDirs {
				p := filepath.Join(gdir, "conversations", cascadeID+".db")
				if _, err := os.Stat(p); err == nil {
					dbPath = p
					resolvedInst = inst.ID
					break
				}
			}
			if dbPath != "" {
				break
			}
		}
	}

	if sm, ok := metas[resolvedInst+":"+cascadeID]; ok {
		detail.Title = sm.Title
		detail.Workspace = sm.Workspace
	}
	detail.InstanceID = resolvedInst
	if dbPath == "" {
		return detail
	}

	con, err := openReadOnlySQLite(dbPath)
	if err != nil {
		return detail
	}
	defer con.Close()

	toolsByGen := make(map[int][]TurnTool)
	userByGen := make(map[int]string)
	agentByGen := make(map[int]string)
	tsByStep := make(map[int]int64)
	pendingUser := ""

	stepRows, err := con.Query(`SELECT idx, step_type, metadata, step_payload FROM steps ORDER BY idx ASC`)
	if err == nil {
		for stepRows.Next() {
			var idx, stype int
			var meta, pay []byte
			if stepRows.Scan(&idx, &stype, &meta, &pay) != nil {
				continue
			}
			md := parseProtoDict(meta)
			var ts int64
			if b1 := md.firstBytes(1); len(b1) > 0 {
				ts = int64(parseProtoDict(b1).firstVarint(1))
			}
			tsByStep[idx] = ts

			payDict := parseProtoDict(pay)
			if stype == 14 { // USER_INPUT
				if uBytes := payDict.firstBytes(19); len(uBytes) > 0 {
					ui := parseProtoDict(uBytes)
					if txt := cleanSnippet(ui.firstString(2), 180); txt != "" {
						pendingUser = txt
					}
				}
			}
			if stype == 23 { // CHECKPOINT
				if cBytes := payDict.firstBytes(30); len(cBytes) > 0 {
					ck := parseProtoDict(cBytes)
					detail.Checkpoints = append(detail.Checkpoints, CheckpointDetail{
						Index:     int(ck.firstVarint(1)),
						StartStep: int(ck.firstVarint(11)),
						EndStep:   int(ck.firstVarint(12)),
						Intent:    cleanSnippet(ck.firstString(4), 100),
					})
				}
			}

			gIdx := -1
			if b20 := md.firstBytes(20); len(b20) > 0 {
				link := parseProtoDict(b20)
				if _, ok := link[3]; ok {
					gIdx = int(link.firstVarint(3))
				}
			}
			if gIdx < 0 {
				continue
			}
			if pendingUser != "" {
				if _, exists := userByGen[gIdx]; !exists {
					userByGen[gIdx] = pendingUser
					pendingUser = ""
				}
			}
			if stype == 15 { // PLANNER_RESPONSE
				if pBytes := payDict.firstBytes(20); len(pBytes) > 0 {
					pr := parseProtoDict(pBytes)
					if txt := cleanSnippet(pr.firstString(1), 200); txt != "" {
						agentByGen[gIdx] = txt
					}
				}
			}
			if b4 := md.firstBytes(4); len(b4) > 0 { // tool_call
				tc := parseProtoDict(b4)
				name := strings.TrimSpace(tc.firstString(2))
				if name != "" {
					argsJSON := tc.firstString(3)
					tok := int64(md.firstVarint(23))
					if tok <= 0 && len(argsJSON) > 0 {
						tok = int64(maxInt(1, len(argsJSON)/4))
					}
					toolsByGen[gIdx] = append(toolsByGen[gIdx], TurnTool{
						Name:    name,
						Summary: summarizeToolArgs(argsJSON),
						Tokens:  tok,
					})
				}
			}
		}
		stepRows.Close()
	}

	grows, err := con.Query(`SELECT idx, data FROM gen_metadata ORDER BY idx ASC`)
	if err != nil {
		return detail
	}
	defer grows.Close()

	var turns []TurnDetail
	var prevPrompt int64
	var tpsSum float64
	var tpsCount int
	var totalMS int64

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
		u4 := parseProtoDict(m1.firstBytes(4))
		inp := int64(u4.firstVarint(2))
		out := int64(u4.firstVarint(3))
		cache := int64(u4.firstVarint(5))
		think := int64(u4.firstVarint(9))
		resp := int64(u4.firstVarint(10))
		if resp <= 0 && out > think {
			resp = out - think
		}
		if inp == 0 && cache == 0 && out == 0 {
			continue
		}

		me := m1.firstVarint(3)
		if me == 0 {
			me = u4.firstVarint(1)
		}
		mid := m1.firstString(19)
		mn := m1.firstString(21)
		modelName := resolveModelName(me, mid, mn, s.labelFn)

		var ts int64
		for _, f := range m1[20] {
			kvd := parseProtoDict(f.bytes)
			if kvd.firstString(1) == "last_step_index" {
				if stepNo, convErr := parsePositiveInt(kvd.firstString(2)); convErr {
					if t, ok := tsByStep[stepNo]; ok {
						ts = t
					} else if t, ok := tsByStep[stepNo+1]; ok {
						ts = t
					}
				}
			}
		}
		if ts == 0 {
			// Fall back to step timestamp from tools or zero.
			if u4Ts := int64(u4.firstVarint(8)); u4Ts > 1_000_000_000 {
				ts = u4Ts
			}
		}

		prompt := inp + cache
		delta := prompt
		compacted := false
		if prevPrompt > 0 {
			delta = prompt - prevPrompt
			if prevPrompt-prompt > 5000 && float64(prompt) <= float64(prevPrompt)*0.75 {
				compacted = true
				detail.CompactedCount++
			}
		}
		prevPrompt = prompt

		ttftMs := parseProtoDurationMs(m1.firstBytes(11))
		streamMs := parseProtoDurationMs(m1.firstBytes(12))
		sm := computeSpeedMetrics(inp, cache, out, resp, ttftMs, streamMs)
		cc := ComputeCallCost(modelName, mid, inp, cache, out, think, resp, mode)

		detail.TotalInput += inp
		detail.TotalCache += cache
		detail.TotalOutput += out
		detail.TotalThinking += think
		detail.TotalResponse += resp
		detail.TotalUSD += cc.TotalUSD
		detail.ModelName = modelName
		if out > 0 {
			detail.LastOutput = out
		}
		if sm.TPS > 0 {
			tpsSum += sm.TPS
			tpsCount++
			detail.LastTPS = sm.TPS
			if sm.TPS > detail.PeakTPS {
				detail.PeakTPS = sm.TPS
			}
		}
		if ttftMs > 0 {
			detail.LastTTFTMs = ttftMs
		}
		if sm.PrefillTPS > 0 {
			detail.LastPrefillTPS = sm.PrefillTPS
		}
		totalMS += sm.TotalMs

		timeStr := ""
		if ts > 0 {
			timeStr = time.Unix(ts, 0).Format("15:04:05")
		}

		turns = append(turns, TurnDetail{
			Turn:           len(turns) + 1,
			GenIndex:       gidx,
			Timestamp:      ts,
			TimeStr:        timeStr,
			ModelName:      modelName,
			PromptTokens:   prompt,
			InputTokens:    inp,
			CacheTokens:    cache,
			ContextDelta:   delta,
			OutputTokens:   out,
			ThinkingTokens: think,
			ResponseTokens: resp,
			TTFTMs:         ttftMs,
			StreamMs:       streamMs,
			TotalMs:        sm.TotalMs,
			TPS:            sm.TPS,
			StreamTPS:      sm.StreamTPS,
			PrefillTPS:     sm.PrefillTPS,
			CacheHitPct:    sm.CachePct,
			CostUSD:        cc.TotalUSD,
			Compacted:      compacted,
			UserSnippet:    userByGen[gidx],
			AgentSnippet:   agentByGen[gidx],
			Tools:          toolsByGen[gidx],
		})
	}
	_ = totalMS

	if tpsCount > 0 {
		detail.AvgTPS = round1(tpsSum / float64(tpsCount))
	}
	if promptDen := detail.TotalInput + detail.TotalCache; promptDen > 0 {
		detail.CacheHitPct = round1(float64(detail.TotalCache) * 100.0 / float64(promptDen))
	}
	if len(turns) > 60 {
		turns = turns[len(turns)-60:]
	}
	detail.Turns = turns
	return detail
}

func parsePositiveInt(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	v := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		v = v*10 + int(c-'0')
	}
	return v, true
}

func cleanSnippet(raw string, maxRunes int) string {
	s := xmlTagStripRe.ReplaceAllString(raw, " ")
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return ""
	}
	return truncateRunes(s, maxRunes)
}

func truncateRunes(s string, maxRunes int) string {
	if maxRunes <= 0 || utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	out := make([]rune, 0, maxRunes)
	for _, r := range s {
		if len(out) >= maxRunes-1 {
			break
		}
		out = append(out, r)
	}
	return string(out) + "…"
}

func summarizeToolArgs(argsJSON string) string {
	if strings.TrimSpace(argsJSON) == "" {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &m); err != nil {
		return ""
	}
	for _, k := range []string{"Path", "AbsolutePath", "FilePath", "TargetFile", "DirectoryPath"} {
		if v, ok := m[k].(string); ok && strings.TrimSpace(v) != "" {
			return truncateRunes(strings.TrimSpace(v), 64)
		}
	}
	if s, ok := m["toolSummary"].(string); ok && strings.TrimSpace(s) != "" {
		return truncateRunes(strings.TrimSpace(s), 64)
	}
	if c, ok := m["CommandLine"].(string); ok && strings.TrimSpace(c) != "" {
		line := strings.SplitN(strings.TrimSpace(c), "\n", 2)[0]
		return truncateRunes(line, 64)
	}
	for _, k := range []string{"query", "Query", "SearchTerm", "Pattern", "Url", "Uri", "Role"} {
		if v, ok := m[k].(string); ok && strings.TrimSpace(v) != "" {
			return truncateRunes(strings.TrimSpace(v), 64)
		}
	}
	return ""
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
