//go:build windows

package update

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// A protected Windows installation needs permission to rename its binary or
// remove its backup. The helper derives every target from its own image.
// Arguments identify a transaction, never a filesystem path.
func elevatedRecovery(r *Recovery, rollback bool) error {
	action := "clean"
	if rollback {
		action = "restore"
	}
	if windows.GetCurrentProcessToken().IsElevated() || !protectedRecovery(r) {
		return errUpdateHandoff
	}
	process, err := shellExecuteUpdate(r.dst, []string{RecoveryFlag, action, strconv.Itoa(os.Getpid()), r.state.InstalledSHA256, r.state.OldSHA256})
	if err != nil {
		return err
	}
	defer windows.CloseHandle(process)
	wait, err := windows.WaitForSingleObject(process, windows.INFINITE)
	if err != nil || wait != windows.WAIT_OBJECT_0 {
		return errUpdateHandoff
	}
	var status uint32
	if err := windows.GetExitCodeProcess(process, &status); err != nil || status != 0 {
		return errUpdateHandoff
	}
	return nil
}

func RunRecoveryHelper(args []string) error {
	if !windows.GetCurrentProcessToken().IsElevated() || len(args) != 4 || (args[0] != "restore" && args[0] != "clean") {
		return errUpdateHandoff
	}
	exe, err := SelfPath()
	if err != nil || !validUpdateTarget(exe) || !strings.EqualFold(filepath.Base(exe), windowsExecutableName) {
		return errUpdateHandoff
	}
	pid, err := strconv.ParseUint(args[1], 10, 32)
	if err != nil || pid == 0 || pid == uint64(os.Getpid()) {
		return errUpdateHandoff
	}
	caller, err := openUpdateCaller(uint32(pid))
	if err != nil {
		return errUpdateHandoff
	}
	defer windows.CloseHandle(caller)
	if !isUpdateCaller(caller, exe) {
		return errUpdateHandoff
	}
	state, err := readUpdateState(exe)
	if err != nil || state.InstalledSHA256 != args[2] || state.OldSHA256 != args[3] {
		return errUpdateHandoff
	}
	r := &Recovery{dst: exe, state: state}
	if !protectedRecovery(r) || (args[0] == "restore" && r.expired()) {
		return errUpdateHandoff
	}
	return finishFiles(r, args[0] == "restore", moveUpdateFile)
}

// Accept only an admin-controlled tree under Program Files. Unknown ACL
// forms, reparse points and writable backups all leave the installation alone.
func protectedRecovery(r *Recovery) bool {
	if !validUpdateTarget(r.dst) {
		return false
	}
	dir := filepath.Dir(r.dst)
	for _, id := range []*windows.KNOWNFOLDERID{windows.FOLDERID_ProgramFiles, windows.FOLDERID_ProgramFilesX86, windows.FOLDERID_ProgramFilesX64} {
		root, err := windows.KnownFolderPath(id, 0)
		if err != nil || !isWithin(dir, root) || strings.EqualFold(dir, root) {
			continue
		}
		if !adminControlled(root) {
			return false
		}
		for _, p := range []string{r.dst, r.dst + ".update-state.json", filepath.Join(dir, r.state.Old)} {
			if !validUpdateTarget(p) || !adminControlled(p) {
				return false
			}
		}
		for p := dir; !strings.EqualFold(p, root); p = filepath.Dir(p) {
			if !adminControlled(p) {
				return false
			}
		}
		return true
	}
	return false
}

func adminControlled(path string) bool {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return false
	}
	return adminControlledDescriptor(sd)
}

func adminControlledDescriptor(sd *windows.SECURITY_DESCRIPTOR) bool {
	trusted := func(sid *windows.SID) bool {
		// NT SERVICE\TrustedInstaller owns OS-managed files.
		return sid != nil && (sid.IsWellKnown(windows.WinBuiltinAdministratorsSid) || sid.IsWellKnown(windows.WinLocalSystemSid) || sid.String() == "S-1-5-80-956008885-3418522649-1831038044-1853292631-2271478464")
	}
	owner, _, err := sd.Owner()
	if err != nil || !trusted(owner) {
		return false
	}
	acl, _, err := sd.DACL()
	if err != nil || acl == nil {
		return false
	}
	for i := uint16(0); i < acl.AceCount; i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if windows.GetAce(acl, uint32(i), &ace) != nil {
			return false
		}
		if ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0 || ace.Header.AceType == windows.ACCESS_DENIED_ACE_TYPE {
			continue
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return false
		}
		const readOnly = windows.FILE_GENERIC_READ | windows.FILE_GENERIC_EXECUTE | windows.GENERIC_READ | windows.GENERIC_EXECUTE
		if ace.Mask&^readOnly != 0 && !trusted((*windows.SID)(unsafe.Pointer(&ace.SidStart))) {
			return false
		}
	}
	return true
}

func finishRecovery(r *Recovery, rollback bool) error {
	err := finishFiles(r, rollback, moveUpdateFile)
	if errors.Is(err, os.ErrPermission) {
		return elevatedRecovery(r, rollback)
	}
	return err
}
