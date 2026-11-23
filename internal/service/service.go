// Package service installs, removes, restarts, and inspects the AgyMate Hub
// background daemon across macOS (launchd) and Linux (systemd --user).
package service

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	// Label is the neutral launchd agent identifier for AgyMate Hub.
	Label = "io.agymate.hub"
	// LegacyLabel was used in earlier personal builds and is migrated automatically.
	LegacyLabel = "com.realchendahuang.agymate.hub"
	// SystemdUnitName is the user-level systemd service name on Linux.
	SystemdUnitName = "agymate.service"
)

// StatusInfo describes the current background service installation and runtime state.
type StatusInfo struct {
	Supported      bool   `json:"supported"`
	Manager        string `json:"manager"` // launchd | systemd | unsupported
	Label          string `json:"label"`
	UnitPath       string `json:"unitPath,omitempty"`
	Installed      bool   `json:"installed"`
	Running        bool   `json:"running"`
	LegacyMigrated bool   `json:"legacyMigrated,omitempty"`
	Detail         string `json:"detail,omitempty"`
}

// ResolveExecutable returns the canonical absolute path of binPath or the current executable.
func ResolveExecutable(binPath string) (string, error) {
	p := strings.TrimSpace(binPath)
	if p == "" {
		exe, err := os.Executable()
		if err != nil {
			return "", fmt.Errorf("resolve current executable: %w", err)
		}
		p = exe
	}
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, p[2:])
		}
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	if realPath, err := filepath.EvalSymlinks(abs); err == nil {
		return realPath, nil
	}
	return abs, nil
}

// LaunchdPlistPath returns the macOS LaunchAgent plist path for io.agymate.hub.
func LaunchdPlistPath(homeDir string) string {
	return filepath.Join(homeDir, "Library", "LaunchAgents", Label+".plist")
}

// LegacyLaunchdPlistPath returns the legacy macOS LaunchAgent plist path for cleanup.
func LegacyLaunchdPlistPath(homeDir string) string {
	return filepath.Join(homeDir, "Library", "LaunchAgents", LegacyLabel+".plist")
}

// SystemdUnitPath returns the Linux user systemd unit path (~/.config/systemd/user/agymate.service).
func SystemdUnitPath(homeDir string) string {
	cfgHome := os.Getenv("XDG_CONFIG_HOME")
	if cfgHome == "" {
		cfgHome = filepath.Join(homeDir, ".config")
	}
	return filepath.Join(cfgHome, "systemd", "user", SystemdUnitName)
}

// RenderLaunchdPlist renders a macOS LaunchAgent plist running `<binPath> serve`.
func RenderLaunchdPlist(binPath, homeDir, logDir string) string {
	if logDir == "" {
		logDir = filepath.Join(homeDir, "Library", "Logs", "agymate")
	}
	pathEnv := "/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin:" + filepath.Join(homeDir, ".local", "bin")
	outLog := filepath.Join(logDir, "agymate-hub.out.log")
	errLog := filepath.Join(logDir, "agymate-hub.err.log")

	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>serve</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>EnvironmentVariables</key>
  <dict>
    <key>PATH</key>
    <string>%s</string>
  </dict>
  <key>StandardOutPath</key>
  <string>%s</string>
  <key>StandardErrorPath</key>
  <string>%s</string>
</dict>
</plist>
`, xmlEscape(Label), xmlEscape(binPath), xmlEscape(pathEnv), xmlEscape(outLog), xmlEscape(errLog))
}

// RenderSystemdUnit renders a Linux systemd --user service unit running `<binPath> serve`.
func RenderSystemdUnit(binPath string) string {
	return fmt.Sprintf(`[Unit]
Description=AgyMate Local Hub
After=network.target

[Service]
Type=simple
ExecStart="%s" serve
Restart=on-failure
RestartSec=3

[Install]
WantedBy=default.target
`, strings.ReplaceAll(binPath, `"`, `\"`))
}

