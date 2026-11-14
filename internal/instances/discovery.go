// Package instances discovers and manages all local Antigravity instances:
// main (Antigravity.app), ide (Antigravity IDE.app), and ParaGravity sandboxes (p1..pN).
package instances

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/realchendahuang/AgyMate/internal/config"
)

const (
	KindMain    = "main"
	KindIDE     = "ide"
	KindProfile = "profile"
)

// HubInfo describes an active Antigravity language_server daemon hub.
type HubInfo struct {
	PID       int    `json:"pid"`
	HTTPPort  int    `json:"httpPort"`
	HTTPSPort int    `json:"httpsPort"`
	LSVersion string `json:"lsVersion"`
	CSRFToken string `json:"csrfToken"`
	FilePath  string `json:"filePath,omitempty"`
}

// Instance represents one discovered Antigravity desktop/IDE/sandbox slot.
type Instance struct {
	ID                string   `json:"id"`
	Label             string   `json:"label"`
	Kind              string   `json:"kind"` // main | ide | profile
	ReadOnlyLifecycle bool     `json:"readOnlyLifecycle"`
	AppPath           string   `json:"appPath"`
	UserDataDir       string   `json:"userDataDir"`
	HomeDir           string   `json:"homeDir"`
	GeminiDirs        []string `json:"geminiDirs"`
	PrimaryGeminiDir  string   `json:"primaryGeminiDir"`
	ProfileDir        string   `json:"profileDir,omitempty"`
	Description       string   `json:"description,omitempty"`
	Running           bool     `json:"running"`
	MainPID           int      `json:"mainPid,omitempty"`
	Hub               *HubInfo `json:"hub,omitempty"`
	CDPPort           int      `json:"cdpPort,omitempty"`
	Account           string   `json:"account,omitempty"`
	AuthState         string   `json:"authState"` // ok | expired | missing | keychain_managed | no_refresh_token
	AccessTokenExp    string   `json:"accessTokenExp,omitempty"`
	HasRefreshToken   bool     `json:"hasRefreshToken"`
	BrokenSymlinks    int      `json:"brokenSymlinks"`
}

type procEntry struct {
	PID     int
	PPID    int
	Command string
}

// DiscoverAll scans main, ide, and all ParaGravity profiles on disk and pairs
// them with live OS processes, daemon hubs, and CDP ports.
func DiscoverAll(cfg config.Config) []Instance {
	realHome, _ := os.UserHomeDir()
	procs := listProcesses()

	mainDataDir := filepath.Join(realHome, "Library", "Application Support", "Antigravity")
	ideDataDir := filepath.Join(realHome, "Library", "Application Support", "Antigravity IDE")
	mainGemini := filepath.Join(realHome, ".gemini", "antigravity")
	ideGemini := filepath.Join(realHome, ".gemini", "antigravity-ide")

	// Both Antigravity.app and Antigravity IDE.app write hub_*.json under ~/.gemini/antigravity/daemon.
	// Inspect each live hub PID's command line to assign it cleanly without mixing main and ide.
	mainHub, ideHub := splitMainAndIDEHubs(filepath.Join(mainGemini, "daemon"), procs)

	mainPID := findMainAppPID(procs, cfg.Box.MainAppPath, mainDataDir)
	idePID := findIDEAppPID(procs, cfg.Box.IDEAppPath, ideDataDir)

	var ideGeminiDirs []string
	if dirExists(ideGemini) {
		ideGeminiDirs = []string{ideGemini}
	}

	mainInst := Instance{
		ID:                "main",
		Label:             "Main · Ultra",
		Kind:              KindMain,
		ReadOnlyLifecycle: true,
		AppPath:           cfg.Box.MainAppPath,
		UserDataDir:       mainDataDir,
		HomeDir:           realHome,
		GeminiDirs:        existingDirs([]string{mainGemini}),
		PrimaryGeminiDir:  mainGemini,
		Running:           mainPID > 0 || mainHub != nil,
		MainPID:           mainPID,
		Hub:               mainHub,
		CDPPort:           readDevToolsPort(mainDataDir),
		AuthState:         "keychain_managed",
	}

	ideInst := Instance{
		ID:                "ide",
		Label:             "Antigravity IDE",
		Kind:              KindIDE,
		ReadOnlyLifecycle: true,
		AppPath:           cfg.Box.IDEAppPath,
		UserDataDir:       ideDataDir,
		HomeDir:           realHome,
		GeminiDirs:        ideGeminiDirs,
		PrimaryGeminiDir:  ideGemini,
		Running:           idePID > 0 || ideHub != nil,
		MainPID:           idePID,
		Hub:               ideHub,
		CDPPort:           readDevToolsPort(ideDataDir),
		AuthState:         "keychain_managed",
	}

	out := []Instance{mainInst}
	if dirExists(cfg.Box.IDEAppPath) || ideInst.Running || len(ideGeminiDirs) > 0 {
		out = append(out, ideInst)
	}

	profilesDir := config.ExpandPath(cfg.Box.ProfilesDir)
	entries, err := os.ReadDir(profilesDir)
	if err == nil {
		var names []string
		for _, ent := range entries {
			if !ent.IsDir() {
				continue
			}
			pdir := filepath.Join(profilesDir, ent.Name())
			if fileExists(filepath.Join(pdir, "profile.json")) {
				names = append(names, ent.Name())
			}
		}
		sort.Strings(names)
		boxBin := filepath.Join(cfg.Box.BoxAppPath, "Contents", "MacOS", "Antigravity")
		appPath := cfg.Box.BoxAppPath
		if !fileExists(boxBin) {
			appPath = cfg.Box.MainAppPath
		}
		for _, name := range names {
			pdir := filepath.Join(profilesDir, name)
			out = append(out, discoverProfile(pdir, name, appPath, procs))
		}
	}

	return out
}

