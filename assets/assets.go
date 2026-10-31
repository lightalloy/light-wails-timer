// Package assets embeds the tray icons so the binary is self-contained.
package assets

import _ "embed"

// IconIdle is shown when no countdown is running.
//
//go:embed icon-idle.png
var IconIdle []byte

// IconActive is shown while a countdown is running.
//
//go:embed icon-active.png
var IconActive []byte
