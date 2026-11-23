// Package hub: account-grouped overview snapshots, ROI baseline, and menu bar summary.
package hub

import (
	"fmt"
	"github.com/realchendahuang/AgyMate/internal/cdp"
	"github.com/realchendahuang/AgyMate/internal/config"
	"github.com/realchendahuang/AgyMate/internal/instances"
	"github.com/realchendahuang/AgyMate/internal/rpc"
	"github.com/realchendahuang/AgyMate/internal/telemetry"
	"github.com/realchendahuang/AgyMate/internal/version"
	"net/http"
	"sort"
	"strings"
	"time"
)

// BuildOverview composes a complete OverviewPayload snapshot.
func (s *Server) BuildOverview() OverviewPayload {
	cfg := s.Config()
	s.mu.RLock()
	instList := make([]instances.Instance, len(s.instList))
	copy(instList, s.instList)
	snaps := make(map[string]rpc.InstanceSnapshot, len(s.snapshots))
	for k, v := range s.snapshots {
		snaps[k] = v
	}
	todayRep := s.todayRep
	instToday := make(map[string]telemetry.InstAggregate, len(s.instToday))
	for k, v := range s.instToday {
		instToday[k] = v
	}
	docRep := s.doctorRep
	updatedAt := s.lastPollAt
	pollMs := s.lastPollMs
	s.mu.RUnlock()

	liveSessions := s.tracker.Snapshot()
	cdpTargets := s.cdpWatcher.LastStatus()
	cdpByInst := make(map[string][]cdp.TargetStatus)
	for _, t := range cdpTargets {
		cdpByInst[t.InstanceID] = append(cdpByInst[t.InstanceID], t)
	}

	views := make([]InstanceView, 0, len(instList))
	for _, inst := range instList {
		if strings.TrimSpace(inst.Account) == "" {
			inst.Account = s.state.LastInstanceAccount(inst.ID)
		}
		iv := InstanceView{
			Instance:   inst,
			LiveState:  "offline",
			CDPTargets: cdpByInst[inst.ID],
		}
		if snap, ok := snaps[inst.ID]; ok {
			snapCopy := snap
			iv.Snapshot = &snapCopy
			if iv.Account == "" && strings.TrimSpace(snapCopy.Email) != "" {
				iv.Account = strings.TrimSpace(snapCopy.Email)
			}
		}
		if ag, ok := instToday[inst.ID]; ok {
			iv.TodayCalls = ag.Calls
			iv.TodayTokens = ag.Total
			iv.TodayUSD = ag.TotalUSD
			iv.TodayCacheHitRate = ag.CacheRate
		}
		if inst.Running {
			iv.LiveState = "idle"
			for _, ls := range liveSessions {
				if ls.InstanceID == inst.ID && (ls.State == "running" || ls.State == "waiting_approval") {
					iv.ActiveSessions++
					if ls.State == "waiting_approval" {
						iv.LiveState = "waiting_approval"
					} else if iv.LiveState != "waiting_approval" {
						iv.LiveState = "running"
					}
				}
			}
			if iv.Snapshot != nil && iv.Snapshot.RunningCount > iv.ActiveSessions {
				iv.ActiveSessions = iv.Snapshot.RunningCount
			}
			if iv.ActiveSessions > 0 && iv.LiveState == "idle" {
				iv.LiveState = "running"
			}
		}
		views = append(views, iv)
	}

	now := time.Now()
	accounts := buildAccountViews(views, instToday, s.state, now)
	colabSnap := s.colabCollector.Last()

	return OverviewPayload{
		Version:      version.Version,
		Language:     config.NormalizeLanguage(cfg.Language),
		UpdatedAt:    updatedAt,
		PollMs:       pollMs,
		Pricing:      cfg.Pricing,
		CDPZhEnabled: cfg.CDP.Enabled && cfg.CDP.ZhInject,
		DoctorCounts: map[string]int{
			"ok":    docRep.OKCount,
			"warn":  docRep.WarnCount,
			"error": docRep.ErrorCount,
		},
		Accounts:      accounts,
		Instances:     views,
		TodaySummary:  todayRep.Summary,
		BurnRate15m:   todayRep.BurnRate15m,
		MonthROI:      todayRep.MonthROI,
		LiveSessions:  liveSessions,
		CDPTargets:    cdpTargets,
		Colab:         colabSnap,
		ColabAccounts: colabSnap.Accounts,
	}
}

