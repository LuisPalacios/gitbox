//go:build !serverbin

package serverbin

import "embed"

// A build without the serverbin tag carries no server: no identity, no
// targets and an empty file system.
const (
	Version = ""
	Commit  = ""
)

var (
	table []Target
	files embed.FS
)
