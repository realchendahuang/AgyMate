// Package colab collects Google Colab CCU balance, burn rate, eligible accelerators,
// and active VM sessions by delegating safely to ~/.local/bin/colab-safe --json.
package colab

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/realchendahuang/AgyMate/internal/config"
)

const (
	ccuRetailUSD      = 0.10 // $10 per 100 CCU PayG reference valuation
	defaultAllowance  = 2000.0
	burnAlertCooldown = int64(1800) // 30 minutes
)

// SessionItem describes one server-side active Colab runtime assignment.
type SessionItem struct {
	Name        string `json:"name"`
	Orphan      bool   `json:"orphan"`
	Endpoint    string `json:"endpoint"`
	Accelerator string `json:"accelerator"`
	Variant     string `json:"variant"`
	Shape       string `json:"shape"`
	Account     string `json:"account,omitempty"`
}

// Snapshot holds Colab compute radar state (aggregated pool and optional per-account list).
type Snapshot struct {
	Enabled               bool          `json:"enabled"`
	Installed             bool          `json:"installed"`
	OK                    bool          `json:"ok"`
	Status                string        `json:"status"` // authenticated | unauthenticated | missing_cli | disabled | empty | error
	Account               string        `json:"account,omitempty"`
	Source                string        `json:"source,omitempty"`    // oauth | colab-safe | pool
	Removable             bool          `json:"removable,omitempty"` // whether UI can unbind/hide this entry
	CurrentBalance        float64       `json:"currentBalance"`
	EstimatedValueUSD     float64       `json:"estimatedValueUsd"`
	MonthlyAllowance      float64       `json:"monthlyAllowance"`
	ConsumptionRateHourly float64       `json:"consumptionRateHourly"`
	BurnUSDPerHour        float64       `json:"burnUsdPerHour"`
	HoursUntilEmpty       float64       `json:"hoursUntilEmpty"`
	AssignmentsCount      int           `json:"assignmentsCount"`
	OrphanCount           int           `json:"orphanCount"`
	ActiveSince           int64         `json:"activeSince"`
	ActiveMinutes         int           `json:"activeMinutes"`
	EligibleGPUs          []string      `json:"eligibleGpus,omitempty"`
	EligibleTPUs          []string      `json:"eligibleTpus,omitempty"`
	Sessions              []SessionItem `json:"sessions,omitempty"`
	Accounts              []Snapshot    `json:"accounts,omitempty"`
	CheckedAt             int64         `json:"checkedAt"`
	FetchMs               float64       `json:"fetchMs"`
	LastError             string        `json:"lastError,omitempty"`
}

// AccountAlert reports Telegram alert triggers for one Colab account.
type AccountAlert struct {
	Account       string
	BurnMsgNeeded bool
	LowMsgNeeded  bool
	Snapshot      Snapshot
}

// NukeResult represents the structured output from terminating Colab VMs.
type NukeResult struct {
	OK                    bool          `json:"ok"`
	TerminatedCount       int           `json:"terminatedCount"`
	Terminated            []SessionItem `json:"terminated,omitempty"`
	RemainingCount        int           `json:"remainingCount"`
	FailedCount           int           `json:"failedCount"`
	CurrentBalance        float64       `json:"currentBalance"`
	ConsumptionRateHourly float64       `json:"consumptionRateHourly"`
	CheckedAt             int64         `json:"checkedAt"`
	Error                 string        `json:"error,omitempty"`
}

type doctorJSON struct {
	OK                    bool          `json:"ok"`
	Status                string        `json:"status"`
	Account               string        `json:"account"`
	CurrentBalance        float64       `json:"currentBalance"`
	ConsumptionRateHourly float64       `json:"consumptionRateHourly"`
	AssignmentsCount      int           `json:"assignmentsCount"`
	EligibleGPUs          []string      `json:"eligibleGpus"`
	EligibleTPUs          []string      `json:"eligibleTpus"`
	Sessions              []SessionItem `json:"sessions"`
	CheckedAt             int64         `json:"checkedAt"`
	Error                 string        `json:"error"`
}

