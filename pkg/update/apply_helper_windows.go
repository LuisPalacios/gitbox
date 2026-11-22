//go:build windows

package update

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const elevatedWait = 60 * time.Second

var errUpdateHandoff = errors.New("could not hand off the verified update to the administrator process")

// ApplyElevated starts a copy of the installed executable with UAC. The
// unelevated caller retains ownership of its downloads and waits for the
// helper to verify an independent snapshot before it quits.
func ApplyElevated(archive, tag string, relaunch bool) error {
	exe, err := SelfPath()
	if err != nil || windows.GetCurrentProcessToken().IsElevated() {
		return errUpdateHandoff
	}

	dir := filepath.Dir(archive)
	paths := []string{filepath.Join(dir, checksumsAsset), filepath.Join(dir, signatureAsset), archive}
	args := []string{ApplyUpdateFlag, tag, strconv.Itoa(os.Getpid())}
	for _, path := range paths {
		file, err := os.Open(path)
		if err != nil {
			return errUpdateHandoff
		}
		defer file.Close()
		args = append(args, formatUpdateHandle(windows.Handle(file.Fd())))
	}

	ready, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return errUpdateHandoff
	}
	defer windows.CloseHandle(ready)
	apply, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return errUpdateHandoff
	}
	defer windows.CloseHandle(apply)
	args = append(args, formatUpdateHandle(ready), formatUpdateHandle(apply))

	process, err := shellExecuteUpdate(exe, args)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(process)
	event, err := windows.WaitForMultipleObjects([]windows.Handle{process, ready}, false, uint32(elevatedWait.Milliseconds()))
	if err != nil || event != windows.WAIT_OBJECT_0+1 {
		return errUpdateHandoff
	}
	if relaunch {
		if err := startUpdateRestart(exe, process, os.Args[1:]); err != nil {
			return errors.New("could not prepare an unelevated restart of GitboxApp")
		}
	}
	if err := windows.SetEvent(apply); err != nil {
		return errUpdateHandoff
	}
	return nil
}

type shellExecuteInfo struct {
	size, mask                        uint32
	window                            windows.Handle
	verb, file, parameters, directory *uint16
	show                              int32
	instance                          windows.Handle
	idList                            uintptr
	class                             *uint16
	classKey                          windows.Handle
	hotKey                            uint32
	icon, process                     windows.Handle
}

func shellExecuteUpdate(exe string, args []string) (windows.Handle, error) {
	file, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return 0, errUpdateHandoff
	}
	params, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(args))
	if err != nil {
		return 0, errUpdateHandoff
	}
	dir, err := windows.GetSystemDirectory()
	if err != nil {
		return 0, errUpdateHandoff
	}
	verb, _ := windows.UTF16PtrFromString("runas")
	directory, _ := windows.UTF16PtrFromString(dir)
	info := shellExecuteInfo{
		size: uint32(unsafe.Sizeof(shellExecuteInfo{})),
		// SEE_MASK_NOCLOSEPROCESS | SEE_MASK_NOASYNC | SEE_MASK_FLAG_NO_UI
		mask: 0x00000040 | 0x00000100 | 0x00000400,
		verb: verb, file: file, parameters: params,
		directory: directory, show: windows.SW_SHOWNORMAL,
	}
	proc := windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteExW")
	ok, _, callErr := proc.Call(uintptr(unsafe.Pointer(&info)))
	if ok == 0 || info.process == 0 {
		if errors.Is(callErr, windows.ERROR_CANCELLED) {
			return 0, errors.New("administrator approval was canceled")
		}
		return 0, errUpdateHandoff
	}
	return info.process, nil
}

