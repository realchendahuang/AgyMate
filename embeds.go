// Package agymate exposes embedded plugin assets for self-contained distribution.
package agymate

import (
	"embed"
	"io/fs"
)

//go:embed antigravity-plugin
var rawAntigravityPluginFS embed.FS

//go:embed magpie-plugin
var rawMagpiePluginFS embed.FS

// AntigravityPluginFS returns the embedded antigravity-plugin directory tree.
func AntigravityPluginFS() fs.FS {
	sub, err := fs.Sub(rawAntigravityPluginFS, "antigravity-plugin")
	if err != nil {
		return rawAntigravityPluginFS
	}
	return sub
}

// MagpiePluginFS returns the embedded magpie-plugin directory tree.
func MagpiePluginFS() fs.FS {
	sub, err := fs.Sub(rawMagpiePluginFS, "magpie-plugin")
	if err != nil {
		return rawMagpiePluginFS
	}
	return sub
}
