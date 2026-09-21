//go:build clinicdesktop

package frontend

import "embed"

// Assets are bundled locally; the desktop app never loads remote UI into its bridge.
//
//go:embed all:dist-desktop
var Assets embed.FS
