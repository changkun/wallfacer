//go:build desktop

// Package icons embeds the tray icon assets for the desktop build.
package icons

import _ "embed"

//go:embed tray.png
var Tray []byte

//go:embed tray@2x.png
var Tray2x []byte
