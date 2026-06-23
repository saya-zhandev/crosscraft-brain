// Package web embeds the built Vite SPA so the single Go binary serves both the
// UI and the API. The production build (apps/web -> vite build) writes into
// ./dist; a committed dist/.gitkeep keeps this compiling before any build.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var embedded embed.FS

// FS returns the built SPA assets (the dist subtree).
func FS() fs.FS {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic(err) // dist is embedded at compile time; Sub cannot fail in practice
	}
	return sub
}

