//go:build serverbin

package serverbin

import "embed"

// The binaries scripts/build-server.sh left here. zz_generated.go, written
// in the same run, holds Version, Commit and the table that pins them.
//
//go:embed *.gz
var files embed.FS
