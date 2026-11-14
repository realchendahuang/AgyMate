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
	Window              string  `json:"window"` // "5h" | "weekly" | ...
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
	Email         string           `json:"email,omitempty"`
	UserName      string           `json:"userName,omitempty"`
	PlanName      string           `json:"planName,omitempty"`
	TierID        string           `json:"tierId,omitempty"`
	Buckets       []QuotaBucket    `json:"buckets,omitempty"`
	Models        []ModelQuota     `json:"models,omitempty"`
	Context       *ContextWindow   `json:"context,omitempty"`
	Trajectories  []TrajectoryInfo `json:"trajectories,omitempty"`
	RunningCount  int              `json:"runningCount"`
	AuthFailed    bool             `json:"authFailed"`
	Error         string           `json:"error,omitempty"`
	CollectedAt   int64            `json:"collectedAt"`
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
			"MODEL_PLACEHOLDER_M667": "Gemini 4 Hydrogen (Medium)",
			"MODEL_PLACEHOLDER_M525": "Gemini 4 Hydrogen (High)",
			"MODEL_PLACEHOLDER_M526": "Gemini 4 Hydrogen (Medium)",
			"MODEL_PLACEHOLDER_M528": "Gemini 4 Hydrogen (XHigh)",
			"MODEL_PLACEHOLDER_M427": "Gemini 4 Carbon (High)",
			"MODEL_PLACEHOLDER_M437": "Gemini 4 Carbon (Medium)",
			"MODEL_PLACEHOLDER_M438": "Gemini 4 Carbon (Low)",
			"MODEL_PLACEHOLDER_M417": "Gemini 4 Argon (High)",
			"MODEL_PLACEHOLDER_M418": "Gemini 4 Argon (Medium)",
			"MODEL_PLACEHOLDER_M419": "Gemini 4 Argon (Low)",
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
			for _, b := range grp.Buckets {
				resetUnix := parseISOUnix(b.ResetTime)
				bucketKey := instID + ":" + b.BucketID
				var runway config.RunwayEstimate
				if state != nil {
					state.RecordQuota(bucketKey, b.RemainingFraction, resetUnix, now)
					runway = state.EstimateRunway(bucketKey, b.RemainingFraction, resetUnix, now)
				}
				snap.Buckets = append(snap.Buckets, QuotaBucket{
					GroupName:           grp.DisplayName,
					BucketID:            b.BucketID,
					DisplayName:         b.DisplayName,
					Window:              b.Window,
					RemainingFraction:   b.RemainingFraction,
					RemainingPct:        b.RemainingFraction * 100.0,
					ResetTime:           b.ResetTime,
					ResetUnix:           resetUnix,
					BurnPctPerHour:      runway.BurnPctPerHour,
					HoursUntilEmpty:     runway.HoursUntilEmpty,
					ExhaustsBeforeReset: runway.ExhaustsBeforeReset,
				})
			}
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
			cw := c.fetchContextWindow(ctx, instID, hub, list[0])
			if cw != nil {
				snap.Context = cw
			}
		}
	}

	return snap
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
			cg := ContextGroup{Name: g.Name, NumTokens: int64(g.NumTokens)}
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
				Window            string  `json:"window"`
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
