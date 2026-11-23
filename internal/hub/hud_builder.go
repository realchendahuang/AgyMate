// Package hub: CDP window HUD payload assembly and telemetry enrichment.
package hub

import (
	"context"
	"fmt"
	"github.com/realchendahuang/AgyMate/internal/cdp"
	"github.com/realchendahuang/AgyMate/internal/colab"
	"github.com/realchendahuang/AgyMate/internal/config"
	"github.com/realchendahuang/AgyMate/internal/hooks"
	"github.com/realchendahuang/AgyMate/internal/instances"
	"github.com/realchendahuang/AgyMate/internal/rpc"
	"github.com/realchendahuang/AgyMate/internal/telemetry"
	"sort"
	"strings"
	"time"
)

// SyncCDPOnce pushes Chinese localization & HUD state across reachable CDP targets.
func (s *Server) SyncCDPOnce(ctx context.Context, force bool) []cdp.TargetStatus {
	cfg := s.Config()
	s.mu.RLock()
	instList := make([]instances.Instance, len(s.instList))
	copy(instList, s.instList)
	snaps := make(map[string]rpc.InstanceSnapshot, len(s.snapshots))
	for k, v := range s.snapshots {
		snaps[k] = v
	}
	instToday := make(map[string]telemetry.InstAggregate, len(s.instToday))
	for k, v := range s.instToday {
		instToday[k] = v
	}
	s.mu.RUnlock()

	if len(instList) == 0 {
		instList = instances.DiscoverAll(cfg)
	}
	liveSessions := s.tracker.Snapshot()

	instByID := make(map[string]instances.Instance, len(instList))
	for _, item := range instList {
		instByID[item.ID] = item
	}
	colabSnap := s.colabCollector.Last()
	now := time.Now()

	hudFn := func(instID string) *cdp.HUDData {
		inst, ok := instByID[instID]
		if !ok {
			inst = instances.Instance{ID: instID, Running: true}
		}
		return s.buildHUDData(inst, snaps[inst.ID], instToday[inst.ID], instToday, liveSessions, colabSnap, cfg, now)
	}

	targets := s.cdpWatcher.SyncAll(ctx, instList, cfg, hudFn, force)
	s.broadcast("cdp")
	return targets
}