// MenuBarSummary builds a compact macOS Menu Bar label + tooltip from the current state.
func (s *Server) MenuBarSummary() (string, string) {
	ov := s.BuildOverview()
	quotaStr := "--%"
	fallbackQuota := ""
	for _, acct := range ov.Accounts {
		if len(acct.Buckets) == 0 {
			continue
		}
		picked := acct.Buckets[0]
		for _, b := range acct.Buckets {
			if !b.Disabled {
				picked = b
				break
			}
		}
		pctStr := fmt.Sprintf("%.0f%%", picked.RemainingPct)
		if acct.OnlineCount > 0 {
			quotaStr = pctStr
			break
		}
		if fallbackQuota == "" {
			fallbackQuota = pctStr
		}
	}
	if quotaStr == "--%" && fallbackQuota != "" {
		quotaStr = fallbackQuota
	}

	var tipParts []string
	for _, iv := range ov.Instances {
		if iv.Running {
			tipParts = append(tipParts, fmt.Sprintf("%s(%s)", firstNonEmpty(iv.Label, iv.ID), localizeLiveState(iv.LiveState)))
		}
	}
	label := fmt.Sprintf("配额 %s · %s", quotaStr, formatMoney(ov.TodaySummary.TotalUSD, ov.Pricing))
	tooltip := "AgyMate"
	if len(tipParts) > 0 {
		tooltip += " | 在线: " + strings.Join(tipParts, ", ")
	}
	if ov.Colab.OK {
		if ov.Colab.AssignmentsCount > 0 || ov.Colab.ConsumptionRateHourly > 0.01 {
			label += fmt.Sprintf(" · 🔥%d台", ov.Colab.AssignmentsCount)
			tooltip += fmt.Sprintf(" | Colab: 🔥%d台 %.2f CCU/小时", ov.Colab.AssignmentsCount, ov.Colab.ConsumptionRateHourly)
		} else {
			tooltip += fmt.Sprintf(" | Colab: %.0f CCU", ov.Colab.CurrentBalance)
		}
	}
	return label, tooltip
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("refresh") == "1" {
		s.PollOnceWithForce(r.Context(), true)
	} else {
		s.waitBootPoll(r.Context())
	}
	writeJSON(w, http.StatusOK, s.BuildOverview())
}

func toCachedBuckets(buckets []rpc.QuotaBucket) []config.CachedBucket {
	if len(buckets) == 0 {
		return nil
	}
	out := make([]config.CachedBucket, len(buckets))
	for i, b := range buckets {
		out[i] = config.CachedBucket{
			GroupName:         b.GroupName,
			BucketID:          b.BucketID,
			DisplayName:       b.DisplayName,
			Description:       b.Description,
			Window:            b.Window,
			Disabled:          b.Disabled,
			RemainingFraction: b.RemainingFraction,
			RemainingPct:      b.RemainingPct,
			ResetTime:         b.ResetTime,
			ResetUnix:         b.ResetUnix,
		}
	}
	return out
}

func fromCachedBuckets(cached []config.CachedBucket) []rpc.QuotaBucket {
	if len(cached) == 0 {
		return nil
	}
	out := make([]rpc.QuotaBucket, len(cached))
	for i, b := range cached {
		out[i] = rpc.QuotaBucket{
			GroupName:         b.GroupName,
			BucketID:          b.BucketID,
			DisplayName:       rpc.FormatBucketDisplayName(b.GroupName, b.DisplayName, b.Window, b.BucketID),
			Description:       b.Description,
			Window:            b.Window,
			Disabled:          b.Disabled,
			RemainingFraction: b.RemainingFraction,
			RemainingPct:      b.RemainingPct,
			ResetTime:         b.ResetTime,
			ResetUnix:         b.ResetUnix,
			HoursUntilEmpty:   -1,
		}
	}
	return out
}

func liveStateRank(state string) int {
	switch state {
	case "waiting_approval":
		return 3
	case "running":
		return 2
	case "idle":
		return 1
	default:
		return 0
	}
}

