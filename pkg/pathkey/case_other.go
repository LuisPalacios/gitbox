//go:build !windows && !linux && !darwin

package pathkey

import "os"

const fallbackFold = false

func directoryFolds(_ string, _ string, _ os.FileInfo) bool { return false }