// buildHUDData assembles fully localized Simplified Chinese telemetry for an Antigravity window HUD.
func (s *Server) buildHUDData(
	inst instances.Instance,
	snap rpc.InstanceSnapshot,
	today telemetry.InstAggregate,
	allToday map[string]telemetry.InstAggregate,
	liveSessions []hooks.LiveSession,
	colabSnap colab.Snapshot,
	cfg config.Config,
	now time.Time,
) *cdp.HUDData {
	badge := formatInstanceBadge(inst)
	account := strings.TrimSpace(firstNonEmpty(inst.Account, snap.Email))
	if account == "" && s.state != nil {
		account = strings.TrimSpace(s.state.LastInstanceAccount(inst.ID))
	}

	state := "idle"
	var activeTitle, activeModel string
	if !inst.Running {
		state = "offline"
	} else {
		for _, ls := range liveSessions {
			if ls.InstanceID != inst.ID {
				continue
			}
			if ls.State == "waiting_approval" {
				state = "waiting_approval"
				activeTitle = firstNonEmpty(ls.Title, activeTitle)
				activeModel = firstNonEmpty(ls.Model, activeModel)
				break
			}
			if ls.State == "running" {
				state = "running"
				if activeTitle == "" {
					activeTitle = ls.Title
				}
				if activeModel == "" {
					activeModel = ls.Model
				}
			}
		}
		if state == "idle" && snap.RunningCount > 0 {
			state = "running"
		}
	}

	if snap.Context != nil {
		if activeTitle == "" {
			activeTitle = snap.Context.Title
		}
		if activeModel == "" {
			activeModel = snap.Context.Model
		}
	}
	if activeTitle == "" && len(snap.Trajectories) > 0 {
		activeTitle = snap.Trajectories[0].Title
	}
	activeTitle = sanitizeHUDTitle(activeTitle)

	planName := localizePlanName(snap.PlanName, snap.TierID)
	buckets := snap.Buckets
	cachedQuota := false
	if (len(buckets) == 0 || planName == "") && account != "" && s.state != nil {
		if cached, ok := s.state.GetAccountSnapshot(account, now); ok {
			if planName == "" {
				planName = localizePlanName(cached.PlanName, cached.TierID)
			}
			if len(buckets) == 0 && len(cached.Buckets) > 0 {
				buckets = fromCachedBuckets(cached.Buckets)
				cachedQuota = true
			}
		}
	}

	// If Antigravity IDE shares conversation storage with main and has 0 dedicated calls, surface account-shared main stats.
	if inst.Kind == instances.KindIDE && today.Calls == 0 && allToday != nil {
		if mainAgg, ok := allToday[instances.KindMain]; ok {
			if account == "" || mainAgg.Account == "" || strings.EqualFold(mainAgg.Account, account) {
				today = mainAgg
			}
		}
	}

	curSym := "$"
	if strings.EqualFold(cfg.Pricing.Currency, "CNY") {
		curSym = "¥"
	}
	hd := cdp.HUDData{
		InstanceID:      inst.ID,
		Lang:            config.NormalizeLanguage(cfg.Language),
		Badge:           badge,
		Account:         account,
		PlanName:        planName,
		State:           state,
		StateLabel:      localizeLiveState(state),
		Position:        config.NormalizeHUDPosition(cfg.CDP.HUDPosition),
		HubURL:          "http://" + cfg.ListenAddr,
		ActiveTitle:     activeTitle,
		ActiveModel:     activeModel,
		CachedQuota:     cachedQuota,
		CheckpointIndex: -1,
		ContextWarnPct:  cfg.ContextWarnPct,
		QuotaPct:        -1,
		TodayCostLabel:  formatMoney(today.TotalUSD, cfg.Pricing),
		TodayTokens:     today.Total,
		CacheHitRate:    today.CacheRate,
		CurrencySymbol:  curSym,
	}

	if matchedColab := resolveColabForAccount(colabSnap, account); matchedColab != nil {
		hd.ColabBurning = matchedColab.AssignmentsCount > 0 || matchedColab.ConsumptionRateHourly > 0.01
		if hd.ColabBurning {
			hd.ColabLabel = fmt.Sprintf("🔥 %d台实例 · %.2f CCU/小时 · %.0f CCU",
				matchedColab.AssignmentsCount, matchedColab.ConsumptionRateHourly, matchedColab.CurrentBalance)
		} else {
			hd.ColabLabel = fmt.Sprintf("%.0f CCU · 空闲", matchedColab.CurrentBalance)
		}
	}

	if snap.Context != nil {
		hd.ActiveCascadeID = snap.Context.CascadeID
		hd.ContextUsed = snap.Context.EstimatedTokensUsed
		hd.ContextMax = snap.Context.MaxContextTokens
		hd.ContextPct = snap.Context.UsagePct
		hd.CheckpointIndex = snap.Context.CheckpointIndex
		hd.TruncationReason = snap.Context.TruncationReason
		hd.Compacted = snap.Context.CheckpointIndex >= 0
		hd.Breakdown = toHUDBreakdown(snap.Context.TokenBreakdown)
	}

	if len(buckets) > 0 {
		for _, b := range buckets {
			grp := rpc.LocalizeBucketGroup(b.GroupName, b.DisplayName, b.BucketID)
			_, _, canonWin := rpc.LocalizeQuotaWindow(b.Window, b.DisplayName, b.BucketID)
			bName := rpc.FormatBucketDisplayName(b.GroupName, b.DisplayName, b.Window, b.BucketID)
			resetLeft := cdp.FormatResetCountdown(b.ResetUnix)
			var trend []float64
			if s.state != nil && b.BucketID != "" {
				trend = s.state.BucketTrendPct(inst.ID+":"+b.BucketID, 24)
			}
			hd.Buckets = append(hd.Buckets, cdp.HUDBucket{
				ID:                  b.BucketID,
				Name:                bName,
				Group:               grp,
				Window:              canonWin,
				Description:         b.Description,
				Disabled:            b.Disabled,
				Pct:                 b.RemainingPct,
				ResetLeft:           resetLeft,
				BurnHint:            formatBucketBurnHint(b, resetLeft),
				BurnPctPerHour:      b.BurnPctPerHour,
				HoursUntilEmpty:     b.HoursUntilEmpty,
				ExhaustsBeforeReset: b.ExhaustsBeforeReset,
				Trend:               trend,
			})
		}
		qLabel, qPct := pickPrimaryQuotaForHUD(buckets, activeModel, cfg.QuotaWarnPct)
		hd.QuotaLabel = qLabel
		hd.QuotaPct = qPct
		hd.BurnHint = pickTopBurnHintForHUD(buckets)
	}

	if len(snap.Models) > 0 {
		modelsCopy := append([]rpc.ModelQuota(nil), snap.Models...)
		sort.SliceStable(modelsCopy, func(i, j int) bool {
			return modelsCopy[i].RemainingPct < modelsCopy[j].RemainingPct
		})
		limit := len(modelsCopy)
		if limit > 14 {
			limit = 14
		}
		for _, mq := range modelsCopy[:limit] {
			resetUnix := parseModelResetUnix(mq.ResetTime)
			hd.Models = append(hd.Models, cdp.HUDModelQuota{
				Label:     firstNonEmpty(mq.Label, mq.ModelID),
				ModelID:   mq.ModelID,
				Pct:       mq.RemainingPct,
				ResetLeft: cdp.FormatResetCountdown(resetUnix),
			})
		}
	}

	s.enrichHUDTelemetry(&hd, inst, snap, cfg, now)

	return &hd
}

