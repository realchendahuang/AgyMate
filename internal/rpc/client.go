// Package rpc queries local Antigravity Language Server Connect-RPC endpoints
// over 127.0.0.1 using daemon/hub_*.json ports and csrfToken.
package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/realchendahuang/AgyMate/internal/config"
	"github.com/realchendahuang/AgyMate/internal/instances"
)

const servicePrefix = "/exa.language_server_pb.LanguageServerService/"

// QuotaBucket represents one official rate-limit bucket (e.g. 5h or weekly).
type QuotaBucket struct {
	GroupName           string  `json:"groupName"`
	BucketID            string  `json:"bucketId"`
	DisplayName         string  `json:"displayName"`
	Description         string  `json:"description,omitempty"`
	Window              string  `json:"window"` // "5h" | "weekly" | ...
	Disabled            bool    `json:"disabled,omitempty"`
	RemainingFraction   float64 `json:"remainingFraction"`
	RemainingPct        float64 `json:"remainingPct"`
	ResetTime           string  `json:"resetTime"`
	ResetUnix           int64   `json:"resetUnix"`
	BurnPctPerHour      float64 `json:"burnPctPerHour"`
	HoursUntilEmpty     float64 `json:"hoursUntilEmpty"`
	ExhaustsBeforeReset bool    `json:"exhaustsBeforeReset"`
}

// ModelQuota captures per-model remaining quota from GetUserStatus.
type ModelQuota struct {
	Label             string  `json:"label"`
	ModelID           string  `json:"modelId"`
	ModelAlias        string  `json:"modelAlias"`
	RemainingFraction float64 `json:"remainingFraction"`
	RemainingPct      float64 `json:"remainingPct"`
	ResetTime         string  `json:"resetTime"`
}

// ContextGroup describes a top-level context window breakdown slice.
type ContextGroup struct {
	Key       string         `json:"key,omitempty"`
	Name      string         `json:"name"`
	NumTokens int64          `json:"numTokens"`
	Children  []ContextChild `json:"children,omitempty"`
}

// ContextChild describes a sub-item in the context breakdown.
type ContextChild struct {
	Name      string `json:"name"`
	NumTokens int64  `json:"numTokens"`
}

// ContextWindow describes the active conversation's context consumption.
type ContextWindow struct {
	CascadeID           string         `json:"cascadeId"`
	Title               string         `json:"title"`
	Status              string         `json:"status"`
	Running             bool           `json:"running"`
	StepCount           int            `json:"stepCount"`
	LastModifiedTime    string         `json:"lastModifiedTime"`
	Workspace           string         `json:"workspace,omitempty"`
	Model               string         `json:"model,omitempty"`
	CheckpointIndex     int            `json:"checkpointIndex"`
	EstimatedTokensUsed int64          `json:"estimatedTokensUsed"`
	MaxContextTokens    int64          `json:"maxContextTokens"`
	UsagePct            float64        `json:"usagePct"`
	TruncationReason    string         `json:"truncationReason,omitempty"`
	TokenBreakdown      []ContextGroup `json:"tokenBreakdown,omitempty"`
}

// TrajectoryInfo summarizes one conversation returned by GetAllCascadeTrajectories.
type TrajectoryInfo struct {
	CascadeID         string `json:"cascadeId"`
	Title             string `json:"title"`
	Status            string `json:"status"`
	Running           bool   `json:"running"`
	StepCount         int    `json:"stepCount"`
	LastModifiedTime  string `json:"lastModifiedTime"`
	CreatedTime       string `json:"createdTime"`
	Workspace         string `json:"workspace,omitempty"`
	TerminationReason string `json:"terminationReason,omitempty"`
}