type acctTrackState struct {
	activeSince     int64
	lastBurnAlertAt int64
	lowAlertSent    bool
}

// Collector caches Colab telemetry across fast Hub ticks, manages multi-account OAuth tokens,
// and optionally incorporates ~/.local/bin/colab-safe.
type Collector struct {
	probeMu           sync.Mutex
	mu                sync.Mutex
	last              Snapshot
	acctStates        map[string]*acctTrackState
	nowFn             func() time.Time
	oauthMu           sync.Mutex
	accountsMu        sync.Mutex
	pendingFlows      map[string]pendingOAuthFlow
	httpClient        *http.Client
	authURL           string
	tokenURL          string
	userInfoURL       string
	colabBaseURL      string
	storePathOverride string
	isHiddenFn        func(string) bool
}

// NewCollector constructs a thread-safe multi-account Colab radar collector.
func NewCollector() *Collector {
	return &Collector{
		last: Snapshot{
			Status:          "disabled",
			HoursUntilEmpty: -1,
		},
		acctStates:   make(map[string]*acctTrackState),
		pendingFlows: make(map[string]pendingOAuthFlow),
		httpClient:   &http.Client{Timeout: 12 * time.Second},
	}
}

// SetHiddenChecker wires an optional predicate that filters out hidden external (colab-safe) accounts.
func (c *Collector) SetHiddenChecker(fn func(string) bool) {
	c.mu.Lock()
	c.isHiddenFn = fn
	c.mu.Unlock()
}

func (c *Collector) isHidden(email string) bool {
	c.mu.Lock()
	fn := c.isHiddenFn
	c.mu.Unlock()
	if fn == nil {
		return false
	}
	return fn(email)
}

func (c *Collector) now() time.Time {
	if c.nowFn != nil {
		return c.nowFn()
	}
	return time.Now()
}

func (c *Collector) trackStateLocked(key string) *acctTrackState {
	if c.acctStates == nil {
		c.acctStates = make(map[string]*acctTrackState)
	}
	st := c.acctStates[key]
	if st == nil {
		st = &acctTrackState{}
		c.acctStates[key] = st
	}
	return st
}

func accountTrackKey(account string) string {
	low := strings.ToLower(strings.TrimSpace(account))
	if low == "" {
		return "_cli"
	}
	return low
}

// Last returns a snapshot copy with up-to-date ActiveMinutes if a cloud VM is running.
func (c *Collector) Last() Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snapshotWithElapsedLocked(c.now().Unix())
}

func (c *Collector) snapshotWithElapsedLocked(nowUnix int64) Snapshot {
	snap := cloneSnapshot(c.last)
	if snap.ActiveSince > 0 && nowUnix >= snap.ActiveSince {
		snap.ActiveMinutes = int((nowUnix - snap.ActiveSince) / 60)
	}
	for i := range snap.Accounts {
		if snap.Accounts[i].ActiveSince > 0 && nowUnix >= snap.Accounts[i].ActiveSince {
			snap.Accounts[i].ActiveMinutes = int((nowUnix - snap.Accounts[i].ActiveSince) / 60)
		}
	}
	return snap
}

// Probe fetches the latest Colab status across OAuth-bound accounts and colab-safe CLI,
// honoring PollIntervalSec unless force is true.
func (c *Collector) Probe(ctx context.Context, cfg config.ColabConfig, force bool) Snapshot {
	c.probeMu.Lock()
	defer c.probeMu.Unlock()
	return c.probeLocked(ctx, cfg, force)
}

