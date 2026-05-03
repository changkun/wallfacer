// Package spa embeds the built frontend distribution for the wallfacer web server.
package spa

import "embed"

// FS holds the embedded frontend dist directory.
//
//go:embed all:dist
var FS embed.FS