// InstanceSnapshot holds all RPC-derived live telemetry for one Antigravity instance.
type InstanceSnapshot struct {
	Email              string                    `json:"email,omitempty"`
	UserName           string                    `json:"userName,omitempty"`
	PlanName           string                    `json:"planName,omitempty"`
	TierID             string                    `json:"tierId,omitempty"`
	Buckets            []QuotaBucket             `json:"buckets,omitempty"`
	Models             []ModelQuota              `json:"models,omitempty"`
	Context            *ContextWindow            `json:"context,omitempty"`
	TrajectoryContexts map[string]*ContextWindow `json:"trajectoryContexts,omitempty"`
	Trajectories       []TrajectoryInfo          `json:"trajectories,omitempty"`
	RunningCount       int                       `json:"runningCount"`
	AuthFailed         bool                      `json:"authFailed"`
	Error              string                    `json:"error,omitempty"`
	CollectedAt        int64                     `json:"collectedAt"`
}

// Client caches Connect-RPC metadata and dynamic model labels.
type Client struct {
	httpClient  *http.Client
	mu          sync.RWMutex
	modelLabels map[string]string
	ctxCache    map[string]cachedContext
}

type cachedContext struct {
	cascadeID string
	stepCount int
	window    ContextWindow
}

// NewClient creates a local Connect-RPC client with a 3-second request timeout.
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 3 * time.Second},
		modelLabels: map[string]string{
			"MODEL_PLACEHOLDER_M532":           "Gemini 4 Neon (High)",
			"MODEL_PLACEHOLDER_M533":           "Gemini 4 Neon (Medium)",
			"MODEL_PLACEHOLDER_M534":           "Gemini 4 Neon (Low)",
			"MODEL_PLACEHOLDER_M535":           "Gemini 4 Neon (XHigh)",
			"MODEL_PLACEHOLDER_M537":           "Orchestrator (Neon)",
			"MODEL_PLACEHOLDER_M667":           "Orchestrator (Carbon)",
			"MODEL_PLACEHOLDER_M681":           "Orchestrator (Argon)",
			"MODEL_PLACEHOLDER_M525":           "Gemini 4 Hydrogen (High)",
			"MODEL_PLACEHOLDER_M526":           "Gemini 4 Hydrogen (Medium)",
			"MODEL_PLACEHOLDER_M527":           "Gemini 4 Hydrogen (Low)",
			"MODEL_PLACEHOLDER_M528":           "Gemini 4 Hydrogen (XHigh)",
			"MODEL_PLACEHOLDER_M427":           "Gemini 4 Carbon (High)",
			"MODEL_PLACEHOLDER_M437":           "Gemini 4 Carbon (Medium)",
			"MODEL_PLACEHOLDER_M438":           "Gemini 4 Carbon (Low)",
			"MODEL_PLACEHOLDER_M417":           "Gemini 4 Argon (High)",
			"MODEL_PLACEHOLDER_M418":           "Gemini 4 Argon (Medium)",
			"MODEL_PLACEHOLDER_M419":           "Gemini 4 Argon (Low)",
			"MODEL_PLACEHOLDER_M318":           "Gemini 3.8 Flash (High)",
			"MODEL_PLACEHOLDER_M319":           "Gemini 3.8 Flash (Medium)",
			"MODEL_PLACEHOLDER_M320":           "Gemini 3.8 Flash (Low)",
			"MODEL_PLACEHOLDER_M400":           "Claude Opus 5.5 (Low)",
			"MODEL_PLACEHOLDER_M401":           "Claude Opus 5.5 (Medium)",
			"MODEL_PLACEHOLDER_M402":           "Claude Opus 5.5 (High)",
			"MODEL_PLACEHOLDER_M403":           "Claude Sonnet 5.5 (Low)",
			"MODEL_PLACEHOLDER_M404":           "Claude Sonnet 5.5 (Medium)",
			"MODEL_PLACEHOLDER_M405":           "Claude Sonnet 5.5 (High)",
			"MODEL_OPENAI_GPT_OSS_120B_MEDIUM": "GPT-OSS 120B (Medium)",
		},
		ctxCache: make(map[string]cachedContext),
	}
}

