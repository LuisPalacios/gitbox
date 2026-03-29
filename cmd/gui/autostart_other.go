//go:build !windows && !darwin

package main

import "fmt"

func autostartEnabled() (bool, error) {
	return false, fmt.Errorf("autostart is not supported on this platform")
}

func autostartSet(_ bool) error {
	return fmt.Errorf("autostart is not supported on this platform")
}