func (c *Collector) probeLocked(ctx context.Context, cfg config.ColabConfig, force bool) Snapshot {
	now := c.now()
	nowUnix := now.Unix()
	allowance := cfg.MonthlyAllowance
	if allowance <= 0 {
		allowance = defaultAllowance
	}

	if !cfg.Enabled {
		c.mu.Lock()
		c.acctStates = make(map[string]*acctTrackState)
		c.last = Snapshot{
			Enabled:          false,
			Status:           "disabled",
			MonthlyAllowance: allowance,
			HoursUntilEmpty:  -1,
			CheckedAt:        nowUnix,
		}
		snap := cloneSnapshot(c.last)
		c.mu.Unlock()
		return snap
	}

	pollSec := cfg.PollIntervalSec
	if pollSec < 15 {
		pollSec = 60
	}

	c.mu.Lock()
	if !force && c.last.CheckedAt > 0 && nowUnix-c.last.CheckedAt < int64(pollSec) {
		cached := c.snapshotWithElapsedLocked(nowUnix)
		cached.Enabled = true
		if len(cached.Accounts) <= 1 {
			cached.MonthlyAllowance = allowance
		}
		c.mu.Unlock()
		return cached
	}
	c.mu.Unlock()

	t0 := time.Now()
	oauthAccs, loadErr := c.loadAccounts()
	cliPath, cliOK := resolveCLIPath(cfg.CLIPath)

	if len(oauthAccs) == 0 && !cliOK {
		errMsg := fmt.Sprintf("未找到可执行编排器: %s", cfg.CLIPath)
		if loadErr != nil {
			errMsg = loadErr.Error()
		}
		snap := Snapshot{
			Enabled:          true,
			Installed:        false,
			OK:               false,
			Status:           "missing_cli",
			MonthlyAllowance: allowance,
			HoursUntilEmpty:  -1,
			CheckedAt:        nowUnix,
			FetchMs:          elapsedMs(t0),
			LastError:        errMsg,
		}
		c.mu.Lock()
		c.last = cloneSnapshot(snap)
		c.mu.Unlock()
		return snap
	}

	// Probe all OAuth-bound accounts and optional colab-safe CLI concurrently.
	oauthResults := make([]Snapshot, len(oauthAccs))
	var cliResult *Snapshot
	var wg sync.WaitGroup

	for i, acc := range oauthAccs {
		wg.Add(1)
		go func(idx int, account StoredAccount) {
			defer wg.Done()
			oauthResults[idx] = c.probeOAuthAccount(ctx, allowance, account)
		}(i, acc)
	}

	if cliOK {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := c.probeCLIAccount(ctx, cliPath, allowance, nowUnix)
			cliResult = &s
		}()
	}

	wg.Wait()

	oauthEmails := make(map[string]bool, len(oauthResults))
	accounts := make([]Snapshot, 0, len(oauthResults)+1)
	for _, s := range oauthResults {
		if low := strings.ToLower(strings.TrimSpace(s.Account)); low != "" {
			oauthEmails[low] = true
		}
		accounts = append(accounts, s)
	}

	if cliResult != nil {
		lowCLI := strings.ToLower(strings.TrimSpace(cliResult.Account))
		duplicateOAuth := lowCLI != "" && oauthEmails[lowCLI]
		hiddenCLI := lowCLI != "" && c.isHidden(lowCLI)
		unauthShadow := len(accounts) > 0 && !cliResult.OK && (cliResult.Status == "unauthenticated" || lowCLI == "")
		if !duplicateOAuth && !hiddenCLI && !unauthShadow {
			accounts = append(accounts, *cliResult)
		}
	}

	// Update per-account active VM tracking under c.mu.
	c.mu.Lock()
	for i := range accounts {
		s := &accounts[i]
		key := accountTrackKey(s.Account)
		st := c.trackStateLocked(key)
		if s.OK {
			burning := s.AssignmentsCount > 0 || len(s.Sessions) > 0 || s.ConsumptionRateHourly > 0.01
			if burning {
				if st.activeSince == 0 {
					st.activeSince = nowUnix
				}
				s.ActiveSince = st.activeSince
				if nowUnix >= st.activeSince {
					s.ActiveMinutes = int((nowUnix - st.activeSince) / 60)
				}
			} else {
				st.activeSince = 0
				st.lastBurnAlertAt = 0
				s.ActiveSince = 0
				s.ActiveMinutes = 0
			}
		} else if st.activeSince > 0 {
			if s.Status == "unauthenticated" {
				st.activeSince = 0
				st.lastBurnAlertAt = 0
			} else {
				s.ActiveSince = st.activeSince
				if nowUnix >= st.activeSince {
					s.ActiveMinutes = int((nowUnix - st.activeSince) / 60)
				}
			}
		}
	}

	agg := aggregateSnapshots(accounts, allowance, nowUnix, elapsedMs(t0))
	c.last = cloneSnapshot(agg)
	c.mu.Unlock()

	return agg
}

