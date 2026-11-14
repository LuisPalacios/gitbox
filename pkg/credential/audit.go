package credential

import (
	"os"
	"path/filepath"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

// Finding describes credential metadata that needs attention. Problem and Hint
// are stable codes for presentation; Exposed distinguishes unsafe permissions
// from metadata the audit could not verify.
type Finding struct {
	Path    string `json:"path"`
	Problem string `json:"problem"`
	Hint    string `json:"hint"`
	Exposed bool   `json:"exposed"`
}

// AuditCredentialDir inspects directory entries and permissions without reading
// credentials or changing permissions. It reports links without following them.
// A missing directory is normal before the first token account exists.
func AuditCredentialDir() []Finding {
	dir := filepath.Join(config.ConfigRoot(), "gitbox", "credentials")
	findings := []Finding{}
	info, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		return findings
	}
	if err != nil {
		return append(findings, unverifiableFinding(dir))
	}
	if finding := auditEntryType(dir, info, true); finding != nil {
		return append(findings, *finding)
	}
	findings = append(findings, auditPermissions(dir, info)...)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return append(findings, unverifiableFinding(dir))
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			// A concurrent writer can remove its temporary entry during the audit.
			continue
		}
		if err != nil {
			findings = append(findings, unverifiableFinding(path))
			continue
		}
		if finding := auditEntryType(path, info, false); finding != nil {
			findings = append(findings, *finding)
			continue
		}
		findings = append(findings, auditPermissions(path, info)...)
	}
	return findings
}

func auditEntryType(path string, info os.FileInfo, directory bool) *Finding {
	if info.Mode()&os.ModeSymlink != 0 {
		return &Finding{Path: path, Problem: "symlink", Hint: "replace_symlink"}
	}
	if (directory && !info.IsDir()) || (!directory && !info.Mode().IsRegular()) {
		return &Finding{Path: path, Problem: "unexpected_type", Hint: "inspect_entry"}
	}
	return nil
}

func unverifiableFinding(path string) Finding {
	return Finding{Path: path, Problem: "unverifiable", Hint: "inspect_permissions"}
}
