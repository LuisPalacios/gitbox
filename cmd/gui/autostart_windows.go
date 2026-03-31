package main

import (
	"os"

	"golang.org/x/sys/windows/registry"
)

const autostartRegistryKey = `Software\Microsoft\Windows\CurrentVersion\Run`
const autostartValueName = "GitboxApp"

func autostartEnabled() (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, autostartRegistryKey, registry.QUERY_VALUE)
	if err != nil {
		return false, nil // key doesn't exist → not enabled
	}
	defer k.Close()
	_, _, err = k.GetStringValue(autostartValueName)
	if err != nil {
		return false, nil
	}
	return true, nil
}

func autostartSet(enable bool) error {
	if enable {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		k, _, err := registry.CreateKey(registry.CURRENT_USER, autostartRegistryKey, registry.SET_VALUE)
		if err != nil {
			return err
		}
		defer k.Close()
		return k.SetStringValue(autostartValueName, `"`+exe+`"`)
	}
	// Disable: delete the value.
	k, err := registry.OpenKey(registry.CURRENT_USER, autostartRegistryKey, registry.SET_VALUE)
	if err != nil {
		return nil // nothing to delete
	}
	defer k.Close()
	err = k.DeleteValue(autostartValueName)
	if err == registry.ErrNotExist {
		return nil
	}
	return err
}
