//go:build !windows

package credential

import "os"

func auditPermissions(path string, info os.FileInfo) []Finding {
	want := os.FileMode(0o600)
	problem, hint := "file_mode", "restrict_file"
	if info.IsDir() {
		want = 0o700
		problem, hint = "directory_mode", "restrict_directory"
	}
	if info.Mode().Perm() == want && info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) == 0 {
		return nil
	}
	return []Finding{{Path: path, Problem: problem, Hint: hint, Exposed: info.Mode().Perm()&0o077 != 0}}
}