func xmlEscape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// Install writes/updates the OS user daemon definition, migrates legacy units, and starts the Hub.
func Install(binPath string) (string, error) {
	bin, err := ResolveExecutable(binPath)
	if err != nil {
		return "", err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch runtime.GOOS {
	case "darwin":
		return installLaunchd(bin, home)
	case "linux":
		return installSystemd(bin, home)
	default:
		return "", fmt.Errorf("当前系统 (%s) 暂不支持后台服务自动托管，请直接运行 `%s serve`", runtime.GOOS, bin)
	}
}

// Uninstall stops and removes the AgyMate Hub background service (plus any legacy plist).
func Uninstall() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch runtime.GOOS {
	case "darwin":
		return uninstallLaunchd(home)
	case "linux":
		return uninstallSystemd(home)
	default:
		return "", fmt.Errorf("当前系统 (%s) 暂不支持后台服务卸载", runtime.GOOS)
	}
}

// Restart reloads or restarts the installed background Hub service.
func Restart() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch runtime.GOOS {
	case "darwin":
		domain := "gui/" + strconv.Itoa(os.Getuid())
		target := domain + "/" + Label
		plistPath := LaunchdPlistPath(home)
		if exec.Command("launchctl", "print", target).Run() != nil {
			if _, statErr := os.Stat(plistPath); statErr != nil {
				return "", fmt.Errorf("后台服务尚未安装 (%s)，请先运行 `agymate service install`", plistPath)
			}
			if out, err := exec.Command("launchctl", "bootstrap", domain, plistPath).CombinedOutput(); err != nil {
				return "", fmt.Errorf("launchctl bootstrap 失败: %w (%s)", err, strings.TrimSpace(string(out)))
			}
		}
		if out, err := exec.Command("launchctl", "kickstart", "-k", target).CombinedOutput(); err != nil {
			return "", fmt.Errorf("launchctl kickstart 失败: %w (%s)", err, strings.TrimSpace(string(out)))
		}
		return fmt.Sprintf("已重启后台服务 %s (launchd)", Label), nil
	case "linux":
		if out, err := exec.Command("systemctl", "--user", "restart", SystemdUnitName).CombinedOutput(); err != nil {
			return "", fmt.Errorf("systemctl restart 失败: %w (%s)", err, strings.TrimSpace(string(out)))
		}
		return fmt.Sprintf("已重启后台服务 %s (systemd --user)", SystemdUnitName), nil
	default:
		return "", fmt.Errorf("当前系统 (%s) 暂不支持后台服务重启", runtime.GOOS)
	}
}

// Status inspects whether the AgyMate Hub user service is installed and running.
func Status() (StatusInfo, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return StatusInfo{}, err
	}
	switch runtime.GOOS {
	case "darwin":
		plistPath := LaunchdPlistPath(home)
		legacyPath := LegacyLaunchdPlistPath(home)
		_, stErr := os.Stat(plistPath)
		_, legacyErr := os.Stat(legacyPath)
		domain := "gui/" + strconv.Itoa(os.Getuid())
		target := domain + "/" + Label
		out, printErr := exec.Command("launchctl", "print", target).CombinedOutput()
		info := StatusInfo{
			Supported:      true,
			Manager:        "launchd",
			Label:          Label,
			UnitPath:       plistPath,
			Installed:      stErr == nil,
			Running:        printErr == nil,
			LegacyMigrated: legacyErr != nil,
		}
		if printErr == nil {
			info.Detail = extractLaunchdSummary(string(out))
		} else if legacyErr == nil {
			info.Detail = "检测到旧版 plist (" + legacyPath + ")，请运行 `agymate service install` 自动迁移"
		}
		return info, nil
	case "linux":
		unitPath := SystemdUnitPath(home)
		_, stErr := os.Stat(unitPath)
		out, activeErr := exec.Command("systemctl", "--user", "is-active", SystemdUnitName).CombinedOutput()
		state := strings.TrimSpace(string(out))
		return StatusInfo{
			Supported: true,
			Manager:   "systemd",
			Label:     SystemdUnitName,
			UnitPath:  unitPath,
			Installed: stErr == nil,
			Running:   activeErr == nil && state == "active",
			Detail:    state,
		}, nil
	default:
		return StatusInfo{
			Supported: false,
			Manager:   "unsupported",
			Label:     Label,
			Detail:    "当前系统 (" + runtime.GOOS + ") 请直接运行 `agymate serve`",
		}, nil
	}
}