// ModelLabel returns the friendly label for a model alias or ID if known.
func (c *Client) ModelLabel(key string) string {
	if key == "" {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if v, ok := c.modelLabels[key]; ok {
		return v
	}
	return ""
}

func (c *Client) registerModel(alias, modelID, label string) {
	if label == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if alias != "" {
		c.modelLabels[alias] = label
	}
	if modelID != "" {
		c.modelLabels[modelID] = label
	}
}

// FetchInstance queries GetUserStatus, RetrieveUserQuotaSummary, and active ContextWindow
// from an alive Language Server hub.
func (c *Client) FetchInstance(ctx context.Context, instID string, hub *instances.HubInfo, state *config.PersistentState) InstanceSnapshot {
	snap := InstanceSnapshot{CollectedAt: time.Now().Unix()}
	if hub == nil || hub.HTTPPort <= 0 {
		return snap
	}

	var usResp getUserStatusResp
	if status, err := c.call(ctx, hub, "GetUserStatus", nil, &usResp); err != nil {
		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			snap.AuthFailed = true
		}
		snap.Error = err.Error()
		return snap
	}

	us := usResp.UserStatus
	snap.Email = us.Email
	snap.UserName = us.Name
	if us.UserTier.Name != "" {
		snap.PlanName = us.UserTier.Name
		snap.TierID = us.UserTier.ID
	} else if us.PlanStatus.PlanInfo.PlanName != "" {
		snap.PlanName = us.PlanStatus.PlanInfo.PlanName
	}

	for _, mc := range us.CascadeModelConfigData.ClientModelConfigs {
		alias := mc.ModelOrAlias.Model
		c.registerModel(alias, mc.ModelID, mc.Label)
		snap.Models = append(snap.Models, ModelQuota{
			Label:             mc.Label,
			ModelID:           mc.ModelID,
			ModelAlias:        alias,
			RemainingFraction: mc.QuotaInfo.RemainingFraction,
			RemainingPct:      mc.QuotaInfo.RemainingFraction * 100.0,
			ResetTime:         mc.QuotaInfo.ResetTime,
		})
	}

	var qsResp retrieveQuotaSummaryResp
	if _, err := c.call(ctx, hub, "RetrieveUserQuotaSummary", nil, &qsResp); err == nil {
		now := time.Now()
		for _, grp := range qsResp.Response.Groups {
			grpBuckets := make([]QuotaBucket, 0, len(grp.Buckets))
			for _, b := range grp.Buckets {
				resetUnix := parseISOUnix(b.ResetTime)
				bucketKey := instID + ":" + b.BucketID
				runway := config.RunwayEstimate{HoursUntilEmpty: -1}
				if state != nil && !b.Disabled {
					state.RecordQuota(bucketKey, b.RemainingFraction, resetUnix, now)
					runway = state.EstimateRunway(bucketKey, b.RemainingFraction, resetUnix, now)
				}
				grpBuckets = append(grpBuckets, QuotaBucket{
					GroupName:           grp.DisplayName,
					BucketID:            b.BucketID,
					DisplayName:         FormatBucketDisplayName(grp.DisplayName, b.DisplayName, b.Window, b.BucketID),
					Description:         b.Description,
					Window:              b.Window,
					Disabled:            b.Disabled,
					RemainingFraction:   b.RemainingFraction,
					RemainingPct:        b.RemainingFraction * 100.0,
					ResetTime:           b.ResetTime,
					ResetUnix:           resetUnix,
					BurnPctPerHour:      runway.BurnPctPerHour,
					HoursUntilEmpty:     runway.HoursUntilEmpty,
					ExhaustsBeforeReset: runway.ExhaustsBeforeReset,
				})
			}
			sort.SliceStable(grpBuckets, func(i, j int) bool {
				return windowSortOrder(grpBuckets[i].Window, grpBuckets[i].BucketID) < windowSortOrder(grpBuckets[j].Window, grpBuckets[j].BucketID)
			})
			snap.Buckets = append(snap.Buckets, grpBuckets...)
		}
	}

	var trResp getAllTrajectoriesResp
	if _, err := c.call(ctx, hub, "GetAllCascadeTrajectories", nil, &trResp); err == nil {
		var list []TrajectoryInfo
		for cid, s := range trResp.TrajectorySummaries {
			if s.TrajectoryType != "" && s.TrajectoryType != "CORTEX_TRAJECTORY_TYPE_CASCADE" {
				continue
			}
			running := s.Status == "CASCADE_RUN_STATUS_RUNNING"
			if running {
				snap.RunningCount++
			}
			ws := ""
			if len(s.Workspaces) > 0 {
				ws = s.Workspaces[0].WorkspaceFolderAbsoluteUri
			}
			list = append(list, TrajectoryInfo{
				CascadeID:         cid,
				Title:             s.Summary,
				Status:            s.Status,
				Running:           running,
				StepCount:         s.StepCount,
				LastModifiedTime:  s.LastModifiedTime,
				CreatedTime:       s.CreatedTime,
				Workspace:         ws,
				TerminationReason: s.LatestExecutorTerminationReason,
			})
		}
		sort.Slice(list, func(i, j int) bool {
			if list[i].Running != list[j].Running {
				return list[i].Running
			}
			return list[i].LastModifiedTime > list[j].LastModifiedTime
		})
		if len(list) > 20 {
			snap.Trajectories = list[:20]
		} else {
			snap.Trajectories = list
		}
		if len(list) > 0 {
			limit := len(list)
			if limit > 6 {
				limit = 6
			}
			trajCtxs := make(map[string]*ContextWindow, limit)
			for i := 0; i < limit; i++ {
				if cw := c.fetchContextWindow(ctx, instID, hub, list[i]); cw != nil {
					trajCtxs[list[i].CascadeID] = cw
					if snap.Context == nil {
						snap.Context = cw
					}
				}
			}
			if len(trajCtxs) > 0 {
				snap.TrajectoryContexts = trajCtxs
			}
		}
	}

	return snap
}