func (c *Collector) probeCLIAccount(ctx context.Context, cliPath string, allowance float64, nowUnix int64) Snapshot {
	t0 := time.Now()
	stdout, stderr, runErr := runCLICommand(ctx, cliPath, []string{"doctor", "--json"}, 12*time.Second)
	snap := Snapshot{
		Enabled:          true,
		Installed:        true,
		MonthlyAllowance: allowance,
		HoursUntilEmpty:  -1,
		Source:           "colab-safe",
		Removable:        c.isHiddenFn != nil,
		CheckedAt:        nowUnix,
		FetchMs:          elapsedMs(t0),
	}

	var parsed doctorJSON
	if jerr := json.Unmarshal([]byte(stdout), &parsed); jerr == nil && parsed.Status != "" {
		snap.OK = parsed.OK && runErr == nil
		snap.Status = parsed.Status
		snap.Account = parsed.Account
		snap.CurrentBalance = parsed.CurrentBalance
		snap.EstimatedValueUSD = parsed.CurrentBalance * ccuRetailUSD
		snap.ConsumptionRateHourly = parsed.ConsumptionRateHourly
		snap.BurnUSDPerHour = parsed.ConsumptionRateHourly * ccuRetailUSD
		if parsed.ConsumptionRateHourly > 0.01 && parsed.CurrentBalance > 0 {
			snap.HoursUntilEmpty = parsed.CurrentBalance / parsed.ConsumptionRateHourly
		}
		snap.AssignmentsCount = maxInt(parsed.AssignmentsCount, len(parsed.Sessions))
		snap.EligibleGPUs = append([]string(nil), parsed.EligibleGPUs...)
		snap.EligibleTPUs = append([]string(nil), parsed.EligibleTPUs...)
		snap.Sessions = make([]SessionItem, 0, len(parsed.Sessions))
		for _, sess := range parsed.Sessions {
			if sess.Account == "" {
				sess.Account = parsed.Account
			}
			if sess.Orphan {
				snap.OrphanCount++
			}
			snap.Sessions = append(snap.Sessions, sess)
		}
		if parsed.CheckedAt > 0 {
			snap.CheckedAt = parsed.CheckedAt
		}
		if parsed.Error != "" {
			snap.LastError = parsed.Error
		} else if runErr != nil {
			snap.OK = false
			snap.LastError = summarizeExecErr(runErr, stderr)
		}
	} else {
		snap.OK = false
		snap.Status = "error"
		if runErr != nil {
			snap.LastError = summarizeExecErr(runErr, stderr)
		} else {
			snap.LastError = fmt.Sprintf("解析 colab-safe doctor --json 输出失败: %v", jerr)
		}
	}
	return snap
}

