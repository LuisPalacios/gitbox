package pathkey

import (
	"os"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

// Darwin declares _PC_CASE_SENSITIVE in sys/unistd.h. Query the volume,
// including APFS volumes formatted with the non-default case policy.
const pcCaseSensitive = 11

var volumes = struct {
	sync.Mutex
	fold map[int32]bool
}{fold: map[int32]bool{}}

const fallbackFold = true

func directoryFolds(path, _ string, info os.FileInfo) bool {
	dev := info.Sys().(*syscall.Stat_t).Dev
	volumes.Lock()
	fold, ok := volumes.fold[dev]
	volumes.Unlock()
	if ok {
		return fold
	}
	v, err := unix.Pathconf(path, pcCaseSensitive)
	if err != nil || v < 0 {
		return true
	}
	volumes.Lock()
	defer volumes.Unlock()
	if len(volumes.fold) >= 64 {
		clear(volumes.fold)
	}
	volumes.fold[dev] = v == 0
	return v == 0
}
