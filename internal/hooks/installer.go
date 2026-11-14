// Package hooks manages Antigravity lifecycle hooks registration and live session tracking.
package hooks

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/realchendahuang/AgyMate/internal/config"
)

// EventPayload matches Antigravity's stdin JSON contract (plus optional instanceId).
type EventPayload struct {
	InstanceID        string   `json:"instanceId,omitempty"`
	ConversationID    string   `json:"conversationId"`
	WorkspacePaths    []string `json:"workspacePaths"`
	TranscriptPath    string   `json:"transcriptPath"`
	ModelName         string   `json:"modelName"`
	LastUserInput     string   `json:"lastUserInput"`
	HookEventName     string   `json:"hookEventName"`
	CWD               string   `json:"cwd"`
	StepIdx           int      `json:"stepIdx,omitempty"`
	Error             string   `json:"error,omitempty"`
	TerminationReason string   `json:"terminationReason,omitempty"`
	FullyIdle         bool     `json:"fullyIdle,omitempty"`
	ToolCall          *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"toolCall,omitempty"`
}

// InstallTargets returns all hooks.json paths (global + each ParaGravity profile).
func InstallTargets(cfg config.Config) []string {
	realHome, _ := os.UserHomeDir()
	targets := []string{
		filepath.Join(realHome, ".gemini", "config", "hooks.json"),
	}
	profilesDir := config.ExpandPath(cfg.Box.ProfilesDir)
	if ents, err := os.ReadDir(profilesDir); err == nil {
		for _, ent := range ents {
			if !ent.IsDir() {
				continue
			}
			pdir := filepath.Join(profilesDir, ent.Name())
			if _, err := os.Stat(filepath.Join(pdir, "profile.json")); err == nil {
				targets = append(targets, filepath.Join(pdir, "home", ".gemini", "config", "hooks.json"))
			}
		}
	}
	return targets
}

// InstallHooks merges the "agymate" hook spec into global and profile hooks.json files
// without disturbing existing keys (e.g. "herdr", "marktoast") and without leaving .bak files.
func InstallHooks(cfg config.Config, cliBin string) ([]string, error) {
	if cliBin == "" {
		realHome, _ := os.UserHomeDir()
		cliBin = filepath.Join(realHome, ".local", "bin", "agymate")
	}
	spec := buildHookSpec(cliBin)
	targets := InstallTargets(cfg)
	var updated []string
	for _, p := range targets {
		if err := mergeHooksFile(p, spec); err != nil {
			return updated, fmt.Errorf("update %s: %w", p, err)
		}
		updated = append(updated, p)
	}
	return updated, nil
}

// CheckInstalled reports how many target hooks.json files currently contain "agymate".
func CheckInstalled(cfg config.Config) (installed int, total int, missing []string) {
	targets := InstallTargets(cfg)
	total = len(targets)
	for _, p := range targets {
		raw, err := os.ReadFile(p)
		if err != nil {
			missing = append(missing, p)
			continue
		}
		var doc map[string]any
		if json.Unmarshal(raw, &doc) != nil {
			missing = append(missing, p)
			continue
		}
		if _, ok := doc["agymate"]; ok {
			installed++
		} else {
			missing = append(missing, p)
		}
	}
	return installed, total, missing
}

func buildHookSpec(cliBin string) map[string]any {
	cmdFor := func(ev string) string {
		return fmt.Sprintf("%s hook --event %s", shellQuote(cliBin), ev)
	}
	// Do NOT register PreToolUse so permission dialogs are never touched.
	return map[string]any{
		"SessionStart": []any{
			map[string]any{"type": "command", "command": cmdFor("SessionStart"), "timeout": 5},
		},
		"PreInvocation": []any{
			map[string]any{"type": "command", "command": cmdFor("PreInvocation"), "timeout": 5},
		},
		"PostToolUse": []any{
			map[string]any{
				"matcher": "",
				"hooks": []any{
					map[string]any{"type": "command", "command": cmdFor("PostToolUse"), "timeout": 5},
				},
			},
		},
		"Stop": []any{
			map[string]any{"type": "command", "command": cmdFor("Stop"), "timeout": 5},
		},
	}
}

func mergeHooksFile(path string, spec map[string]any) error {
	doc := make(map[string]any)
	if raw, err := os.ReadFile(path); err == nil && len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &doc); err != nil {
			return fmt.Errorf("invalid existing JSON in %s: %w", path, err)
		}
	}
	doc["agymate"] = spec
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func shellQuote(s string) string {
	if !strings.ContainsAny(s, " \t'\"\\$`") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

// InferInstanceID determines whether a hook event originated from main, ide, or a profile p1..pN.
func InferInstanceID(ev EventPayload) string {
	paths := []string{ev.TranscriptPath, os.Getenv("HOME")}
	for _, p := range paths {
		if p == "" {
			continue
		}
		if idx := strings.Index(p, ".antigravity-profiles/"); idx >= 0 {
			rest := p[idx+len(".antigravity-profiles/"):]
			if slash := strings.IndexByte(rest, '/'); slash > 0 {
				return rest[:slash]
			}
			if rest != "" {
				return rest
			}
		}
		if strings.Contains(p, "antigravity-ide/") {
			return "ide"
		}
	}
	return "main"
}

// IngestFromStdin reads stdin JSON, forwards it to the local AgyMate hub with a 100ms timeout,
// and always succeeds non-blockingly (returning `{}`).
func IngestFromStdin(listenAddr, eventOverride string) []byte {
	raw, _ := io.ReadAll(io.LimitReader(os.Stdin, 256*1024))
	var ev EventPayload
	_ = json.Unmarshal(raw, &ev)
	if eventOverride != "" {
		ev.HookEventName = eventOverride
	}
	if ev.InstanceID == "" {
		ev.InstanceID = InferInstanceID(ev)
	}
	if ev.ConversationID != "" || ev.HookEventName != "" {
		body, err := json.Marshal(ev)
		if err == nil {
			client := &http.Client{Timeout: 100 * time.Millisecond}
			resp, err := client.Post("http://"+listenAddr+"/api/hooks/ingest", "application/json", bytes.NewReader(body))
			if err == nil {
				_ = resp.Body.Close()
			}
		}
	}
	return []byte("{}\n")
}