func toHUDBreakdown(src []rpc.ContextGroup) []cdp.HUDBreakdown {
	if len(src) == 0 {
		return nil
	}
	out := make([]cdp.HUDBreakdown, 0, len(src))
	for _, grp := range src {
		hb := cdp.HUDBreakdown{
			Key:    grp.Key,
			Name:   rpc.LocalizeContextGroupName(grp.Name),
			Tokens: grp.NumTokens,
		}
		for _, ch := range grp.Children {
			hb.Children = append(hb.Children, cdp.HUDBreakdown{
				Name:   ch.Name,
				Tokens: ch.NumTokens,
			})
		}
		out = append(out, hb)
	}
	return out
}

func parseModelResetUnix(iso string) int64 {
	iso = strings.TrimSpace(iso)
	if iso == "" {
		return 0
	}
	t, err := time.Parse(time.RFC3339Nano, iso)
	if err != nil {
		t, err = time.Parse(time.RFC3339, iso)
		if err != nil {
			return 0
		}
	}
	return t.Unix()
}

func (s *Server) enrichHUDTelemetry(
	hd *cdp.HUDData,
	inst instances.Instance,
	snap rpc.InstanceSnapshot,
	cfg config.Config,
	now time.Time,
) {
	if hd == nil {
		return
	}
	s.mu.RLock()
	instList := make([]instances.Instance, len(s.instList))
	copy(instList, s.instList)
	allSnaps := make(map[string]rpc.InstanceSnapshot, len(s.snapshots))
	for k, v := range s.snapshots {
		allSnaps[k] = v
	}
	s.mu.RUnlock()

	if len(instList) == 0 {
		instList = []instances.Instance{inst}
	}

	var speedByCID map[string]telemetry.CascadeSpeedSummary
	if s.store != nil {
		filter := inst.ID
		if hd.Account != "" {
			filter = "acct:" + hd.Account
		}
		speedByCID = s.store.SummarizeLatestByCascade(instList, "all")
		rep := s.store.QueryTelemetry(instList, "today", cfg.Pricing.Mode, filter, 0)
		if rep.Summary.SavedByCacheUSD > 0 {
			hd.CacheSavedLabel = formatMoney(rep.Summary.SavedByCacheUSD, cfg.Pricing)
		}
		// 12-week compact daily activity heatmap
		hm := s.store.BuildHeatmap(instList, filter, cfg.Pricing.Mode, 12)
		if len(hm) > 0 {
			hd.Heatmap = make([]cdp.HUDHeatDay, 0, len(hm))
			for _, d := range hm {
				hd.Heatmap = append(hd.Heatmap, cdp.HUDHeatDay{
					Date:   d.Date,
					Tokens: d.Tokens,
					Calls:  d.Calls,
					USD:    d.CostUSD,
					Level:  d.Level,
				})
			}
		}

		// Populate active conversation speed metrics & recent turns
		if hd.ActiveCascadeID != "" {
			if sm, ok := speedByCID[hd.ActiveCascadeID]; ok {
				hd.LastTPS = sm.LastTPS
				hd.AvgTPS = sm.AvgTPS
				hd.PeakTPS = sm.PeakTPS
				hd.LastTTFTMs = sm.LastTTFTMs
				hd.LastPrefillTPS = sm.LastPrefillTPS
				hd.LastOutputTokens = sm.LastOutput
				if sm.CompactedTurns > 0 {
					hd.Compacted = true
				}
			}
			cd := s.store.QueryConversationDetail(context.Background(), instList, inst.ID, hd.ActiveCascadeID, cfg.Pricing.Mode)
			if len(cd.Turns) > 0 {
				start := 0
				if len(cd.Turns) > 16 {
					start = len(cd.Turns) - 16
				}
				for _, td := range cd.Turns[start:] {
					hd.RecentTurns = append(hd.RecentTurns, cdp.HUDTurnBrief{
						Turn:           td.Turn,
						TimeStr:        td.TimeStr,
						Model:          td.ModelName,
						PromptTokens:   td.PromptTokens,
						ContextDelta:   td.ContextDelta,
						OutputTokens:   td.OutputTokens,
						ThinkingTokens: td.ThinkingTokens,
						ResponseTokens: td.ResponseTokens,
						TTFTMs:         td.TTFTMs,
						StreamMs:       td.StreamMs,
						TPS:            td.TPS,
						PrefillTPS:     td.PrefillTPS,
						CacheHitPct:    td.CacheHitPct,
						Compacted:      td.Compacted,
						UserSnippet:    truncateRunes(td.UserSnippet, 72),
					})
				}
			}
		}
	}

	// Build per-conversation context map for split-pane / multi-tab ring isolation.
	ctxMap := make(map[string]cdp.HUDContextBrief)
	addCtx := func(cw *rpc.ContextWindow) {
		if cw == nil || cw.CascadeID == "" {
			return
		}
		brief := cdp.HUDContextBrief{
			CascadeID:        cw.CascadeID,
			Title:            sanitizeHUDTitle(cw.Title),
			Model:            cw.Model,
			Used:             cw.EstimatedTokensUsed,
			Max:              cw.MaxContextTokens,
			Pct:              cw.UsagePct,
			Compacted:        cw.CheckpointIndex >= 0,
			CheckpointIndex:  cw.CheckpointIndex,
			TruncationReason: cw.TruncationReason,
			Breakdown:        toHUDBreakdown(cw.TokenBreakdown),
		}
		if sm, ok := speedByCID[cw.CascadeID]; ok {
			brief.LastTPS = sm.LastTPS
			brief.LastTTFTMs = sm.LastTTFTMs
			brief.LastPrefillTPS = sm.LastPrefillTPS
			brief.LastOutputTokens = sm.LastOutput
			brief.CacheHitRate = sm.LastCachePct
			if sm.CompactedTurns > 0 {
				brief.Compacted = true
			}
		}
		ctxMap[cw.CascadeID] = brief
	}
	if snap.Context != nil {
		addCtx(snap.Context)
	}
	for _, cw := range snap.TrajectoryContexts {
		addCtx(cw)
	}
	if len(ctxMap) > 0 {
		hd.CascadeContexts = ctxMap
	}

	// Build concise multi-account summary strip.
	type acctEntry struct {
		email   string
		plan    string
		tight   float64
		running int
		active  bool
	}
	acctSeen := make(map[string]*acctEntry)
	upsertAcct := func(email, plan string, tight float64, running int) {
		em := strings.TrimSpace(email)
		if em == "" {
			return
		}
		low := strings.ToLower(em)
		ae := acctSeen[low]
		if ae == nil {
			ae = &acctEntry{
				email:  em,
				plan:   plan,
				tight:  tight,
				active: hd.Account != "" && strings.EqualFold(hd.Account, em),
			}
			acctSeen[low] = ae
		} else {
			if plan != "" && ae.plan == "" {
				ae.plan = plan
			}
			if tight >= 0 && (ae.tight < 0 || tight < ae.tight) {
				ae.tight = tight
			}
		}
		ae.running += running
	}
	for _, it := range instList {
		sp := allSnaps[it.ID]
		em := firstNonEmpty(it.Account, sp.Email)
		if em == "" {
			continue
		}
		tight := -1.0
		for _, b := range sp.Buckets {
			if b.Disabled {
				continue
			}
			if tight < 0 || b.RemainingPct < tight {
				tight = b.RemainingPct
			}
		}
		run := 0
		if it.Running {
			run = 1
		}
		upsertAcct(em, localizePlanName(sp.PlanName, sp.TierID), tight, run)
	}
	if s.state != nil {
		for k, cached := range s.state.AllAccountSnapshots(now) {
			em := firstNonEmpty(cached.Email, k)
			tight := -1.0
			for _, b := range cached.Buckets {
				if b.Disabled {
					continue
				}
				if tight < 0 || b.RemainingPct < tight {
					tight = b.RemainingPct
				}
			}
			upsertAcct(em, localizePlanName(cached.PlanName, cached.TierID), tight, 0)
		}
	}
	if len(acctSeen) > 0 {
		hd.AccountCount = len(acctSeen)
		list := make([]cdp.HUDAccountBrief, 0, len(acctSeen))
		for _, ae := range acctSeen {
			list = append(list, cdp.HUDAccountBrief{
				Email:        ae.email,
				PlanName:     ae.plan,
				TightestPct:  ae.tight,
				Active:       ae.active,
				RunningSlots: ae.running,
			})
		}
		sort.Slice(list, func(i, j int) bool {
			if list[i].Active != list[j].Active {
				return list[i].Active
			}
			return list[i].Email < list[j].Email
		})
		hd.Accounts = list
	}
}