func buildAccountViews(views []InstanceView, instToday map[string]telemetry.InstAggregate, st *config.PersistentState, now time.Time) []AccountView {
	type acctGroup struct {
		view     AccountView
		sumIn    int64
		sumCache int64
		fallback float64
		bestRank int
	}

	groupMap := make(map[string]*acctGroup, len(views))
	order := make([]string, 0, len(views))

	for _, iv := range views {
		email := strings.TrimSpace(iv.Account)
		if email == "" && iv.Snapshot != nil {
			email = strings.TrimSpace(iv.Snapshot.Email)
		}
		key := iv.ID
		display := firstNonEmpty(iv.Label, iv.ID)
		if email != "" {
			key = "acct:" + strings.ToLower(email)
			display = email
		}

		grp := groupMap[key]
		if grp == nil {
			grp = &acctGroup{
				view: AccountView{
					Key:         key,
					Email:       email,
					DisplayName: display,
					LiveState:   "offline",
				},
			}
			groupMap[key] = grp
			order = append(order, key)
		}
		if grp.view.Email == "" && email != "" {
			grp.view.Email = email
			grp.view.DisplayName = email
		}

		grp.view.Instances = append(grp.view.Instances, iv)
		grp.view.TotalSlots++
		if iv.Running {
			grp.view.OnlineCount++
		}
		grp.view.ActiveSessions += iv.ActiveSessions
		if rank := liveStateRank(iv.LiveState); rank > grp.bestRank {
			grp.bestRank = rank
			grp.view.LiveState = iv.LiveState
		}

		grp.view.TodayCalls += iv.TodayCalls
		grp.view.TodayTokens += iv.TodayTokens
		grp.view.TodayUSD += iv.TodayUSD
		if ag, ok := instToday[iv.ID]; ok {
			grp.sumIn += ag.Input
			grp.sumCache += ag.Cache
			if ag.CacheRate > grp.fallback {
				grp.fallback = ag.CacheRate
			}
		} else if iv.TodayCacheHitRate > grp.fallback {
			grp.fallback = iv.TodayCacheHitRate
		}
	}

	accounts := make([]AccountView, 0, len(order))
	for _, key := range order {
		grp := groupMap[key]
		av := &grp.view
		if den := grp.sumIn + grp.sumCache; den > 0 {
			av.TodayCacheHitRate = float64(grp.sumCache) * 100.0 / float64(den)
		} else if grp.fallback > 0 {
			av.TodayCacheHitRate = grp.fallback
		}

		// Populate plan/buckets/context from live online slots (preferring running slot with buckets).
		var bestCtx *rpc.ContextWindow
		for _, iv := range av.Instances {
			if iv.Snapshot == nil {
				continue
			}
			snap := iv.Snapshot
			if av.UserName == "" && snap.UserName != "" {
				av.UserName = snap.UserName
			}
			if av.PlanName == "" && snap.PlanName != "" {
				av.PlanName = snap.PlanName
			}
			if av.TierID == "" && snap.TierID != "" {
				av.TierID = snap.TierID
			}
			if len(snap.Buckets) > 0 && (len(av.Buckets) == 0 || iv.Running) {
				av.Buckets = append([]rpc.QuotaBucket(nil), snap.Buckets...)
				av.QuotaUpdatedAt = snap.CollectedAt
			}
			if snap.Context != nil {
				if bestCtx == nil || (snap.Context.Running && !bestCtx.Running) || (snap.Context.Running == bestCtx.Running && snap.Context.UsagePct > bestCtx.UsagePct) {
					cCopy := *snap.Context
					bestCtx = &cCopy
				}
			}
		}
		av.Context = bestCtx

		// Hydrate offline account plan & quota from PersistentState (with expired reset rollover).
		if av.Email != "" && st != nil {
			if cached, ok := st.GetAccountSnapshot(av.Email, now); ok {
				if av.UserName == "" {
					av.UserName = cached.UserName
				}
				if av.PlanName == "" {
					av.PlanName = cached.PlanName
				}
				if av.TierID == "" {
					av.TierID = cached.TierID
				}
				if len(av.Buckets) == 0 && len(cached.Buckets) > 0 {
					av.Buckets = fromCachedBuckets(cached.Buckets)
					av.CachedQuota = true
					av.QuotaUpdatedAt = cached.UpdatedAt
				}
			}
		}

		accounts = append(accounts, *av)
	}

	sort.Slice(accounts, func(i, j int) bool {
		ri, rj := liveStateRank(accounts[i].LiveState), liveStateRank(accounts[j].LiveState)
		if ri != rj {
			return ri > rj
		}
		if accounts[i].TodayUSD != accounts[j].TodayUSD {
			return accounts[i].TodayUSD > accounts[j].TodayUSD
		}
		if accounts[i].OnlineCount != accounts[j].OnlineCount {
			return accounts[i].OnlineCount > accounts[j].OnlineCount
		}
		return strings.ToLower(accounts[i].DisplayName) < strings.ToLower(accounts[j].DisplayName)
	})
	return accounts
}