func discoverProfile(profileDir, name, appPath string, procs []procEntry) Instance {
	homeDir := filepath.Join(profileDir, "home")
	dataDir := filepath.Join(profileDir, "data")
	meta := readProfileMeta(profileDir)

	boxGemini := filepath.Join(homeDir, ".gemini", "antigravitybox")
	legacyGemini := filepath.Join(homeDir, ".gemini", "antigravity")
	geminiDirs := existingDirs([]string{boxGemini, legacyGemini})
	primaryGemini := boxGemini
	if !dirExists(boxGemini) && dirExists(legacyGemini) {
		primaryGemini = legacyGemini
	}

	pid := findProfilePID(profileDir, dataDir, procs)
	hub := findLatestAliveHub(
		[]string{
			filepath.Join(boxGemini, "daemon"),
			filepath.Join(legacyGemini, "daemon"),
		},
		procs,
	)

	acc := ExtractAccountInfo(profileDir)
	authState := "missing"
	if acc.HasRefreshToken {
		authState = "ok"
	} else if acc.HasToken {
		if tokenIsExpired(acc.TokenExp) {
			authState = "expired"
		} else {
			authState = "no_refresh_token"
		}
	}

	return Instance{
		ID:                name,
		Label:             strings.ToUpper(name),
		Kind:              KindProfile,
		ReadOnlyLifecycle: false,
		AppPath:           appPath,
		UserDataDir:       dataDir,
		HomeDir:           homeDir,
		GeminiDirs:        geminiDirs,
		PrimaryGeminiDir:  primaryGemini,
		ProfileDir:        profileDir,
		Description:       meta.Description,
		Running:           pid > 0 || hub != nil,
		MainPID:           pid,
		Hub:               hub,
		CDPPort:           readDevToolsPort(dataDir),
		Account:           acc.Email,
		AuthState:         authState,
		AccessTokenExp:    acc.TokenExp,
		HasRefreshToken:   acc.HasRefreshToken,
		BrokenSymlinks:    countBrokenSymlinks(homeDir),
	}
}

func splitMainAndIDEHubs(daemonDir string, procs []procEntry) (mainHub *HubInfo, ideHub *HubInfo) {
	hubs := readAliveHubs(daemonDir, procs)
	procMap := make(map[int]string, len(procs))
	for _, p := range procs {
		procMap[p.PID] = p.Command
	}
	for _, h := range hubs {
		cmd := procMap[h.PID]
		isIDE := strings.Contains(cmd, "Antigravity IDE.app") ||
			strings.Contains(cmd, "antigravity-ide") ||
			strings.Contains(cmd, "--app_data_dir antigravity-ide")
		if isIDE {
			if ideHub == nil {
				cp := h
				ideHub = &cp
			}
		} else {
			if mainHub == nil {
				cp := h
				mainHub = &cp
			}
		}
	}
	return mainHub, ideHub
}

func findLatestAliveHub(daemonDirs []string, procs []procEntry) *HubInfo {
	for _, d := range daemonDirs {
		hubs := readAliveHubs(d, procs)
		if len(hubs) > 0 {
			cp := hubs[0]
			return &cp
		}
	}
	return nil
}

