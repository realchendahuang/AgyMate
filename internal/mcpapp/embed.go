// Package mcpapp implements AgyMate's stdio MCP server and Antigravity 2.34+ MCP App UI.
package mcpapp

import (
	_ "embed"
	"os"
	"strings"
)

const (
	// AppResourceURI is the ui:// resource identifier served to MCP App hosts.
	AppResourceURI = "ui://agymate/app.html"
	// AppResourceMimeType is the required MIME type for MCP Apps (SEP-1865).
	AppResourceMimeType = "text/html;profile=mcp-app"
)

//go:embed dist/app.html
var embeddedAppHTML string

// LoadAppHTML returns the self-contained MCP App HTML document, supporting
// AGYMATE_MCP_APP_HTML override for rapid local UI iteration.
func LoadAppHTML() string {
	if override := strings.TrimSpace(os.Getenv("AGYMATE_MCP_APP_HTML")); override != "" {
		if raw, err := os.ReadFile(override); err == nil && len(raw) > 0 {
			return string(raw)
		}
	}
	return embeddedAppHTML
}
