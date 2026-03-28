//go:build desktop

// Package icons embeds the tray icon assets for the desktop build.
package icons

import _ "embed"

//go:embed tray.png
var Tray []byte

//go:embed tray@2x.png
var Tray2x []byte

//go:embed tray-active.png
var TrayActive []byte

//go:embed tray-active@2x.png
var TrayActive2x []byte

//go:embed tray-attention.png
var TrayAttention []byte

//go:embed tray-attention@2x.png
var TrayAttention2x []byte

//go:embed tray.ico
var TrayICO []byte
