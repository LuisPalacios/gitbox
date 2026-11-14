package credential

import (
	"errors"
	"os"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

func auditPermissions(path string, _ os.FileInfo) []Finding {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return []Finding{unverifiableFinding(path)}
	}
	return auditSecurityDescriptor(path, sd)
}

func auditSecurityDescriptor(path string, sd *windows.SECURITY_DESCRIPTOR) []Finding {
	if sd == nil {
		return []Finding{unverifiableFinding(path)}
	}
	dacl, _, err := sd.DACL()
	if errors.Is(err, windows.ERROR_OBJECT_NOT_FOUND) || (err == nil && dacl == nil) {
		// Missing and NULL DACLs allow everyone; an empty ACL allows nobody.
		return []Finding{{Path: path, Problem: "unsafe_acl", Hint: "restrict_acl", Exposed: true}}
	}
	if err != nil {
		return []Finding{unverifiableFinding(path)}
	}
	owner, _, err := sd.Owner()
	if err != nil || owner == nil || !owner.IsValid() {
		return []Finding{unverifiableFinding(path)}
	}
	unknown, exposed := false, false
	for i := uint32(0); i < uint32(dacl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil || ace == nil {
			unknown = true
			continue
		}
		switch ace.Header.AceType {
		case windows.ACCESS_DENIED_ACE_TYPE:
			// This is an allowlist policy, not an effective-access calculation.
			continue
		case windows.ACCESS_ALLOWED_ACE_TYPE:
			// A SID has an eight-byte header followed by its subauthorities.
			const sidOffset = unsafe.Offsetof(windows.ACCESS_ALLOWED_ACE{}.SidStart)
			if uintptr(ace.Header.AceSize) < sidOffset+8 {
				unknown = true
				continue
			}
			sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
			if uintptr(ace.Header.AceSize) < sidOffset+8+4*uintptr(sid.SubAuthorityCount()) || !sid.IsValid() {
				unknown = true
				continue
			}
			if ace.Mask == 0 || sid.Equals(owner) || sid.IsWellKnown(windows.WinLocalSystemSid) || sid.IsWellKnown(windows.WinBuiltinAdministratorsSid) {
				continue
			}
			if ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0 && sid.IsWellKnown(windows.WinCreatorOwnerSid) {
				// Windows replaces CREATOR OWNER with each child's owner.
				continue
			}
			// Include inheritable grants: they can expose future token files.
			exposed = true
		default:
			// Object/callback ACEs need different layouts and interpretation.
			unknown = true
		}
	}
	runtime.KeepAlive(sd)
	findings := []Finding{}
	if exposed {
		findings = append(findings, Finding{Path: path, Problem: "unsafe_acl", Hint: "restrict_acl", Exposed: true})
	}
	if unknown {
		findings = append(findings, unverifiableFinding(path))
	}
	return findings
}