func aggregateSnapshots(accounts []Snapshot, allowance float64, nowUnix int64, fetchMs float64) Snapshot {
	if len(accounts) == 0 {
		return Snapshot{
			Enabled:          true,
			Installed:        true,
			OK:               true,
			Status:           "empty",
			Source:           "pool",
			MonthlyAllowance: allowance,
			HoursUntilEmpty:  -1,
			Accounts:         []Snapshot{},
			CheckedAt:        nowUnix,
			FetchMs:          fetchMs,
		}
	}
	if len(accounts) == 1 {
		top := cloneSnapshot(accounts[0])
		top.Accounts = []Snapshot{cloneSnapshot(accounts[0])}
		return top
	}

	agg := Snapshot{
		Enabled:         true,
		Installed:       true,
		Status:          accounts[0].Status,
		Source:          "pool",
		HoursUntilEmpty: -1,
		CheckedAt:       nowUnix,
		FetchMs:         fetchMs,
		Accounts:        make([]Snapshot, 0, len(accounts)),
	}

	seenGPU := make(map[string]bool)
	seenTPU := make(map[string]bool)
	var firstErr string
	okCount := 0

	for _, a := range accounts {
		agg.Accounts = append(agg.Accounts, cloneSnapshot(a))
		if a.CheckedAt > agg.CheckedAt {
			agg.CheckedAt = a.CheckedAt
		}
		if a.LastError != "" && firstErr == "" {
			firstErr = a.LastError
		}
		if !a.OK {
			continue
		}
		okCount++
		if !agg.OK {
			agg.OK = true
			agg.Status = "authenticated"
			agg.Account = a.Account
		}
		agg.CurrentBalance += a.CurrentBalance
		agg.EstimatedValueUSD += a.EstimatedValueUSD
		agg.MonthlyAllowance += a.MonthlyAllowance
		agg.ConsumptionRateHourly += a.ConsumptionRateHourly
		agg.BurnUSDPerHour += a.BurnUSDPerHour
		agg.AssignmentsCount += a.AssignmentsCount
		agg.OrphanCount += a.OrphanCount
		agg.Sessions = append(agg.Sessions, a.Sessions...)
		for _, g := range a.EligibleGPUs {
			if !seenGPU[g] {
				seenGPU[g] = true
				agg.EligibleGPUs = append(agg.EligibleGPUs, g)
			}
		}
		for _, t := range a.EligibleTPUs {
			if !seenTPU[t] {
				seenTPU[t] = true
				agg.EligibleTPUs = append(agg.EligibleTPUs, t)
			}
		}
		if a.ActiveSince > 0 && (agg.ActiveSince == 0 || a.ActiveSince < agg.ActiveSince) {
			agg.ActiveSince = a.ActiveSince
		}
		if a.ActiveMinutes > agg.ActiveMinutes {
			agg.ActiveMinutes = a.ActiveMinutes
		}
	}

	if okCount == 0 {
		agg.Account = accounts[0].Account
		agg.MonthlyAllowance = allowance
		agg.LastError = firstErr
	} else if agg.ConsumptionRateHourly > 0.01 && agg.CurrentBalance > 0 {
		agg.HoursUntilEmpty = agg.CurrentBalance / agg.ConsumptionRateHourly
	}
	return agg
}

// Nuke executes teardown of all active server-side VMs across bound Colab accounts and refreshes state.
func (c *Collector) Nuke(ctx context.Context, cfg config.ColabConfig) (NukeResult, Snapshot, error) {
	return c.NukeAccount(ctx, cfg, "")
}

