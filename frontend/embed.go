// Package frontend embeds AgyMate's zero-bundler Web Dashboard and Sidecar UI assets.
package frontend

import (
	"embed"
	"io/fs"
)

//go:embed index.html style.css app.js logo.svg
var assets embed.FS

// FS returns the embedded frontend root filesystem.
func FS() fs.FS {
	return assets
}