// FetchConversationContext retrieves (or returns cached) live ContextWindow telemetry for any trajectory.
func (c *Client) FetchConversationContext(ctx context.Context, instID string, hub *instances.HubInfo, tr TrajectoryInfo) *ContextWindow {
	if hub == nil || hub.HTTPPort <= 0 || tr.CascadeID == "" {
		return nil
	}
	return c.fetchContextWindow(ctx, instID, hub, tr)
}

func (c *Client) fetchContextWindow(ctx context.Context, instID string, hub *instances.HubInfo, tr TrajectoryInfo) *ContextWindow {
	cacheKey := instID + ":" + tr.CascadeID
	c.mu.RLock()
	cached, ok := c.ctxCache[cacheKey]
	c.mu.RUnlock()
	if ok && cached.cascadeID == tr.CascadeID && cached.stepCount == tr.StepCount {
		w := cached.window
		w.Running = tr.Running
		w.Status = tr.Status
		w.Title = tr.Title
		w.Workspace = tr.Workspace
		w.LastModifiedTime = tr.LastModifiedTime
		return &w
	}

	var gmResp getGenMetaResp
	_, err := c.call(ctx, hub, "GetCascadeTrajectoryGeneratorMetadata", map[string]any{
		"cascadeId": tr.CascadeID,
	}, &gmResp)
	if err != nil {
		return nil
	}

	w := extractLatestContextWindow(tr, gmResp.GeneratorMetadata, c.ModelLabel)
	if w == nil {
		return nil
	}

	c.mu.Lock()
	c.ctxCache[cacheKey] = cachedContext{
		cascadeID: tr.CascadeID,
		stepCount: tr.StepCount,
		window:    *w,
	}
	c.mu.Unlock()
	return w
}

