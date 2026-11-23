// Package instances discovers and manages all local Antigravity instances:
// main (Antigravity), optional ide (Antigravity IDE), isolated profiles, and ad-hoc --user-data-dir instances.
package instances

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/realchendahuang/AgyMate/internal/config"
)

const (
	KindMain    = "main"
	KindIDE     = "ide"
	KindProfile = "profile"
	KindCustom  = "custom"
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
	Kind              string   `json:"kind"` // main | ide | profile | custom
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

var processLister = listProcesses

// DiscoverAll scans main, optional ide, isolated profiles on disk, and any running
// ad-hoc Antigravity processes launched with --user-data-dir=.
func DiscoverAll(cfg config.Config) []Instance {
	realHome, _ := os.UserHomeDir()
	procs := processLister()

	mainAppPath := config.ExpandPath(cfg.Paths.MainAppPath)
	ideAppPath := config.ExpandPath(cfg.Paths.IDEAppPath)
	profilesDir := config.ExpandPath(cfg.Paths.ProfilesDir)

	mainDataDir, ideDataDir := defaultUserDataDirs(realHome)
	mainGemini := filepath.Join(realHome, ".gemini", "antigravity")
	cliGemini := filepath.Join(realHome, ".gemini", "antigravity-cli")
	ideGemini := filepath.Join(realHome, ".gemini", "antigravity-ide")

	mainPID := findMainAppPID(procs, mainAppPath, mainDataDir)
	idePID := findIDEAppPID(procs, ideAppPath, ideDataDir)

	var ideGeminiDirs []string
	if dirExists(ideGemini) {
		ideGeminiDirs = []string{ideGemini}
	}

	mainEmail := extractEmailFromUserDataDir(mainDataDir)
	if mainEmail == "" {
		mainEmail = extractAccountFromGeminiRoot(filepath.Join(realHome, ".gemini")).Email
	}
	ideEmail := extractEmailFromUserDataDir(ideDataDir)

	mainInst := Instance{
		ID:                "main",
		Label:             "Antigravity",
		Kind:              KindMain,
		ReadOnlyLifecycle: true,
		AppPath:           mainAppPath,
		UserDataDir:       mainDataDir,
		HomeDir:           realHome,
		GeminiDirs:        existingDirs([]string{mainGemini, cliGemini}),
		PrimaryGeminiDir:  mainGemini,
		MainPID:           mainPID,
		CDPPort:           readDevToolsPort(mainDataDir),
		Account:           mainEmail,
		AuthState:         "keychain_managed",
	}

	ideInst := Instance{
		ID:                "ide",
		Label:             "Antigravity IDE",
		Kind:              KindIDE,
		ReadOnlyLifecycle: true,
		AppPath:           ideAppPath,
		UserDataDir:       ideDataDir,
		HomeDir:           realHome,
		GeminiDirs:        ideGeminiDirs,
		PrimaryGeminiDir:  ideGemini,
		MainPID:           idePID,
		CDPPort:           readDevToolsPort(ideDataDir),
		Account:           ideEmail,
		AuthState:         "keychain_managed",
	}

	var profileInsts []Instance
	entries, err := os.ReadDir(profilesDir)
	if err == nil {
		var names []string
		for _, ent := range entries {
			if !ent.IsDir() {
				continue
			}
			pdir := filepath.Join(profilesDir, ent.Name())
			if fileExists(filepath.Join(pdir, "profile.json")) ||
				dirExists(filepath.Join(pdir, "data")) ||
				dirExists(filepath.Join(pdir, "home")) {
				names = append(names, ent.Name())
			}
		}
		sort.Strings(names)
		for _, name := range names {
			pdir := filepath.Join(profilesDir, name)
			profileInsts = append(profileInsts, discoverProfile(pdir, name, mainAppPath, procs))
		}
	}

	known := make([]Instance, 0, 2+len(profileInsts))
	known = append(known, mainInst, ideInst)
	known = append(known, profileInsts...)

	adhocInsts := discoverAdhocInstances(procs, known, mainAppPath)
	adhocPtrs := make([]*Instance, len(adhocInsts))
	for i := range adhocInsts {
		adhocPtrs[i] = &adhocInsts[i]
	}

	// Both Antigravity and Antigravity IDE may write hub_*.json under ~/.gemini/antigravity/daemon.
	// Inspect process ancestry across live PIDs to assign each hub cleanly without mixing main, ide, or custom.
	sharedHubs := readAliveHubsFromDirs([]string{
		filepath.Join(mainGemini, "daemon"),
		filepath.Join(ideGemini, "daemon"),
	}, procs)
	mainHub, ideHub := assignHubsByAncestry(procs, sharedHubs, mainPID, idePID, adhocPtrs)

	mainInst.Hub = mainHub
	mainInst.Running = mainPID > 0 || mainHub != nil
	if !mainInst.Running {
		mainInst.CDPPort = 0
	}
	ideInst.Hub = ideHub
	ideInst.Running = idePID > 0 || ideHub != nil
	if !ideInst.Running {
		ideInst.CDPPort = 0
	}

	hasIDE := pathExists(ideAppPath) || ideInst.Running || len(ideGeminiDirs) > 0 || ideInst.Account != ""
	hasOther := hasIDE || len(profileInsts) > 0 || len(adhocInsts) > 0

	var out []Instance
	if pathExists(mainAppPath) || mainInst.Running || mainInst.Account != "" || len(mainInst.GeminiDirs) > 0 || !hasOther {
		out = append(out, mainInst)
	}
	if hasIDE {
		out = append(out, ideInst)
	}
	out = append(out, profileInsts...)
	out = append(out, adhocInsts...)
	return out
}

func defaultUserDataDirs(realHome string) (mainDir, ideDir string) {
	switch runtime.GOOS {
	case "linux":
		cfgHome := os.Getenv("XDG_CONFIG_HOME")
		if cfgHome == "" {
			cfgHome = filepath.Join(realHome, ".config")
		}
		return filepath.Join(cfgHome, "Antigravity"), filepath.Join(cfgHome, "Antigravity IDE")
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = filepath.Join(realHome, "AppData", "Roaming")
		}
		return filepath.Join(appData, "Antigravity"), filepath.Join(appData, "Antigravity IDE")
	default:
		return filepath.Join(realHome, "Library", "Application Support", "Antigravity"),
			filepath.Join(realHome, "Library", "Application Support", "Antigravity IDE")
	}
}