// NukeAccount tears down active VMs for targetAccount (or all accounts when targetAccount is empty).
func (c *Collector) NukeAccount(ctx context.Context, cfg config.ColabConfig, targetAccount string) (NukeResult, Snapshot, error) {
	c.probeMu.Lock()
	defer c.probeMu.Unlock()

	target := strings.ToLower(strings.TrimSpace(targetAccount))
	oauthAccs, _ := c.loadAccounts()
	cliPath, cliOK := resolveCLIPath(cfg.CLIPath)

	// Remember currently known CLI account to avoid double-nuking or hitting hidden accounts.
	var knownCLIAccount string
	c.mu.Lock()
	for _, a := range c.last.Accounts {
		if a.Source == "colab-safe" && a.Account != "" {
			knownCLIAccount = strings.ToLower(strings.TrimSpace(a.Account))
			break
		}
	}
	if knownCLIAccount == "" && c.last.Source == "colab-safe" {
		knownCLIAccount = strings.ToLower(strings.TrimSpace(c.last.Account))
	}
	c.mu.Unlock()

	if target != "" {
		for _, acc := range oauthAccs {
			if strings.EqualFold(strings.TrimSpace(acc.Email), target) {
				res, nerr := c.nukeOAuthAccount(ctx, acc)
				if res.OK && res.RemainingCount == 0 && res.FailedCount == 0 {
					c.clearTrackTimer(target)
				}
				snap := c.probeLocked(ctx, cfg, true)
				res.CurrentBalance = snap.CurrentBalance
				res.ConsumptionRateHourly = snap.ConsumptionRateHourly
				return res, snap, nerr
			}
		}
		if cliOK {
			if knownCLIAccount == "" {
				cliProbe := c.probeCLIAccount(ctx, cliPath, cfg.MonthlyAllowance, c.now().Unix())
				knownCLIAccount = strings.ToLower(strings.TrimSpace(cliProbe.Account))
			}
			if knownCLIAccount == "" || knownCLIAccount == target {
				return c.nukeCLILocked(ctx, cfg, cliPath, knownCLIAccount)
			}
		}
		err := fmt.Errorf("未找到 Colab 账号: %s", targetAccount)
		snap := c.probeLocked(ctx, cfg, true)
		return NukeResult{
			OK:        false,
			Error:     err.Error(),
			CheckedAt: c.now().Unix(),
		}, snap, err
	}

	if len(oauthAccs) == 0 && !cliOK {
		err := fmt.Errorf("未找到可执行编排器: %s", cfg.CLIPath)
		snap := c.probeLocked(ctx, cfg, true)
		return NukeResult{
			OK:        false,
			Error:     err.Error(),
			CheckedAt: c.now().Unix(),
		}, snap, err
	}

	if len(oauthAccs) == 0 && cliOK {
		return c.nukeCLILocked(ctx, cfg, cliPath, knownCLIAccount)
	}

	oauthEmails := make(map[string]bool, len(oauthAccs))
	combined := NukeResult{
		OK:        true,
		CheckedAt: c.now().Unix(),
	}
	var errMsgs []string

	for _, acc := range oauthAccs {
		low := strings.ToLower(strings.TrimSpace(acc.Email))
		oauthEmails[low] = true
		res, nerr := c.nukeOAuthAccount(ctx, acc)
		combined.TerminatedCount += res.TerminatedCount
		combined.Terminated = append(combined.Terminated, res.Terminated...)
		combined.RemainingCount += res.RemainingCount
		combined.FailedCount += res.FailedCount
		if res.OK && res.RemainingCount == 0 && res.FailedCount == 0 {
			c.clearTrackTimer(low)
		}
		if nerr != nil {
			combined.OK = false
			errMsgs = append(errMsgs, fmt.Sprintf("%s: %v", acc.Email, nerr))
		}
	}

	if cliOK {
		if knownCLIAccount == "" {
			cliProbe := c.probeCLIAccount(ctx, cliPath, cfg.MonthlyAllowance, c.now().Unix())
			knownCLIAccount = strings.ToLower(strings.TrimSpace(cliProbe.Account))
		}
		if knownCLIAccount != "" && !oauthEmails[knownCLIAccount] && !c.isHidden(knownCLIAccount) {
			cliRes, cliErr := c.execCLINuke(ctx, cliPath)
			combined.TerminatedCount += cliRes.TerminatedCount
			combined.Terminated = append(combined.Terminated, cliRes.Terminated...)
			combined.RemainingCount += cliRes.RemainingCount
			combined.FailedCount += cliRes.FailedCount
			if cliRes.OK && cliRes.RemainingCount == 0 && cliRes.FailedCount == 0 {
				c.clearTrackTimer(knownCLIAccount)
			}
			if cliErr != nil {
				combined.OK = false
				errMsgs = append(errMsgs, cliErr.Error())
			}
		}
	}

	snap := c.probeLocked(ctx, cfg, true)
	combined.CurrentBalance = snap.CurrentBalance
	combined.ConsumptionRateHourly = snap.ConsumptionRateHourly
	if len(errMsgs) > 0 {
		combined.OK = false
		combined.Error = strings.Join(errMsgs, "; ")
		return combined, snap, errors.New(combined.Error)
	}
	return combined, snap, nil
}