func extractLatestContextWindow(tr TrajectoryInfo, list []genMetaEntry, labelFn func(string) string) *ContextWindow {
	for i := len(list) - 1; i >= 0; i-- {
		cm := list[i].ChatModel
		if cm == nil {
			continue
		}
		cwm := cm.ChatStartMetadata.ContextWindowMetadata
		est := int64(cwm.EstimatedTokensUsed)
		usageTotal := int64(cm.Usage.InputTokens) + int64(cm.Usage.CacheReadTokens)
		if est < usageTotal {
			est = usageTotal
		}
		maxTok := int64(cwm.MaxContextTokens)
		if maxTok <= 0 && est > 0 {
			maxTok = 256000
		}
		if est == 0 && maxTok == 0 {
			continue
		}

		modelName := labelFn(cm.Model)
		if modelName == "" {
			modelName = cm.Model
		}
		var pct float64
		if maxTok > 0 {
			pct = float64(est) * 100.0 / float64(maxTok)
			if pct > 100 {
				pct = 100
			}
		}

		groups := make([]ContextGroup, 0, len(cwm.TokenBreakdown.Groups))
		for _, g := range cwm.TokenBreakdown.Groups {
			cg := ContextGroup{
				Key:       canonicalContextGroupKey(g.Name),
				Name:      LocalizeContextGroupName(g.Name),
				NumTokens: int64(g.NumTokens),
			}
			for _, ch := range g.Children {
				cg.Children = append(cg.Children, ContextChild{
					Name:      ch.Name,
					NumTokens: int64(ch.NumTokens),
				})
			}
			groups = append(groups, cg)
		}

		cpIdx := -1
		if cm.ChatStartMetadata.CheckpointIndex != nil {
			cpIdx = *cm.ChatStartMetadata.CheckpointIndex
		}

		return &ContextWindow{
			CascadeID:           tr.CascadeID,
			Title:               tr.Title,
			Status:              tr.Status,
			Running:             tr.Running,
			StepCount:           tr.StepCount,
			LastModifiedTime:    tr.LastModifiedTime,
			Workspace:           tr.Workspace,
			Model:               modelName,
			CheckpointIndex:     cpIdx,
			EstimatedTokensUsed: est,
			MaxContextTokens:    maxTok,
			UsagePct:            pct,
			TruncationReason:    cwm.TruncationReason,
			TokenBreakdown:      groups,
		}
	}
	return nil
}

func (c *Client) call(ctx context.Context, hub *instances.HubInfo, method string, extra map[string]any, out any) (int, error) {
	payload := map[string]any{
		"metadata": map[string]string{
			"ideName":       "antigravity",
			"extensionName": "antigravity",
			"locale":        "en",
		},
	}
	for k, v := range extra {
		payload[k] = v
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}

	url := fmt.Sprintf("http://127.0.0.1:%d%s%s", hub.HTTPPort, servicePrefix, method)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	req.Header.Set("x-codeium-csrf-token", hub.CSRFToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, fmt.Errorf("rpc %s HTTP %d: %s", method, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return resp.StatusCode, err
		}
	}
	return resp.StatusCode, nil
}

func parseISOUnix(s string) int64 {
	if s == "" {
		return 0
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, err = time.Parse(time.RFC3339, s)
		if err != nil {
			return 0
		}
	}
	return t.Unix()
}

func windowSortOrder(win, bucketID string) int {
	_, _, canon := LocalizeQuotaWindow(win, "", bucketID)
	switch canon {
	case "1h":
		return 1
	case "5h":
		return 2
	case "daily":
		return 3
	case "weekly":
		return 4
	case "monthly":
		return 5
	default:
		return 9
	}
}

func containsCJK(s string) bool {
	for _, r := range s {
		if r >= 0x4E00 && r <= 0x9FFF {
			return true
		}
	}
	return false
}

// LocalizeBucketGroup normalizes an Antigravity quota group/model family label.
func LocalizeBucketGroup(groupName, displayName, bucketID string) string {
	g := strings.TrimSpace(groupName)
	id := strings.ToLower(strings.TrimSpace(bucketID))
	disp := strings.ToLower(strings.TrimSpace(displayName))
	probe := strings.ToLower(g) + " " + id

	switch {
	case strings.Contains(probe, "gemini"):
		return "Gemini"
	case (strings.Contains(probe, "claude") && strings.Contains(probe, "gpt")) ||
		strings.HasPrefix(id, "3p-") || strings.HasPrefix(id, "3p_") ||
		strings.Contains(probe, "third-party") || strings.Contains(probe, "third party"):
		return "Claude / GPT"
	case strings.Contains(probe, "claude") || disp == "claude":
		return "Claude"
	case strings.Contains(probe, "gpt") || strings.Contains(probe, "openai"):
		return "GPT"
	}

	if g == "" {
		return ""
	}
	cleaned := strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(g, " Models"), " models"))
	switch strings.ToLower(cleaned) {
	case "", "all", "default", "general", "quota", "standard":
		return ""
	default:
		return cleaned
	}
}