func tierMonthlyUSD(planName, tierID string) float64 {
	plan := strings.ToLower(planName + " " + tierID)
	switch {
	case strings.Contains(plan, "ultra"):
		return 200.0
	case strings.Contains(plan, "pro"):
		return 20.0
	default:
		return 0.0
	}
}

func effectiveMonthlySubUSD(cfg config.Config, snaps map[string]rpc.InstanceSnapshot) float64 {
	return effectiveMonthlySubUSDWithState(cfg, snaps, nil, time.Now())
}

func effectiveMonthlySubUSDWithState(cfg config.Config, snaps map[string]rpc.InstanceSnapshot, st *config.PersistentState, now time.Time) float64 {
	if cfg.Pricing.UltraMonthlyUSD > 0 {
		return cfg.Pricing.UltraMonthlyUSD
	}
	byAccount := make(map[string]float64)

	ids := make([]string, 0, len(snaps))
	for id := range snaps {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		snap := snaps[id]
		usd := tierMonthlyUSD(snap.PlanName, snap.TierID)
		key := strings.ToLower(strings.TrimSpace(snap.Email))
		if key == "" {
			key = "inst:" + id
		}
		if usd > byAccount[key] {
			byAccount[key] = usd
		}
	}

	if st != nil {
		for k, cached := range st.AllAccountSnapshots(now) {
			emailKey := strings.ToLower(strings.TrimSpace(firstNonEmpty(cached.Email, k)))
			if emailKey == "" {
				continue
			}
			usd := tierMonthlyUSD(cached.PlanName, cached.TierID)
			if usd > byAccount[emailKey] {
				byAccount[emailKey] = usd
			}
		}
	}

	var total float64
	for _, usd := range byAccount {
		total += usd
	}
	return total
}

func (s *Server) effectiveMonthlySubUSD(cfg config.Config) float64 {
	if cfg.Pricing.UltraMonthlyUSD > 0 {
		return cfg.Pricing.UltraMonthlyUSD
	}
	s.mu.RLock()
	snaps := make(map[string]rpc.InstanceSnapshot, len(s.snapshots))
	for k, v := range s.snapshots {
		snaps[k] = v
	}
	s.mu.RUnlock()
	return effectiveMonthlySubUSDWithState(cfg, snaps, s.state, time.Now())
}

func formatMoney(usd float64, pricing config.PricingConfig) string {
	if strings.EqualFold(pricing.Currency, "CNY") {
		rate := pricing.USDToCNY
		if rate <= 0 {
			rate = 7.20
		}
		return fmt.Sprintf("¥%.2f", usd*rate)
	}
	if usd >= 100 {
		return fmt.Sprintf("$%.1f", usd)
	}
	return fmt.Sprintf("$%.2f", usd)
}

func localizeLiveState(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "waiting_approval", "waiting":
		return "待确认"
	case "running":
		return "运行中"
	case "offline":
		return "离线"
	default:
		return "空闲"
	}
}

func localizePlanName(planName, tierID string) string {
	raw := strings.TrimSpace(planName)
	probe := strings.ToLower(raw + " " + tierID)
	switch {
	case strings.Contains(probe, "ultra"):
		return "Ultra 套餐"
	case strings.Contains(probe, "pro"):
		return "Pro 套餐"
	case strings.Contains(probe, "team") || strings.Contains(probe, "enterprise") || strings.Contains(probe, "business"):
		return "团队套餐"
	case strings.Contains(probe, "free") || strings.Contains(probe, "basic"):
		return "免费版"
	default:
		return raw
	}
}
