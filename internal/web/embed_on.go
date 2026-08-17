//go:build embed

package web

import "embed"

// dist is the Vite-built SPA, baked in only for production builds
// (`go build -tags embed`, i.e. `mise run server:build`). mise runs web:build
// first, so apps/server/internal/web/dist is populated before this compiles.
//
//go:embed all:dist
var dist embed.FS