// LocalizeQuotaWindow maps raw RPC window identifiers and English display labels into concise Chinese.
// Returns (shortCapsuleLabel, bucketSuffixLabel, canonicalWindow).
func LocalizeQuotaWindow(window, displayName, bucketID string) (string, string, string) {
	w := strings.ToLower(strings.TrimSpace(window))
	d := strings.ToLower(strings.TrimSpace(displayName))
	id := strings.ToLower(strings.TrimSpace(bucketID))
	probe := w + " " + d + " " + id

	switch {
	case w == "5h" || w == "5hr" || w == "five_hour" ||
		strings.Contains(probe, "five hour") || strings.Contains(probe, "5 hour") ||
		strings.Contains(probe, "5小时") || strings.HasSuffix(id, "-5h") || strings.HasSuffix(id, "_5h"):
		return "5小时", "5小时限额", "5h"
	case w == "weekly" || w == "7d" || w == "1w" || w == "week" ||
		strings.Contains(probe, "weekly") || strings.Contains(probe, "7 day") ||
		strings.Contains(probe, "周配额") || strings.Contains(probe, "周限额") ||
		strings.HasSuffix(id, "-weekly") || strings.HasSuffix(id, "_weekly"):
		return "周配额", "周配额", "weekly"
	case w == "daily" || w == "24h" || w == "1d" ||
		strings.Contains(probe, "daily") || strings.Contains(probe, "24 hour") ||
		strings.Contains(probe, "日配额") || strings.Contains(probe, "日限额"):
		return "日配额", "日配额", "daily"
	case w == "monthly" || w == "30d" || w == "1m" ||
		strings.Contains(probe, "monthly") || strings.Contains(probe, "月配额") || strings.Contains(probe, "月限额"):
		return "月配额", "月配额", "monthly"
	case w == "1h" || w == "hourly" || strings.Contains(probe, "1 hour") || strings.Contains(probe, "1小时"):
		return "1小时", "1小时限额", "1h"
	}

	if containsCJK(displayName) {
		trimmed := strings.TrimSpace(displayName)
		return trimmed, trimmed, w
	}
	return "配额", "配额余量", w
}

// FormatBucketDisplayName builds a clear, unambiguous Simplified Chinese bucket label
// such as "Gemini · 5小时限额" or "Claude / GPT · 周配额".
func FormatBucketDisplayName(groupName, displayName, window, bucketID string) string {
	trimmedDisp := strings.TrimSpace(displayName)
	grp := LocalizeBucketGroup(groupName, trimmedDisp, bucketID)
	_, fullWin, _ := LocalizeQuotaWindow(window, trimmedDisp, bucketID)

	if containsCJK(trimmedDisp) {
		if grp != "" && !strings.Contains(strings.ToLower(trimmedDisp), strings.ToLower(grp)) {
			return grp + " · " + trimmedDisp
		}
		return trimmedDisp
	}
	if grp != "" {
		return grp + " · " + fullWin
	}
	return fullWin
}