func installLaunchd(bin, home string) (string, error) {
	domain := "gui/" + strconv.Itoa(os.Getuid())
	migrated := cleanupLegacyLaunchd(domain, home)

	logDir := filepath.Join(home, "Library", "Logs", "agymate")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return "", err
	}
	plistPath := LaunchdPlistPath(home)
	if err := os.MkdirAll(filepath.Dir(plistPath), 0o755); err != nil {
		return "", err
	}
	content := RenderLaunchdPlist(bin, home, logDir)
	if err := os.WriteFile(plistPath, []byte(content), 0o644); err != nil {
		return "", err
	}

	target := domain + "/" + Label
	if exec.Command("launchctl", "print", target).Run() == nil {
		_ = exec.Command("launchctl", "bootout", target).Run()
		for i := 0; i < 45; i++ {
			if exec.Command("launchctl", "print", target).Run() != nil {
				break
			}
			time.Sleep(150 * time.Millisecond)
		}
	}
	var lastOut []byte
	var lastErr error
	for i := 0; i < 5; i++ {
		lastOut, lastErr = exec.Command("launchctl", "bootstrap", domain, plistPath).CombinedOutput()
		if lastErr == nil || exec.Command("launchctl", "print", target).Run() == nil {
			lastErr = nil
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if lastErr != nil && exec.Command("launchctl", "print", target).Run() != nil {
		return "", fmt.Errorf("launchctl bootstrap 失败: %w (%s)", lastErr, strings.TrimSpace(string(lastOut)))
	}
	_ = exec.Command("launchctl", "kickstart", "-k", target).Run()
	if migrated {
		return fmt.Sprintf("已安装并启动后台服务 %s（已从旧版 %s 平滑迁移）", plistPath, LegacyLabel), nil
	}
	return fmt.Sprintf("已安装并启动后台服务 %s", plistPath), nil
}

func uninstallLaunchd(home string) (string, error) {
	domain := "gui/" + strconv.Itoa(os.Getuid())
	migrated := cleanupLegacyLaunchd(domain, home)
	plistPath := LaunchdPlistPath(home)
	_ = exec.Command("launchctl", "bootout", domain+"/"+Label).Run()
	err := os.Remove(plistPath)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if migrated {
		return fmt.Sprintf("已移除后台服务 %s 及旧版 %s", Label, LegacyLabel), nil
	}
	return "已移除后台服务 " + Label, nil
}

func cleanupLegacyLaunchd(domain, home string) bool {
	legacyPath := LegacyLaunchdPlistPath(home)
	_, err := os.Stat(legacyPath)
	hadFile := err == nil
	loaded := exec.Command("launchctl", "print", domain+"/"+LegacyLabel).Run() == nil
	if loaded {
		_ = exec.Command("launchctl", "bootout", domain+"/"+LegacyLabel).Run()
	}
	if hadFile {
		_ = os.Remove(legacyPath)
	}
	return hadFile || loaded
}

func installSystemd(bin, home string) (string, error) {
	unitPath := SystemdUnitPath(home)
	if err := os.MkdirAll(filepath.Dir(unitPath), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(unitPath, []byte(RenderSystemdUnit(bin)), 0o644); err != nil {
		return "", err
	}
	if out, err := exec.Command("systemctl", "--user", "daemon-reload").CombinedOutput(); err != nil {
		return "", fmt.Errorf("systemctl daemon-reload failed: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	if out, err := exec.Command("systemctl", "--user", "enable", "--now", SystemdUnitName).CombinedOutput(); err != nil {
		return "", fmt.Errorf("systemctl enable --now failed: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	_ = exec.Command("systemctl", "--user", "restart", SystemdUnitName).Run()
	return fmt.Sprintf("installed and started %s", unitPath), nil
}

func uninstallSystemd(home string) (string, error) {
	_ = exec.Command("systemctl", "--user", "disable", "--now", SystemdUnitName).Run()
	unitPath := SystemdUnitPath(home)
	err := os.Remove(unitPath)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
	return "removed systemd user service " + SystemdUnitName, nil
}

func extractLaunchdSummary(out string) string {
	var state, pid string
	for _, line := range strings.Split(out, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "state = ") {
			state = strings.TrimPrefix(t, "state = ")
		} else if strings.HasPrefix(t, "pid = ") {
			pid = strings.TrimPrefix(t, "pid = ")
		}
	}
	if pid != "" && state != "" {
		return fmt.Sprintf("state=%s pid=%s", state, pid)
	}
	if state != "" {
		return "state=" + state
	}
	return "loaded"
}