func readAliveHubs(daemonDir string, procs []procEntry) []HubInfo {
	files, err := filepath.Glob(filepath.Join(daemonDir, "hub_*.json"))
	if err != nil || len(files) == 0 {
		return nil
	}
	type candidate struct {
		hub   HubInfo
		mtime time.Time
	}
	procAlive := make(map[int]bool, len(procs))
	for _, p := range procs {
		procAlive[p.PID] = true
	}

	var list []candidate
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var h HubInfo
		if err := json.Unmarshal(raw, &h); err != nil || h.PID <= 0 || h.HTTPPort <= 0 {
			continue
		}
		if len(procs) > 0 {
			if !procAlive[h.PID] {
				continue
			}
		} else if !IsPIDAlive(h.PID) {
			continue
		}
		h.FilePath = f
		info, _ := os.Stat(f)
		var mt time.Time
		if info != nil {
			mt = info.ModTime()
		}
		list = append(list, candidate{hub: h, mtime: mt})
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].mtime.After(list[j].mtime)
	})
	out := make([]HubInfo, 0, len(list))
	for _, c := range list {
		out = append(out, c.hub)
	}
	return out
}

func listProcesses() []procEntry {
	cmd := exec.Command("ps", "-axo", "pid=,ppid=,command=")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	lines := strings.Split(string(out), "\n")
	res := make([]procEntry, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		pid, err1 := strconv.Atoi(fields[0])
		ppid, err2 := strconv.Atoi(fields[1])
		if err1 != nil || err2 != nil {
			continue
		}
		// Advance past pid and ppid fields cleanly without matching digits inside pid.
		rest := strings.TrimSpace(line[len(fields[0]):])
		rest = strings.TrimSpace(rest[len(fields[1]):])
		res = append(res, procEntry{PID: pid, PPID: ppid, Command: rest})
	}
	return res
}

func findMainAppPID(procs []procEntry, mainAppPath, _ string) int {
	binSuffix := filepath.Join(mainAppPath, "Contents", "MacOS", "Antigravity")
	for _, p := range procs {
		if strings.HasPrefix(p.Command, binSuffix) &&
			!strings.Contains(p.Command, "Helper") &&
			!strings.Contains(p.Command, "--user-data-dir=") {
			return p.PID
		}
	}
	return 0
}

func findIDEAppPID(procs []procEntry, ideAppPath, _ string) int {
	binSuffix := filepath.Join(ideAppPath, "Contents", "MacOS", "Electron")
	for _, p := range procs {
		if strings.HasPrefix(p.Command, binSuffix) && !strings.Contains(p.Command, "Helper") {
			return p.PID
		}
	}
	return 0
}

func findProfilePID(profileDir, dataDir string, procs []procEntry) int {
	resolvedData, err := filepath.EvalSymlinks(dataDir)
	if err != nil {
		resolvedData = filepath.Clean(dataDir)
	}
	marker := "--user-data-dir=" + resolvedData
	markerRaw := "--user-data-dir=" + filepath.Clean(dataDir)

	if raw, err := os.ReadFile(filepath.Join(profileDir, "run.pid")); err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil && pid > 0 {
			for _, p := range procs {
				if p.PID == pid && (strings.Contains(p.Command, marker) || strings.Contains(p.Command, markerRaw) || strings.Contains(p.Command, "Antigravity")) {
					return pid
				}
			}
		}
	}

	reMarker := regexp.MustCompile(`--user-data-dir=(` + regexp.QuoteMeta(resolvedData) + `|` + regexp.QuoteMeta(filepath.Clean(dataDir)) + `)( |$)`)
	for _, p := range procs {
		if !strings.Contains(p.Command, "--type=") && reMarker.MatchString(p.Command) {
			return p.PID
		}
	}
	return 0
}

// IsPIDAlive reports whether a process with the given PID exists.
func IsPIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

func readDevToolsPort(userDataDir string) int {
	if userDataDir == "" {
		return 0
	}
	raw, err := os.ReadFile(filepath.Join(userDataDir, "DevToolsActivePort"))
	if err != nil {
		return 0
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) == 0 {
		return 0
	}
	port, err := strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil || port <= 0 || port > 65535 {
		return 0
	}
	return port
}

func existingDirs(candidates []string) []string {
	var out []string
	seen := make(map[string]bool)
	for _, d := range candidates {
		if d == "" || seen[d] {
			continue
		}
		if dirExists(d) {
			seen[d] = true
			out = append(out, d)
		}
	}
	return out
}

func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

func countBrokenSymlinks(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	broken := 0
	for _, ent := range entries {
		full := filepath.Join(dir, ent.Name())
		linfo, err := os.Lstat(full)
		if err != nil || linfo.Mode()&os.ModeSymlink == 0 {
			continue
		}
		if _, err := os.Stat(full); err != nil {
			broken++
		}
	}
	return broken
}

func tokenIsExpired(expStr string) bool {
	if expStr == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339Nano, expStr)
	if err != nil {
		t, err = time.Parse(time.RFC3339, expStr)
		if err != nil {
			return false
		}
	}
	return t.Before(time.Now())
}