func discoverProfile(profileDir, name, appPath string, procs []procEntry) Instance {
	homeDir := filepath.Join(profileDir, "home")
	dataDir := filepath.Join(profileDir, "data")
	meta := readProfileMeta(profileDir)

	stdGemini := filepath.Join(homeDir, ".gemini", "antigravity")
	cliGemini := filepath.Join(homeDir, ".gemini", "antigravity-cli")
	legacyBoxGemini := filepath.Join(homeDir, ".gemini", "antigravitybox")
	geminiDirs := existingDirs([]string{stdGemini, cliGemini, legacyBoxGemini})
	primaryGemini := stdGemini

	pid := findProfilePID(profileDir, dataDir, procs)
	hub := findLatestAliveHub(
		[]string{
			filepath.Join(stdGemini, "daemon"),
			filepath.Join(legacyBoxGemini, "daemon"),
		},
		procs,
	)

	acc := ExtractAccountInfo(profileDir)
	if acc.Email == "" {
		acc.Email = extractEmailFromUserDataDir(dataDir)
	}
	authState := classifyAuthState(acc, "missing")

	label := strings.TrimSpace(meta.Description)
	if label == "" {
		label = strings.ToUpper(name)
	}

	running := pid > 0 || hub != nil
	cdpPort := 0
	if running {
		cdpPort = readDevToolsPort(dataDir)
	}

	return Instance{
		ID:                name,
		Label:             label,
		Kind:              KindProfile,
		ReadOnlyLifecycle: false,
		AppPath:           appPath,
		UserDataDir:       dataDir,
		HomeDir:           homeDir,
		GeminiDirs:        geminiDirs,
		PrimaryGeminiDir:  primaryGemini,
		ProfileDir:        profileDir,
		Description:       meta.Description,
		Running:           running,
		MainPID:           pid,
		Hub:               hub,
		CDPPort:           cdpPort,
		Account:           acc.Email,
		AuthState:         authState,
		AccessTokenExp:    acc.TokenExp,
		HasRefreshToken:   acc.HasRefreshToken,
		BrokenSymlinks:    countBrokenSymlinks(homeDir),
	}
}

