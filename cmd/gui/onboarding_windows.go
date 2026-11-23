package main

import "golang.org/x/sys/windows"

// Request directory creation rights on the existing directory without
// creating a probe file. On a directory, FILE_APPEND_DATA means add a
// subdirectory and FILE_WRITE_DATA means add a file.
func onboardingFolderWritable(path string) bool {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	h, err := windows.CreateFile(name, windows.FILE_APPEND_DATA|windows.FILE_WRITE_DATA, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return false
	}
	return windows.CloseHandle(h) == nil
}
