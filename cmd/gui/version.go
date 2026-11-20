package main

// Build-time variables (set via -ldflags). They live outside main.go so the
// gitboxserver build, which excludes main.go, carries the same stamp.
var (
	version = "dev"
	commit  = "none"
)