// RunElevatedUpdate accepts only handles from the running copy of this
// executable. It never opens or removes caller-supplied filesystem paths.
func RunElevatedUpdate(current string, args []string) error {
	if !windows.GetCurrentProcessToken().IsElevated() || len(args) != 7 {
		return errUpdateHandoff
	}
	pid, err := strconv.ParseUint(args[1], 10, 32)
	if err != nil || pid == 0 || pid == uint64(os.Getpid()) {
		return errUpdateHandoff
	}
	exe, err := SelfPath()
	if err != nil || !strings.EqualFold(filepath.Base(exe), windowsExecutableName) {
		return errUpdateHandoff
	}
	process, err := openUpdateCaller(uint32(pid))
	if err != nil {
		return errUpdateHandoff
	}
	defer windows.CloseHandle(process)
	if !isUpdateCaller(process, exe) || !validUpdateTarget(exe) {
		return errUpdateHandoff
	}
	oldHash, err := regularFileHash(exe)
	if err != nil {
		return errUpdateHandoff
	}

	checksums, err := readUpdateHandle(process, args[2], updateMetadataLimit)
	if err != nil {
		return errUpdateHandoff
	}
	signature, err := readUpdateHandle(process, args[3], updateMetadataLimit)
	if err != nil {
		return errUpdateHandoff
	}
	archive, err := readUpdateHandle(process, args[4], updateArchiveLimit)
	if err != nil {
		return errUpdateHandoff
	}
	payload, err := verifiedWindowsExecutable(current, args[0], ArtifactName(), checksums, signature, archive)
	if err != nil {
		return err
	}

	ready, err := duplicateUpdateHandle(process, args[5], windows.EVENT_MODIFY_STATE)
	if err != nil {
		return errUpdateHandoff
	}
	defer windows.CloseHandle(ready)
	apply, err := duplicateUpdateHandle(process, args[6], windows.SYNCHRONIZE)
	if err != nil {
		return errUpdateHandoff
	}
	defer windows.CloseHandle(apply)
	if err := windows.SetEvent(ready); err != nil {
		return errUpdateHandoff
	}
	event, err := windows.WaitForMultipleObjects([]windows.Handle{apply, process}, false, uint32(elevatedWait.Milliseconds()))
	if err != nil || event != windows.WAIT_OBJECT_0 {
		return errUpdateHandoff
	}
	event, err = windows.WaitForSingleObject(process, uint32(elevatedWait.Milliseconds()))
	if err != nil || event != windows.WAIT_OBJECT_0 {
		return reportElevatedFailure("GitboxApp did not close in time. The update was not installed.")
	}

	if err := installElevatedPayload(exe, oldHash, payload); err != nil {
		if errors.Is(err, errInstalledUpdateChanged) {
			return reportElevatedFailure("Another update changed GitboxApp. Open GitboxApp to continue.")
		}
		if errors.Is(err, errUpdateRollback) {
			return reportElevatedFailure("GitboxApp could not finish the update or restore the previous executable. The backup remains in the install directory.")
		}
		return reportElevatedFailure("GitboxApp could not install the update. Open GitboxApp again to retry.")
	}
	return nil
}

func formatUpdateHandle(handle windows.Handle) string {
	return strconv.FormatUint(uint64(handle), 10)
}

func parseUpdateHandle(value string) (windows.Handle, error) {
	raw, err := strconv.ParseUint(value, 10, strconv.IntSize)
	if err != nil || raw == 0 || raw > uint64(^uintptr(0)>>1) {
		return 0, errUpdateHandoff
	}
	return windows.Handle(raw), nil
}

func duplicateUpdateHandle(process windows.Handle, raw string, access uint32) (windows.Handle, error) {
	source, err := parseUpdateHandle(raw)
	if err != nil {
		return 0, err
	}
	var handle windows.Handle
	err = windows.DuplicateHandle(process, source, windows.CurrentProcess(), &handle, access, false, 0)
	return handle, err
}

func readUpdateHandle(process windows.Handle, raw string, limit int64) ([]byte, error) {
	handle, err := duplicateUpdateHandle(process, raw, windows.FILE_GENERIC_READ)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(handle), "update input")
	defer file.Close()

	kind, err := windows.GetFileType(handle)
	if err != nil || kind != windows.FILE_TYPE_DISK {
		return nil, errUpdateHandoff
	}
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil ||
		info.FileAttributes&(windows.FILE_ATTRIBUTE_DIRECTORY|windows.FILE_ATTRIBUTE_REPARSE_POINT) != 0 {
		return nil, errUpdateHandoff
	}
	size := uint64(info.FileSizeHigh)<<32 | uint64(info.FileSizeLow)
	if size == 0 || size > uint64(limit) {
		return nil, errUpdateHandoff
	}
	return readUpdateBytes(file, limit)
}

func openUpdateCaller(pid uint32) (windows.Handle, error) {
	const access = windows.SYNCHRONIZE | windows.PROCESS_QUERY_LIMITED_INFORMATION | windows.PROCESS_DUP_HANDLE
	process, err := windows.OpenProcess(access, false, pid)
	if !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		return process, err
	}

	// Over-the-shoulder UAC can launch under a different administrator.
	// Narrow the debug privilege to this one OpenProcess call.
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_ADJUST_PRIVILEGES|windows.TOKEN_QUERY, &token); err != nil {
		return 0, err
	}
	defer token.Close()
	var luid windows.LUID
	privilege, _ := windows.UTF16PtrFromString("SeDebugPrivilege")
	if err := windows.LookupPrivilegeValue(nil, privilege, &luid); err != nil {
		return 0, err
	}
	enable := windows.Tokenprivileges{
		PrivilegeCount: 1,
		Privileges:     [1]windows.LUIDAndAttributes{{Luid: luid, Attributes: windows.SE_PRIVILEGE_ENABLED}},
	}
	var previous windows.Tokenprivileges
	var used uint32
	if err := windows.AdjustTokenPrivileges(token, false, &enable, uint32(unsafe.Sizeof(previous)), &previous, &used); err != nil {
		return 0, err
	}
	process, err = windows.OpenProcess(access, false, pid)
	if restoreErr := windows.AdjustTokenPrivileges(token, false, &previous, 0, nil, nil); restoreErr != nil {
		if process != 0 {
			windows.CloseHandle(process)
		}
		return 0, restoreErr
	}
	return process, err
}