func formatInstanceBadge(inst instances.Instance) string {
	switch strings.ToLower(strings.TrimSpace(inst.ID)) {
	case instances.KindMain:
		return "主窗口"
	case instances.KindIDE:
		return "IDE"
	}
	if desc := strings.TrimSpace(inst.Description); desc != "" {
		return truncateRunes(desc, 10)
	}
	if lbl := strings.TrimSpace(inst.Label); lbl != "" {
		return truncateRunes(lbl, 10)
	}
	return strings.ToUpper(strings.TrimSpace(inst.ID))
}

func sanitizeHUDTitle(title string) string {
	t := strings.TrimSpace(title)
	if t == "" || looksLikeUUID(t) {
		return ""
	}
	return truncateRunes(t, 44)
}

func looksLikeUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, ch := range s {
		switch i {
		case 8, 13, 18, 23:
			if ch != '-' {
				return false
			}
		default:
			if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')) {
				return false
			}
		}
	}
	return true
}

func truncateRunes(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes || maxRunes <= 0 {
		return s
	}
	return string(runes[:maxRunes]) + "…"
}

func formatHoursCN(hours float64) string {
	if hours <= 0 {
		return "即将"
	}
	if hours < 1.0 {
		mins := int(hours*60.0 + 0.5)
		if mins < 1 {
			mins = 1
		}
		return fmt.Sprintf("约 %d 分钟", mins)
	}
	if hours >= 24.0 {
		days := int(hours) / 24
		remH := int(hours) % 24
		return fmt.Sprintf("约 %d天%d小时", days, remH)
	}
	return fmt.Sprintf("约 %.1f 小时", hours)
}