func classifyAuthState(acc AccountInfo, emptyDefault string) string {
	if acc.HasRefreshToken {
		return "ok"
	}
	if acc.HasToken {
		if tokenIsExpired(acc.TokenExp) {
			return "expired"
		}
		return "no_refresh_token"
	}
	return emptyDefault
}

func hasAncestorPID(procByPID map[int]procEntry, pid, targetPID int) bool {
	if pid <= 0 || targetPID <= 0 {
		return false
	}
	if pid == targetPID {
		return true
	}
	cur := pid
	seen := make(map[int]bool, 8)
	for hop := 0; hop < 16 && cur > 1; hop++ {
		if seen[cur] {
			break
		}
		seen[cur] = true
		ent, ok := procByPID[cur]
		if !ok || ent.PPID <= 0 {
			break
		}
		if ent.PPID == targetPID {
			return true
		}
		cur = ent.PPID
	}
	return false
}

func isIDECmd(cmd string) bool {
	return strings.Contains(cmd, "Antigravity IDE.app") ||
		strings.Contains(cmd, "antigravity-ide") ||
		strings.Contains(cmd, "--app_data_dir antigravity-ide") ||
		strings.Contains(cmd, "--app_data_dir=antigravity-ide")
}

func hasIDEInAncestry(procByPID map[int]procEntry, pid int) bool {
	cur := pid
	seen := make(map[int]bool, 8)
	for hop := 0; hop < 16 && cur > 1; hop++ {
		if seen[cur] {
			break
		}
		seen[cur] = true
		ent, ok := procByPID[cur]
		if !ok {
			break
		}
		if isIDECmd(ent.Command) {
			return true
		}
		if ent.PPID <= 1 {
			break
		}
		cur = ent.PPID
	}
	return false
}

func assignHubsByAncestry(procs []procEntry, sharedHubs []HubInfo, mainPID, idePID int, adhoc []*Instance) (mainHub *HubInfo, ideHub *HubInfo) {
	procByPID := make(map[int]procEntry, len(procs))
	for _, p := range procs {
		procByPID[p.PID] = p
	}
	claimed := make(map[int]bool)

	for _, inst := range adhoc {
		if inst.Hub != nil {
			claimed[inst.Hub.PID] = true
			continue
		}
		if inst.MainPID <= 0 {
			continue
		}
		for _, h := range sharedHubs {
			if claimed[h.PID] {
				continue
			}
			if hasAncestorPID(procByPID, h.PID, inst.MainPID) {
				cp := h
				inst.Hub = &cp
				claimed[h.PID] = true
				break
			}
		}
	}

	// Pass 1: strict ancestry matching for IDE and Main.
	for _, h := range sharedHubs {
		if claimed[h.PID] {
			continue
		}
		isIDE := (idePID > 0 && hasAncestorPID(procByPID, h.PID, idePID)) || hasIDEInAncestry(procByPID, h.PID)
		if isIDE {
			claimed[h.PID] = true
			if ideHub == nil {
				cp := h
				ideHub = &cp
			}
			continue
		}
		if mainPID > 0 && hasAncestorPID(procByPID, h.PID, mainPID) {
			claimed[h.PID] = true
			if mainHub == nil {
				cp := h
				mainHub = &cp
			}
		}
	}

	// Pass 2: headless/detached fallback when a hub was reparented to PPID 1.
	if mainHub == nil {
		for _, h := range sharedHubs {
			if claimed[h.PID] {
				continue
			}
			if (idePID > 0 && hasAncestorPID(procByPID, h.PID, idePID)) || hasIDEInAncestry(procByPID, h.PID) {
				continue
			}
			adhocOwned := false
			for _, inst := range adhoc {
				if inst.MainPID > 0 && hasAncestorPID(procByPID, h.PID, inst.MainPID) {
					adhocOwned = true
					break
				}
			}
			if adhocOwned {
				continue
			}
			cp := h
			mainHub = &cp
			claimed[h.PID] = true
			break
		}
	}
	return mainHub, ideHub
}

