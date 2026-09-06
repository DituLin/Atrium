package webui

import "embed"

// Dist holds the Vite build output copied from web/dist by `make web-sync`.
// The directory always contains .gitkeep so the embed pattern stays valid
// before the first web build.
//
//go:embed all:dist
var Dist embed.FS
