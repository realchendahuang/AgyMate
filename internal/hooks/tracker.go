package hooks

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/realchendahuang/AgyMate/internal/config"
	"github.com/realchendahuang/AgyMate/internal/instances"
	"github.com/realchendahuang/AgyMate/internal/notify"
	"github.com/realchendahuang/AgyMate/internal/rpc"
	"github.com/realchendahuang/AgyMate/internal/telemetry"
)

// LiveSession represents the real-time execution state of a conversation.
type LiveSession struct {
	ConversationID    string `json:"conversationId"`
	InstanceID        string `json:"instanceId"`
	Title             string `json:"title,omitempty"`
	Workspace         string `json:"workspace,omitempty"`
	Model             string `json:"model,omitempty"`
	State             string `json:"state"` // running | waiting_approval | idle | error
	LastEvent         string `json:"lastEvent,omitempty"`
	LastTool          string `json:"lastTool,omitempty"`
	TerminationReason string `json:"terminationReason,omitempty"`
	Error             string `json:"error,omitempty"`
	StartedAt         int64  `json:"startedAt"`
	UpdatedAt         int64  `json:"updatedAt"`
	DurationSec       int64  `json:"durationSec"`
	stuckAlertedAt    int64
}

// Tracker maintains live session states from Hooks + RPC + SQLite step inspection
// and dispatches Telegram alerts asynchronously.
type Tracker struct {
	mu       sync.Mutex
	sessions map[string]*LiveSession // key: conversationId
	notifier *notify.Client
	getCfg   func() config.Config
}

// NewTracker constructs a session state tracker.
func NewTracker(getCfg func() config.Config) *Tracker {
	return &Tracker{
		sessions: make(map[string]*LiveSession),
		notifier: notify.NewClient(),
		getCfg:   getCfg,
	}
}

// Ingest handles one incoming lifecycle hook payload and triggers notifications if needed.
func (t *Tracker) Ingest(ev EventPayload) LiveSession {
	now := time.Now().Unix()
	if ev.InstanceID == "" {
		ev.InstanceID = InferInstanceID(ev)
	}
	cid := ev.ConversationID
	if cid == "" {
		cid = "unknown-" + ev.InstanceID
	}

	var ws string
	if len(ev.WorkspacePaths) > 0 {
		ws = filepath.Base(ev.WorkspacePaths[0])
		if ws == "" || ws == "." || ws == "/" {
			ws = ev.WorkspacePaths[0]
		}
	}

	t.mu.Lock()
	sess := t.sessions[cid]
	if sess == nil {
		sess = &LiveSession{
			ConversationID: cid,
			InstanceID:     ev.InstanceID,
			StartedAt:      now,
		}
		t.sessions[cid] = sess
	}
	if ev.InstanceID != "" {
		sess.InstanceID = ev.InstanceID
	}
	if ws != "" {
		sess.Workspace = ws
	}
	if ev.ModelName != "" {
		sess.Model = ev.ModelName
	}
	if ev.LastUserInput != "" && sess.Title == "" {
		title := strings.TrimSpace(ev.LastUserInput)
		if len([]rune(title)) > 64 {
			title = string([]rune(title)[:64]) + "..."
		}
		sess.Title = title
	}
	sess.LastEvent = ev.HookEventName
	sess.UpdatedAt = now
	if ev.ToolCall != nil && ev.ToolCall.Name != "" {
		sess.LastTool = ev.ToolCall.Name
	}

	var sendMsg string
	switch ev.HookEventName {
	case "SessionStart", "PreInvocation":
		if sess.State != "running" {
			sess.StartedAt = now
		}
		sess.State = "running"
		sess.Error = ""
		sess.TerminationReason = ""
		sess.stuckAlertedAt = 0
	case "PostToolUse":
		sess.State = "running"
		sess.stuckAlertedAt = 0
	case "Stop":
		sess.TerminationReason = ev.TerminationReason
		sess.Error = ev.Error
		dur := now - sess.StartedAt
		sess.DurationSec = dur
		isErr := ev.Error != "" || (ev.TerminationReason != "" && ev.TerminationReason != "NO_TOOL_CALL" && ev.TerminationReason != "USER_CANCELED")
		if isErr {
			sess.State = "error"
		} else {
			sess.State = "idle"
		}

		cfg := t.getCfg()
		if cfg.Telegram.Enabled && cfg.Telegram.BotToken != "" && cfg.Telegram.ChatID != "" {
			if isErr && cfg.Telegram.NotifyOnError {
				sendMsg = notify.FormatSessionStop(sess.InstanceID, sess.Title, sess.Workspace, sess.Model, sess.TerminationReason, sess.Error, dur, true)
			} else if !isErr && cfg.Telegram.NotifyOnStop && ev.TerminationReason != "USER_CANCELED" && dur >= int64(cfg.Telegram.NotifyMinDurationSec) {
				sendMsg = notify.FormatSessionStop(sess.InstanceID, sess.Title, sess.Workspace, sess.Model, sess.TerminationReason, "", dur, false)
			}
		}
	}

	out := *sess
	t.mu.Unlock()

	if sendMsg != "" {
		cfg := t.getCfg()
		go func(tok, chat, msg string) {
			ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
			defer cancel()
			_ = t.notifier.SendHTML(ctx, tok, chat, msg)
		}(cfg.Telegram.BotToken, cfg.Telegram.ChatID, sendMsg)
	}
	return out
}