func formatBucketBurnHint(b rpc.QuotaBucket, resetLeft string) string {
	if b.Disabled {
		if strings.TrimSpace(b.Description) != "" {
			return "受上级窗口限制，暂不适用"
		}
		return "当前窗口暂不适用"
	}
	if b.RemainingPct <= 0.05 && b.ResetUnix > 0 {
		if resetLeft != "" && resetLeft != "即将重置" {
			return fmt.Sprintf("当前窗口额度已耗尽，%s后自动恢复", resetLeft)
		}
		return "当前窗口额度已耗尽，等待重置"
	}
	if b.ExhaustsBeforeReset && b.HoursUntilEmpty > 0 {
		return fmt.Sprintf("按当前消耗（%.0f%%/小时）预计 %s耗尽", b.BurnPctPerHour, formatHoursCN(b.HoursUntilEmpty))
	}
	return ""
}

func modelMatchesBucketGroup(activeModel, groupLabel string) bool {
	m := strings.ToLower(strings.TrimSpace(activeModel))
	g := strings.ToLower(strings.TrimSpace(groupLabel))
	if m == "" || g == "" {
		return false
	}
	switch {
	case strings.Contains(m, "claude"):
		return strings.Contains(g, "claude")
	case strings.Contains(m, "gpt") || strings.Contains(m, "o1") || strings.Contains(m, "o3") || strings.Contains(m, "openai"):
		return strings.Contains(g, "gpt")
	case strings.Contains(m, "gemini") || strings.Contains(m, "orchestrator"):
		return strings.Contains(g, "gemini")
	default:
		return false
	}
}

