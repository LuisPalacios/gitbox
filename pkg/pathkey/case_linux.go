package pathkey

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"unicode"

	"golang.org/x/sys/unix"
)

const fallbackFold = false

// ext4 and f2fs can set the casefold flag per directory. Cache proven
// observations by directory identity rather than across unrelated mounts.
var directories = struct {
	sync.Mutex
	values map[[4]uint64]bool
}{values: map[[4]uint64]bool{}}

func directoryFolds(dir, child string, info os.FileInfo) bool {
	st := info.Sys().(*syscall.Stat_t)
	id := [4]uint64{uint64(st.Dev), uint64(st.Ino), uint64(st.Ctim.Sec), uint64(st.Ctim.Nsec)}
	directories.Lock()
	fold, ok := directories.values[id]
	directories.Unlock()
	if ok {
		return fold
	}
	probe := func(dir, name string) (bool, bool) {
		for i, r := range name {
			alt := unicode.ToUpper(r)
			if alt == r {
				alt = unicode.ToLower(r)
			}
			if alt == r {
				continue
			}
			first, err := os.Lstat(filepath.Join(dir, name))
			if err != nil {
				return false, false
			}
			other, err := os.Lstat(filepath.Join(dir, name[:i]+string(alt)+name[i+len(string(r)):]))
			if errors.Is(err, os.ErrNotExist) {
				return false, true
			}
			if err == nil && !first.IsDir() && first.Sys().(*syscall.Stat_t).Nlink > 1 && os.SameFile(first, other) {
				// Two spellings can be separate hard links on a sensitive disk.
				return false, false
			}
			return err == nil && os.SameFile(first, other), err == nil
		}
		return false, false
	}
	fold, ok = probe(dir, child)
	if !ok {
		f, err := os.Open(dir)
		if err != nil {
			return fallbackFold
		}
		defer f.Close()
		// Empty FAT directories have no names to probe. ext4 and f2fs
		// expose the effective casefold policy even on an empty directory.
		const fsCasefoldFlag = 0x40000000
		var fs unix.Statfs_t
		if unix.Fstatfs(int(f.Fd()), &fs) == nil {
			switch uint32(fs.Type) {
			case unix.EXFAT_SUPER_MAGIC, unix.MSDOS_SUPER_MAGIC:
				fold, ok = true, true
			case unix.EXT4_SUPER_MAGIC, unix.F2FS_SUPER_MAGIC:
				flags, err := unix.IoctlGetInt(int(f.Fd()), unix.FS_IOC_GETFLAGS)
				fold, ok = flags&fsCasefoldFlag != 0, err == nil
			}
		}
		if !ok {
			names, _ := f.Readdirnames(8)
			for _, name := range names {
				if fold, ok = probe(dir, name); ok {
					break
				}
			}
		}
		if !ok {
			parent := filepath.Dir(dir)
			if stat, err := os.Stat(parent); err == nil && parent != dir && stat.Sys().(*syscall.Stat_t).Dev == st.Dev {
				fold, ok = probe(parent, filepath.Base(dir))
			}
		}
	}
	if ok {
		directories.Lock()
		if len(directories.values) >= 512 {
			clear(directories.values)
		}
		directories.values[id] = fold
		directories.Unlock()
	}
	return fold
}
