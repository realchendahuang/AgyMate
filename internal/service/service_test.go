package service

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderLaunchdPlist(t *testing.T) {
	home := "/Users/example"
	bin := "/Users/example/.local/bin/agymate"
	logDir := "/Users/example/Library/Logs/agymate"

	plist := RenderLaunchdPlist(bin, home, logDir)
	for _, want := range []string{
		"<string>io.agymate.hub</string>",
		"<string>/Users/example/.local/bin/agymate</string>",
		"<string>serve</string>",
		"<string>/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin:/Users/example/.local/bin</string>",
		"<string>/Users/example/Library/Logs/agymate/agymate-hub.out.log</string>",
	} {
		if !strings.Contains(plist, want) {
			t.Fatalf("plist missing %q:\n%s", want, plist)
		}
	}
	if strings.Contains(plist, "chendahuang") {
		t.Fatalf("plist leaked personal identifier:\n%s", plist)
	}
}

func TestRenderSystemdUnitAndPaths(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-cfg")
	if got := SystemdUnitPath("/home/tester"); got != filepath.Join("/tmp/xdg-cfg", "systemd", "user", "agymate.service") {
		t.Fatalf("unexpected SystemdUnitPath: %q", got)
	}
	if got := LaunchdPlistPath("/Users/example"); got != filepath.Join("/Users/example", "Library", "LaunchAgents", "io.agymate.hub.plist") {
		t.Fatalf("unexpected LaunchdPlistPath: %q", got)
	}

	unit := RenderSystemdUnit("/home/tester/.local/bin/agymate")
	for _, want := range []string{
		`ExecStart="/home/tester/.local/bin/agymate" serve`,
		"Restart=on-failure",
		"WantedBy=default.target",
	} {
		if !strings.Contains(unit, want) {
			t.Fatalf("systemd unit missing %q:\n%s", want, unit)
		}
	}
}

func TestExtractLaunchdSummary(t *testing.T) {
	sample := "gui/501/io.agymate.hub = {\n\tstate = running\n\tpid = 4210\n}\n"
	if got := extractLaunchdSummary(sample); got != "state=running pid=4210" {
		t.Fatalf("unexpected summary: %q", got)
	}
}