func canonicalContextGroupKey(raw string) string {
	norm := strings.ToLower(strings.Join(strings.Fields(strings.NewReplacer("_", " ", "-", " ").Replace(strings.TrimSpace(raw))), " "))
	switch {
	case strings.Contains(norm, "mcp"):
		return "mcp"
	case strings.Contains(norm, "skill"):
		return "skills"
	case strings.Contains(norm, "rule"):
		return "rules"
	case strings.Contains(norm, "subagent"):
		return "subagents"
	case strings.Contains(norm, "workflow") || strings.Contains(norm, "flow"):
		return "workflows"
	case strings.Contains(norm, "plugin"):
		return "plugins"
	case strings.Contains(norm, "system") || strings.Contains(norm, "prompt") || strings.Contains(norm, "identity"):
		return "system"
	case strings.Contains(norm, "tool") && (strings.Contains(norm, "res") || strings.Contains(norm, "out") || strings.Contains(norm, "call")):
		return "tool_output"
	case strings.Contains(norm, "tool"):
		return "tools"
	case strings.Contains(norm, "user"):
		return "user"
	case strings.Contains(norm, "assistant") || strings.Contains(norm, "agent") || strings.Contains(norm, "planner") || strings.Contains(norm, "response"):
		return "agent"
	case strings.Contains(norm, "chat") || strings.Contains(norm, "message") || strings.Contains(norm, "conv") || strings.Contains(norm, "history"):
		return "chat"
	case strings.Contains(norm, "think") || strings.Contains(norm, "reason"):
		return "thinking"
	default:
		return "other"
	}
}

// LocalizeContextGroupName translates Antigravity ContextWindow breakdown group names into Simplified Chinese.
func LocalizeContextGroupName(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "其他开销"
	}
	if containsCJK(s) {
		return s
	}
	norm := strings.ToLower(strings.Join(strings.Fields(strings.NewReplacer("_", " ", "-", " ").Replace(s)), " "))
	switch norm {
	case "system prompt", "system instructions", "system instruction", "system":
		return "系统提示词"
	case "tools", "tool", "tool definitions", "tool declarations", "function declarations":
		return "工具定义"
	case "mcp tools", "mcp tool", "mcp servers", "mcp server", "mcp":
		return "MCP 工具"
	case "chat messages", "messages", "conversation", "conversation history", "history", "chat history":
		return "对话消息"
	case "tool results", "tool outputs", "tool responses", "tool calls", "tool output", "tool result":
		return "工具结果"
	case "thinking", "thought", "thoughts", "reasoning":
		return "思考过程"
	case "subagents", "subagent", "sub agents":
		return "子代理"
	case "skills", "skill":
		return "技能指令"
	case "plugins", "plugin", "featured plugins":
		return "插件上下文"
	case "rules", "rule", "user rules":
		return "用户规则"
	case "memory", "memories":
		return "记忆上下文"
	case "files", "file", "code context", "workspace", "attachments":
		return "文件与代码"
	case "artifacts", "artifact":
		return "产物文档"
	case "summary", "summaries", "checkpoint", "checkpoints", "compacted history":
		return "历史摘要"
	case "user input", "user messages", "user":
		return "用户输入"
	case "assistant", "model response", "planner response":
		return "模型回复"
	case "cache", "cached":
		return "缓存上下文"
	case "other", "misc", "miscellaneous":
		return "其他开销"
	}

	switch {
	case strings.Contains(norm, "mcp"):
		return "MCP 工具"
	case strings.Contains(norm, "tool") && (strings.Contains(norm, "res") || strings.Contains(norm, "out")):
		return "工具结果"
	case strings.Contains(norm, "tool"):
		return "工具定义"
	case strings.Contains(norm, "system") || strings.Contains(norm, "prompt"):
		return "系统提示词"
	case strings.Contains(norm, "chat") || strings.Contains(norm, "message") || strings.Contains(norm, "conv"):
		return "对话消息"
	case strings.Contains(norm, "think") || strings.Contains(norm, "reason"):
		return "思考过程"
	case strings.Contains(norm, "subagent") || strings.Contains(norm, "agent"):
		return "子代理"
	case strings.Contains(norm, "skill"):
		return "技能指令"
	case strings.Contains(norm, "plugin"):
		return "插件上下文"
	case strings.Contains(norm, "rule"):
		return "用户规则"
	case strings.Contains(norm, "mem"):
		return "记忆上下文"
	case strings.Contains(norm, "file") || strings.Contains(norm, "code") || strings.Contains(norm, "attach"):
		return "文件与代码"
	default:
		return "上下文项"
	}
}

