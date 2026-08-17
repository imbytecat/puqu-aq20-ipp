//go:build embed

package web

import "embed"

//go:embed all:dist
var dist embed.FS