func (c *Collector) clearTrackTimer(key string) {
	c.mu.Lock()
	if st := c.acctStates[key]; st != nil {
		st.activeSince = 0
		st.lastBurnAlertAt = 0
	}
	if st := c.acctStates["_cli"]; key == "" && st != nil {
		st.activeSince = 0
		st.lastBurnAlertAt = 0
	}
	c.mu.Unlock()
}

func (c *Collector) nukeCLILocked(ctx context.Context, cfg config.ColabConfig, cliPath, knownAccount string) (NukeResult, Snapshot, error) {
	res, err := c.execCLINuke(ctx, cliPath)
	if res.RemainingCount == 0 && res.FailedCount == 0 && res.OK {
		c.clearTrackTimer(accountTrackKey(knownAccount))
		c.clearTrackTimer("_cli")
	}
	snap := c.probeLocked(ctx, cfg, true)
	if err != nil {
		return res, snap, err
	}
	return res, snap, nil
}

func (c *Collector) execCLINuke(ctx context.Context, cliPath string) (NukeResult, error) {
	stdout, stderr, runErr := runCLICommand(ctx, cliPath, []string{"nuke", "--json"}, 35*time.Second)
	var res NukeResult
	jerr := json.Unmarshal([]byte(stdout), &res)
	if jerr != nil {
		msg := summarizeExecErr(runErr, stderr)
		if msg == "" {
			msg = fmt.Sprintf("解析 colab-safe nuke --json 失败: %v", jerr)
		}
		return NukeResult{
			OK:        false,
			Error:     msg,
			CheckedAt: c.now().Unix(),
		}, errors.New(msg)
	}
	if !res.OK || runErr != nil {
		msg := res.Error
		if msg == "" {
			msg = summarizeExecErr(runErr, stderr)
		}
		if msg == "" {
			msg = fmt.Sprintf("清场未完全成功：失败 %d 台，剩余 %d 台", res.FailedCount, res.RemainingCount)
		}
		return res, errors.New(msg)
	}
	return res, nil
}

// EvaluateAlerts checks whether orphan/long-burn or low-balance Telegram notifications should fire now.
func (c *Collector) EvaluateAlerts(cfg config.ColabConfig, now time.Time) (burnMsgNeeded bool, lowMsgNeeded bool, snap Snapshot) {
	alerts := c.EvaluateAccountAlerts(cfg, now)
	for _, al := range alerts {
		if al.BurnMsgNeeded || al.LowMsgNeeded {
			return al.BurnMsgNeeded, al.LowMsgNeeded, al.Snapshot
		}
	}
	return false, false, c.Last()
}

