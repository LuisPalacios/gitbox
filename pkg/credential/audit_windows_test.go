package credential

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func auditSecureTestPath(t *testing.T, path string) {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	owner, _, err := sd.Owner()
	if err != nil || owner == nil {
		t.Fatalf("reading fixture owner: %v", err)
	}
	auditSetTestDACL(t, path, "D:P(A;OICI;FA;;;"+owner.String()+")(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)")
}

func auditSetTestDACL(t *testing.T, path, sddl string) {
	t.Helper()
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
}

func TestAuditCredentialDir_WindowsACL(t *testing.T) {
	dir := auditTestDir(t)
	file := filepath.Join(dir, "example")
	if err := os.WriteFile(file, []byte("fixture-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	auditSecureTestPath(t, file)
	if got := AuditCredentialDir(); len(got) != 0 {
		t.Fatalf("private DACL fixtures: %+v", got)
	}
	for _, path := range []string{file, dir} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			auditSetTestDACL(t, path, "D:P(A;;FA;;;SY)(A;;FA;;;BA)(A;;FA;;;WD)")
			t.Cleanup(func() { auditSecureTestPath(t, path) })
			got := AuditCredentialDir()
			if len(got) != 1 || got[0].Path != path || got[0].Problem != "unsafe_acl" || !got[0].Exposed {
				t.Fatalf("exposed DACL findings: %+v", got)
			}
		})
	}
}

func TestAuditSecurityDescriptor_WindowsPolicy(t *testing.T) {
	const owner = "S-1-5-21-1-2-3-1001"
	for _, tc := range []struct {
		name    string
		dacl    string
		problem string
		exposed bool
	}{
		{"owner-system-admin", "D:P(A;;FA;;;" + owner + ")(A;;FA;;;SY)(A;;FA;;;BA)", "", false},
		{"everyone", "D:P(A;;FR;;;WD)", "unsafe_acl", true},
		{"inherited-users", "D:P(A;ID;FR;;;BU)", "unsafe_acl", true},
		{"inherit-only-users", "D:P(A;OIIO;FR;;;BU)", "unsafe_acl", true},
		{"creator-owner", "D:P(A;OIIO;FA;;;CO)", "", false},
		{"deny-only", "D:P(D;;FA;;;WD)", "", false},
		{"empty", "D:P", "", false},
		{"null", "D:NO_ACCESS_CONTROL", "unsafe_acl", true},
		{"missing", "", "unsafe_acl", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sd, err := windows.SecurityDescriptorFromString("O:" + owner + tc.dacl)
			if err != nil {
				t.Fatal(err)
			}
			got := auditSecurityDescriptor("fixture", sd)
			if tc.problem == "" {
				if len(got) != 0 {
					t.Fatalf("safe descriptor: %+v", got)
				}
			} else if len(got) != 1 || got[0].Problem != tc.problem || got[0].Exposed != tc.exposed {
				t.Fatalf("descriptor findings: %+v", got)
			}
		})
	}
}

func TestAuditSecurityDescriptor_Unverifiable(t *testing.T) {
	got := auditSecurityDescriptor("fixture", nil)
	if len(got) != 1 || got[0].Problem != "unverifiable" || got[0].Exposed {
		t.Fatalf("missing descriptor: %+v", got)
	}
	sd, err := windows.SecurityDescriptorFromString("O:SYD:P(A;;FA;;;SY)")
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	var ace *windows.ACCESS_ALLOWED_ACE
	if err := windows.GetAce(dacl, 0, &ace); err != nil {
		t.Fatal(err)
	}
	ace.Header.AceType = 2 // A non-allow/deny ACE needs different interpretation.
	got = auditSecurityDescriptor("fixture", sd)
	if len(got) != 1 || got[0].Problem != "unverifiable" || got[0].Exposed {
		t.Fatalf("unsupported ACE: %+v", got)
	}
}