func isUpdateCaller(process windows.Handle, exe string) bool {
	name := make([]uint16, 32768)
	n := uint32(len(name))
	if err := windows.QueryFullProcessImageName(process, 0, &name[0], &n); err != nil {
		return false
	}
	return strings.EqualFold(exe, filepath.Clean(windows.UTF16ToString(name[:n])))
}

func validUpdateTarget(exe string) bool {
	volume := filepath.VolumeName(exe)
	if len(volume) != 2 || volume[1] != ':' || !filepath.IsAbs(exe) {
		return false
	}
	root, err := windows.UTF16PtrFromString(volume + string(os.PathSeparator))
	if err != nil || windows.GetDriveType(root) != windows.DRIVE_FIXED {
		return false
	}
	for path := exe; ; path = filepath.Dir(path) {
		name, err := windows.UTF16PtrFromString(path)
		if err != nil {
			return false
		}
		attrs, err := windows.GetFileAttributes(name)
		if err != nil || attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return false
		}
		if path == filepath.Dir(path) {
			break
		}
	}
	return true
}

func installElevatedPayload(exe, oldHash string, payload []byte) error {
	if !validUpdateTarget(exe) {
		return errUpdateHandoff
	}

	ops := realReplaceOps()
	ops.moveFile = moveUpdateFile
	hash := fmt.Sprintf("%x", sha256.Sum256(payload))
	return replaceWindowsExecutableWith(exe, oldHash, ops, func(staged string) (string, error) {
		return stageExecutable(bytes.NewReader(payload), staged, int64(len(payload)), 0o755, hash, ops.copyFile)
	})
}

func reportElevatedFailure(message string) error {
	proc := windows.NewLazySystemDLL("user32.dll").NewProc("MessageBoxW")
	text, _ := windows.UTF16PtrFromString(message)
	title, _ := windows.UTF16PtrFromString("GitboxApp update")
	proc.Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), 0x00000010) // MB_ICONERROR
	return errors.New(message)
}

// startUpdateRestart starts a waiter using this unelevated process's token.
// Only a wait and exit-status handle is inherited, and only by this child.
func startUpdateRestart(exe string, process windows.Handle, args []string) error {
	var status uint32
	if err := windows.GetExitCodeProcess(process, &status); err != nil {
		return err
	}
	var local windows.Handle
	err := windows.DuplicateHandle(windows.CurrentProcess(), process, windows.CurrentProcess(), &local,
		windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, true, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(local)
	argv := append([]string{RestartAfterUpdateFlag, formatUpdateHandle(local)}, args...)
	return startDetached(exe, argv, func(cmd *exec.Cmd) {
		cmd.SysProcAttr.AdditionalInheritedHandles = []syscall.Handle{syscall.Handle(local)}
	})
}

// RestartAfterUpdate runs with the original GUI user's credentials. After
// a successful helper exit, the new executable waits for this old image
// to close through the ordinary relaunch path.
func RestartAfterUpdate(args []string) error {
	if windows.GetCurrentProcessToken().IsElevated() || len(args) == 0 {
		return errUpdateHandoff
	}
	exe, err := SelfPath()
	if err != nil {
		return errUpdateHandoff
	}
	process, err := parseUpdateHandle(args[0])
	if err != nil {
		return err
	}
	defer windows.CloseHandle(process)
	wait, err := windows.WaitForSingleObject(process, uint32((3 * elevatedWait).Milliseconds()))
	if err != nil || wait != windows.WAIT_OBJECT_0 {
		return errUpdateHandoff
	}
	var status uint32
	if err := windows.GetExitCodeProcess(process, &status); err != nil || status != 0 {
		return errUpdateHandoff
	}
	if err := Relaunch(exe, os.Getpid(), args[1:]); err != nil {
		return reportElevatedFailure("The update was installed. Open GitboxApp to continue.")
	}
	return nil
}
