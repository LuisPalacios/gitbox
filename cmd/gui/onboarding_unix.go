//go:build !windows

package main

import "golang.org/x/sys/unix"

func onboardingFolderWritable(path string) bool {
	return unix.Access(path, unix.W_OK|unix.X_OK) == nil
}
