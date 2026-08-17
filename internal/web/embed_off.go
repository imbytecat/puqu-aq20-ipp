//go:build !embed

package web

import "embed"

// Empty without the embed tag; Vite serves the administration UI in development.
var dist embed.FS