// EvaluateAccountAlerts evaluates burn and low-balance notifications independently for each Colab account.
func (c *Collector) EvaluateAccountAlerts(cfg config.ColabConfig, now time.Time) []AccountAlert {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !cfg.Enabled {
		return nil
	}
	nowUnix := now.Unix()
	snap := c.snapshotWithElapsedLocked(nowUnix)
	warnMins := cfg.WarnActiveMinutes
	if warnMins <= 0 {
		warnMins = 30
	}

	targets := snap.Accounts
	if len(targets) == 0 && snap.OK {
		targets = []Snapshot{snap}
	}

	var alerts []AccountAlert
	for _, item := range targets {
		if !item.OK {
			continue
		}
		key := accountTrackKey(item.Account)
		st := c.trackStateLocked(key)

		var burnMsg, lowMsg bool
		burning := item.AssignmentsCount > 0 || len(item.Sessions) > 0 || item.ConsumptionRateHourly > 0.01
		if burning {
			shouldWarnBurn := (item.OrphanCount > 0 && item.ActiveMinutes >= 5) || item.ActiveMinutes >= warnMins
			if shouldWarnBurn && (st.lastBurnAlertAt == 0 || nowUnix-st.lastBurnAlertAt >= burnAlertCooldown) {
				st.lastBurnAlertAt = nowUnix
				burnMsg = true
			}
		}

		if cfg.LowBalanceCCU > 0 && item.CurrentBalance > 0 {
			if item.CurrentBalance <= cfg.LowBalanceCCU {
				if !st.lowAlertSent {
					st.lowAlertSent = true
					lowMsg = true
				}
			} else if item.CurrentBalance > cfg.LowBalanceCCU+20 {
				st.lowAlertSent = false
			}
		}

		if burnMsg || lowMsg {
			alerts = append(alerts, AccountAlert{
				Account:       item.Account,
				BurnMsgNeeded: burnMsg,
				LowMsgNeeded:  lowMsg,
				Snapshot:      item,
			})
		}
	}
	return alerts
}

func resolveCLIPath(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	expanded := config.ExpandPath(trimmed)
	if expanded != "" && isExecutableFile(expanded) {
		return expanded, true
	}
	if trimmed == "" || trimmed == "~/.local/bin/colab-safe" {
		if found, err := exec.LookPath("colab-safe"); err == nil && isExecutableFile(found) {
			return found, true
		}
	}
	return "", false
}

func isExecutableFile(p string) bool {
	info, err := os.Stat(p)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode()&0o111 != 0
}

func runCLICommand(ctx context.Context, binPath string, args []string, timeout time.Duration) (string, string, error) {
	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(tctx, binPath, args...)
	cmd.Env = buildCommandEnv()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()), err
}

func buildCommandEnv() []string {
	home, _ := os.UserHomeDir()
	prefix := "/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin"
	if home != "" {
		prefix += ":" + filepath.Join(home, ".local", "bin")
	}

	env := os.Environ()
	out := make([]string, 0, len(env)+1)
	pathSet := false
	for _, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			cur := strings.TrimPrefix(kv, "PATH=")
			out = append(out, "PATH="+prefix+":"+cur)
			pathSet = true
		} else {
			out = append(out, kv)
		}
	}
	if !pathSet {
		out = append(out, "PATH="+prefix)
	}
	return out
}

func summarizeExecErr(err error, stderr string) string {
	if stderr != "" {
		return stderr
	}
	if err != nil {
		return err.Error()
	}
	return ""
}

func cloneSnapshot(src Snapshot) Snapshot {
	dst := src
	if src.EligibleGPUs != nil {
		dst.EligibleGPUs = append([]string(nil), src.EligibleGPUs...)
	}
	if src.EligibleTPUs != nil {
		dst.EligibleTPUs = append([]string(nil), src.EligibleTPUs...)
	}
	if src.Sessions != nil {
		dst.Sessions = append([]SessionItem(nil), src.Sessions...)
	}
	if src.Accounts != nil {
		dst.Accounts = make([]Snapshot, len(src.Accounts))
		for i, a := range src.Accounts {
			dst.Accounts[i] = cloneSnapshot(a)
		}
	}
	return dst
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func elapsedMs(t0 time.Time) float64 {
	return float64(time.Since(t0).Microseconds()) / 1000.0
}
