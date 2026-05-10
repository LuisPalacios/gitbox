//go:build windows

package main

import (
	"os"
	"os/exec"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Manager-subprocess lifetime binding (issue #69 — orphan Manager survives
// parent close on Windows). Parent's OnShutdown Kill is unreliable on
// Win/Linux: Wails sometimes exits the parent before TerminateProcess fully
// propagates, and Shutdown does not always fire on a hard close. The
// canonical Windows answer is a Job Object with KILL_ON_JOB_CLOSE — the OS
// itself kills every assigned process when the job handle goes away (which
// happens automatically when the parent exits, no matter how).

var (
	jobOnce   sync.Once
	jobHandle windows.Handle
)

func initJobObject() {
	jobOnce.Do(func() {
		h, err := windows.CreateJobObject(nil, nil)
		if err != nil {
			return
		}
		info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
		info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
		if _, err := windows.SetInformationJobObject(
			h,
			windows.JobObjectExtendedLimitInformation,
			uintptr(unsafe.Pointer(&info)),
			uint32(unsafe.Sizeof(info)),
		); err != nil {
			windows.CloseHandle(h)
			return
		}
		jobHandle = h
	})
}

// linkChildLifetimeToParent assigns p to the parent's KILL_ON_JOB_CLOSE
// Job Object. When the parent process exits — graceful, crash, or
// taskkill — Windows closes the job and every member dies. Best-effort
// (silent on failure); the parent-side Shutdown Kill in app.go remains
// as the in-band fallback for the well-behaved-exit case.
func linkChildLifetimeToParent(p *os.Process) {
	initJobObject()
	if jobHandle == 0 || p == nil {
		return
	}
	h, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
		false, uint32(p.Pid),
	)
	if err != nil {
		return
	}
	defer windows.CloseHandle(h)
	_ = windows.AssignProcessToJobObject(jobHandle, h)
}

// configureChildLifetime is a Linux-only Pdeathsig hook. No-op on Windows;
// the Job Object handles everything via linkChildLifetimeToParent.
func configureChildLifetime(_ *exec.Cmd) {}