func readAliveHubsFromDirs(daemonDirs []string, procs []procEntry) []HubInfo {
	byPID := make(map[int]hubCandidate)
	for _, d := range daemonDirs {
		for _, c := range readAliveHubCandidates(d, procs) {
			if prev, ok := byPID[c.hub.PID]; !ok || c.mtime.After(prev.mtime) {
				byPID[c.hub.PID] = c
			}
		}
	}
	list := make([]hubCandidate, 0, len(byPID))
	for _, it := range byPID {
		list = append(list, it)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].mtime.After(list[j].mtime)
	})
	out := make([]HubInfo, 0, len(list))
	for _, it := range list {
		out = append(out, it.hub)
	}
	return out
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

type hubCandidate struct {
	hub   HubInfo
	mtime time.Time
}

func readAliveHubs(daemonDir string, procs []procEntry) []HubInfo {
	cands := readAliveHubCandidates(daemonDir, procs)
	out := make([]HubInfo, 0, len(cands))
	for _, c := range cands {
		out = append(out, c.hub)
	}
	return out
}

func readAliveHubCandidates(daemonDir string, procs []procEntry) []hubCandidate {
	files, err := filepath.Glob(filepath.Join(daemonDir, "hub_*.json"))
	if err != nil || len(files) == 0 {
		return nil
	}
	procAlive := make(map[int]bool, len(procs))
	for _, p := range procs {
		procAlive[p.PID] = true
	}

	var list []hubCandidate
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
		list = append(list, hubCandidate{hub: h, mtime: mt})
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].mtime.After(list[j].mtime)
	})
	return list
}