// flexInt64 unmarshals JSON numbers or quoted numeric strings emitted by protojson.
type flexInt64 int64

func (f *flexInt64) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	if strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`) {
		s = s[1 : len(s)-1]
	}
	if s == "" {
		*f = 0
		return nil
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return err
	}
	*f = flexInt64(v)
	return nil
}

// Raw RPC response wire models

type getUserStatusResp struct {
	UserStatus struct {
		Name     string `json:"name"`
		Email    string `json:"email"`
		UserTier struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"userTier"`
		PlanStatus struct {
			PlanInfo struct {
				TeamsTier string `json:"teamsTier"`
				PlanName  string `json:"planName"`
			} `json:"planInfo"`
		} `json:"planStatus"`
		CascadeModelConfigData struct {
			ClientModelConfigs []struct {
				Label        string `json:"label"`
				ModelID      string `json:"modelId"`
				ModelOrAlias struct {
					Model string `json:"model"`
				} `json:"modelOrAlias"`
				QuotaInfo struct {
					RemainingFraction float64 `json:"remainingFraction"`
					ResetTime         string  `json:"resetTime"`
				} `json:"quotaInfo"`
			} `json:"clientModelConfigs"`
		} `json:"cascadeModelConfigData"`
	} `json:"userStatus"`
}

type retrieveQuotaSummaryResp struct {
	Response struct {
		Groups []struct {
			DisplayName string `json:"displayName"`
			Description string `json:"description"`
			Buckets     []struct {
				BucketID          string  `json:"bucketId"`
				DisplayName       string  `json:"displayName"`
				Description       string  `json:"description"`
				Window            string  `json:"window"`
				Disabled          bool    `json:"disabled"`
				RemainingFraction float64 `json:"remainingFraction"`
				ResetTime         string  `json:"resetTime"`
			} `json:"buckets"`
		} `json:"groups"`
	} `json:"response"`
}

type getAllTrajectoriesResp struct {
	TrajectorySummaries map[string]struct {
		Summary          string `json:"summary"`
		StepCount        int    `json:"stepCount"`
		Status           string `json:"status"`
		LastModifiedTime string `json:"lastModifiedTime"`
		CreatedTime      string `json:"createdTime"`
		TrajectoryType   string `json:"trajectoryType"`
		Workspaces       []struct {
			WorkspaceFolderAbsoluteUri string `json:"workspaceFolderAbsoluteUri"`
		} `json:"workspaces"`
		LatestExecutorTerminationReason string `json:"latestExecutorTerminationReason"`
	} `json:"trajectorySummaries"`
}

type genMetaEntry struct {
	ChatModel *struct {
		Model string `json:"model"`
		Usage struct {
			InputTokens     flexInt64 `json:"inputTokens"`
			OutputTokens    flexInt64 `json:"outputTokens"`
			CacheReadTokens flexInt64 `json:"cacheReadTokens"`
		} `json:"usage"`
		ChatStartMetadata struct {
			CheckpointIndex       *int `json:"checkpointIndex"`
			ContextWindowMetadata struct {
				EstimatedTokensUsed flexInt64 `json:"estimatedTokensUsed"`
				MaxContextTokens    flexInt64 `json:"maxContextTokens"`
				TruncationReason    string    `json:"truncationReason"`
				TokenBreakdown      struct {
					Groups []struct {
						Name      string    `json:"name"`
						NumTokens flexInt64 `json:"numTokens"`
						Children  []struct {
							Name      string    `json:"name"`
							NumTokens flexInt64 `json:"numTokens"`
						} `json:"children"`
					} `json:"groups"`
				} `json:"tokenBreakdown"`
			} `json:"contextWindowMetadata"`
		} `json:"chatStartMetadata"`
	} `json:"chatModel"`
}

type getGenMetaResp struct {
	GeneratorMetadata []genMetaEntry `json:"generatorMetadata"`
}