// ReconcileRPC merges live RPC trajectories and SQLite permission checks into the tracker,
// catching running sessions even if hooks didn't fire yet and detecting stuck approvals.
func (t *Tracker) ReconcileRPC(inst instances.Instance, snap rpc.InstanceSnapshot) {
	now := time.Now().Unix()
	cfg := t.getCfg()
	stuckThresh := int64(cfg.Telegram.NotifyOnStuckSec)
	if stuckThresh <= 0 {
		stuckThresh = 45
	}

	var alerts []string

	t.mu.Lock()
	for _, tr := range snap.Trajectories {
		if !tr.Running && tr.Status != "CASCADE_RUN_STATUS_WAITING" {
			// Update title/idle if tracked
			if sess, ok := t.sessions[tr.CascadeID]; ok {
				if tr.Title != "" {
					sess.Title = tr.Title
				}
				if sess.State == "running" || sess.State == "waiting_approval" {
					sess.State = "idle"
					sess.UpdatedAt = now
				}
			}
			continue
		}

		sess := t.sessions[tr.CascadeID]
		if sess == nil {
			sess = &LiveSession{
				ConversationID: tr.CascadeID,
				InstanceID:     inst.ID,
				StartedAt:      now,
				UpdatedAt:      now,
				State:          "running",
			}
			t.sessions[tr.CascadeID] = sess
		}
		if tr.Title != "" {
			sess.Title = tr.Title
		}
		if tr.Workspace != "" {
			sess.Workspace = filepath.Base(strings.TrimPrefix(tr.Workspace, "file://"))
		}
		if snap.Context != nil && snap.Context.CascadeID == tr.CascadeID && snap.Context.Model != "" {
			sess.Model = snap.Context.Model
		}
		sess.DurationSec = now - sess.StartedAt

		idleFor := now - sess.UpdatedAt
		if tr.Status == "CASCADE_RUN_STATUS_WAITING" || (idleFor >= stuckThresh && telemetry.CheckStepWaitingApproval(inst.GeminiDirs, tr.CascadeID)) {
			sess.State = "waiting_approval"
			if cfg.Telegram.Enabled && cfg.Telegram.BotToken != "" && cfg.Telegram.ChatID != "" &&
				sess.stuckAlertedAt == 0 {
				sess.stuckAlertedAt = now
				alerts = append(alerts, notify.FormatWaitingAlert(inst.ID, sess.Title, sess.Workspace, idleFor))
			}
		} else {
			sess.State = "running"
		}
	}

	// Expire stale non-running sessions after 2 hours
	for cid, sess := range t.sessions {
		if sess.State != "running" && sess.State != "waiting_approval" && now-sess.UpdatedAt > 7200 {
			delete(t.sessions, cid)
		}
	}
	t.mu.Unlock()

	for _, msg := range alerts {
		go func(tok, chat, m string) {
			ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
			defer cancel()
			_ = t.notifier.SendHTML(ctx, tok, chat, m)
		}(cfg.Telegram.BotToken, cfg.Telegram.ChatID, msg)
	}
}

// Snapshot returns current tracked sessions ordered by active state and recency.
func (t *Tracker) Snapshot() []LiveSession {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now().Unix()
	out := make([]LiveSession, 0, len(t.sessions))
	for _, s := range t.sessions {
		cp := *s
		if cp.State == "running" || cp.State == "waiting_approval" {
			cp.DurationSec = now - cp.StartedAt
		}
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool {
		ri := stateRank(out[i].State)
		rj := stateRank(out[j].State)
		if ri != rj {
			return ri < rj
		}
		return out[i].UpdatedAt > out[j].UpdatedAt
	})
	return out
}

func stateRank(s string) int {
	switch s {
	case "waiting_approval":
		return 0
	case "running":
		return 1
	case "error":
		return 2
	default:
		return 3
	}
}