func findMainAppPID(procs []procEntry, mainAppPath, _ string) int {
	if runtime.GOOS == "darwin" {
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
	for _, p := range procs {
		if strings.Contains(p.Command, "--type=") || strings.Contains(p.Command, "--user-data-dir=") {
			continue
		}
		lower := strings.ToLower(p.Command)
		if (mainAppPath != "" && strings.HasPrefix(p.Command, mainAppPath)) ||
			strings.Contains(lower, "antigravity") {
			if !strings.Contains(lower, "agymate") && !strings.Contains(lower, "language_server") {
				return p.PID
			}
		}
	}
	return 0
}

func findIDEAppPID(procs []procEntry, ideAppPath, _ string) int {
	if ideAppPath == "" {
		return 0
	}
	if runtime.GOOS == "darwin" {
		macOSDir := filepath.Join(ideAppPath, "Contents", "MacOS") + string(filepath.Separator)
		for _, p := range procs {
			if strings.HasPrefix(p.Command, macOSDir) && !strings.Contains(p.Command, "Helper") {
				return p.PID
			}
		}
		return 0
	}
	for _, p := range procs {
		if !strings.Contains(p.Command, "--type=") && strings.HasPrefix(p.Command, ideAppPath) {
			return p.PID
		}
	}
	return 0
}

var nonIDCharsRe = regexp.MustCompile(`[^a-z0-9._-]+`)

func discoverAdhocInstances(procs []procEntry, known []Instance, defaultAppPath string) []Instance {
	knownDirs := make(map[string]bool)
	usedIDs := make(map[string]bool)
	for _, inst := range known {
		usedIDs[inst.ID] = true
		addKnownDir(knownDirs, inst.UserDataDir)
	}

	var extra []Instance
	for _, p := range procs {
		if strings.Contains(p.Command, "--type=") || strings.Contains(p.Command, "Helper") {
			continue
		}
		lower := strings.ToLower(p.Command)
		if !strings.Contains(lower, "antigravity") {
			continue
		}
		udir := extractUserDataDir(p.Command)
		if udir == "" {
			continue
		}
		clean := filepath.Clean(udir)
		resolved, err := filepath.EvalSymlinks(clean)
		if err != nil {
			resolved = clean
		}
		if knownDirs[clean] || knownDirs[resolved] {
			continue
		}
		if !dirExists(clean) {
			continue
		}
		addKnownDir(knownDirs, clean)

		base := filepath.Base(clean)
		if strings.EqualFold(base, "data") {
			if parent := filepath.Base(filepath.Dir(clean)); parent != "" && parent != "." && parent != string(filepath.Separator) {
				base = parent
			}
		}
		slug := strings.Trim(nonIDCharsRe.ReplaceAllString(strings.ToLower(base), "-"), "-.")
		if slug == "" {
			slug = "instance"
		}
		id := "custom-" + slug
		if usedIDs[id] {
			for i := 2; ; i++ {
				cand := id + "-" + strconv.Itoa(i)
				if !usedIDs[cand] {
					id = cand
					break
				}
			}
		}
		usedIDs[id] = true

		inst := Instance{
			ID:                id,
			Label:             base,
			Kind:              KindCustom,
			ReadOnlyLifecycle: true,
			AppPath:           defaultAppPath,
			UserDataDir:       clean,
			Running:           true,
			MainPID:           p.PID,
			CDPPort:           readDevToolsPort(clean),
			Account:           extractEmailFromUserDataDir(clean),
			AuthState:         "keychain_managed",
			Description:       "外部 --user-data-dir 实例",
		}

		// Check if a sibling custom HOME layout holds dedicated .gemini telemetry & oauth credentials.
		for _, candGemini := range []string{
			filepath.Join(filepath.Dir(clean), "home", ".gemini", "antigravity"),
			filepath.Join(filepath.Dir(clean), ".gemini", "antigravity"),
			filepath.Join(clean, ".gemini", "antigravity"),
		} {
			if dirExists(candGemini) {
				geminiRoot := filepath.Dir(candGemini)
				inst.HomeDir = filepath.Dir(geminiRoot)
				inst.PrimaryGeminiDir = candGemini
				inst.GeminiDirs = []string{candGemini}
				acc := extractAccountFromGeminiRoot(geminiRoot)
				if acc.Email != "" {
					inst.Account = acc.Email
				}
				if acc.HasToken || acc.HasRefreshToken {
					inst.AuthState = classifyAuthState(acc, "keychain_managed")
					inst.AccessTokenExp = acc.TokenExp
					inst.HasRefreshToken = acc.HasRefreshToken
				}
				inst.Hub = findLatestAliveHub([]string{filepath.Join(candGemini, "daemon")}, procs)
				break
			}
		}

		extra = append(extra, inst)
	}
	sort.Slice(extra, func(i, j int) bool { return extra[i].ID < extra[j].ID })
	return extra
}

func addKnownDir(m map[string]bool, dir string) {
	if dir == "" {
		return
	}
	clean := filepath.Clean(dir)
	m[clean] = true
	if resolved, err := filepath.EvalSymlinks(clean); err == nil {
		m[resolved] = true
	}
}

func extractUserDataDir(cmd string) string {
	const flag = "--user-data-dir="
	idx := strings.Index(cmd, flag)
	if idx < 0 {
		return ""
	}
	rest := cmd[idx+len(flag):]
	if len(rest) == 0 {
		return ""
	}
	if rest[0] == '"' || rest[0] == '\'' {
		quote := rest[0]
		rest = rest[1:]
		if end := strings.IndexByte(rest, quote); end >= 0 {
			return rest[:end]
		}
		return rest
	}
	if end := strings.IndexAny(rest, " \t\r\n"); end >= 0 {
		return rest[:end]
	}
	return rest
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

func pathExists(p string) bool {
	if strings.TrimSpace(p) == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
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
