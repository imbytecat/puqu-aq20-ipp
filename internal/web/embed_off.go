//go:build !embed

package web

import "embed"

// dist is empty without the `embed` build tag (go run/test/vet, gopls, a fresh
// clone): no //go:embed directive means no dist/ is needed on disk, so the whole
// repo compiles with zero frontend build and Handler 404s. The SPA is embedded
// only under `-tags embed` (embed_on.go).
var dist embed.FS