func pickPrimaryQuotaForHUD(buckets []rpc.QuotaBucket, activeModel string, warnPct int) (string, float64) {
	if len(buckets) == 0 {
		return "", -1
	}
	if warnPct <= 0 {
		warnPct = 20
	}

	// 1. If the window has an active model, prefer that model family's active 5h bucket (then any active bucket of that family).
	if strings.TrimSpace(activeModel) != "" {
		var matchedAny *rpc.QuotaBucket
		for i := range buckets {
			b := &buckets[i]
			if b.Disabled {
				continue
			}
			grp := rpc.LocalizeBucketGroup(b.GroupName, b.DisplayName, b.BucketID)
			if !modelMatchesBucketGroup(activeModel, grp) {
				continue
			}
			shortWin, _, canonWin := rpc.LocalizeQuotaWindow(b.Window, b.DisplayName, b.BucketID)
			if canonWin == "5h" {
				return shortWin, b.RemainingPct
			}
			if matchedAny == nil {
				matchedAny = b
			}
		}
		if matchedAny != nil {
			shortWin, _, _ := rpc.LocalizeQuotaWindow(matchedAny.Window, matchedAny.DisplayName, matchedAny.BucketID)
			return shortWin, matchedAny.RemainingPct
		}
	}

	// 2. Otherwise prefer the tightest active 5h bucket if below warnPct, or the first active 5h bucket.
	var first5h *rpc.QuotaBucket
	var lowest5h *rpc.QuotaBucket
	var lowestAll *rpc.QuotaBucket
	for i := range buckets {
		b := &buckets[i]
		if b.Disabled {
			continue
		}
		if lowestAll == nil || b.RemainingPct < lowestAll.RemainingPct {
			lowestAll = b
		}
		_, _, canonWin := rpc.LocalizeQuotaWindow(b.Window, b.DisplayName, b.BucketID)
		if canonWin == "5h" {
			if first5h == nil {
				first5h = b
			}
			if lowest5h == nil || b.RemainingPct < lowest5h.RemainingPct {
				lowest5h = b
			}
		}
	}
	if lowest5h != nil && lowest5h.RemainingPct <= float64(warnPct) {
		return "5小时", lowest5h.RemainingPct
	}
	if first5h != nil {
		return "5小时", first5h.RemainingPct
	}
	chosen := lowestAll
	if chosen == nil {
		chosen = &buckets[0]
	}
	shortWin, _, _ := rpc.LocalizeQuotaWindow(chosen.Window, chosen.DisplayName, chosen.BucketID)
	return shortWin, chosen.RemainingPct
}

func pickTopBurnHintForHUD(buckets []rpc.QuotaBucket) string {
	var exhausted *rpc.QuotaBucket
	var fastestBurn *rpc.QuotaBucket
	for i := range buckets {
		b := &buckets[i]
		if b.Disabled {
			continue
		}
		if b.RemainingPct <= 0.05 && b.ResetUnix > 0 {
			if exhausted == nil {
				exhausted = b
			}
			continue
		}
		if b.ExhaustsBeforeReset && b.HoursUntilEmpty > 0 {
			if fastestBurn == nil || b.HoursUntilEmpty < fastestBurn.HoursUntilEmpty {
				fastestBurn = b
			}
		}
	}
	if exhausted != nil {
		name := rpc.FormatBucketDisplayName(exhausted.GroupName, exhausted.DisplayName, exhausted.Window, exhausted.BucketID)
		resetLeft := cdp.FormatResetCountdown(exhausted.ResetUnix)
		if resetLeft != "" && resetLeft != "即将重置" {
			return fmt.Sprintf("%s已耗尽（%s后重置）", name, resetLeft)
		}
		return fmt.Sprintf("%s已耗尽，等待重置", name)
	}
	if fastestBurn != nil {
		name := rpc.FormatBucketDisplayName(fastestBurn.GroupName, fastestBurn.DisplayName, fastestBurn.Window, fastestBurn.BucketID)
		return fmt.Sprintf("%s预计 %s耗尽（%.0f%%/小时）", name, formatHoursCN(fastestBurn.HoursUntilEmpty), fastestBurn.BurnPctPerHour)
	}
	return ""
}
