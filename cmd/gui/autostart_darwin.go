package main

import (
	"os"
	"path/filepath"
	"text/template"
)

const launchAgentLabel = "com.luispalacios.gitbox"

func launchAgentPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", launchAgentLabel+".plist")
}

var plistTemplate = template.Must(template.New("plist").Parse(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>{{.Label}}</string>
	<key>ProgramArguments</key>
	<array>
		<string>{{.Exe}}</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
</dict>
</plist>
`))

func autostartEnabled() (bool, error) {
	_, err := os.Stat(launchAgentPath())
	if err != nil {
		return false, nil
	}
	return true, nil
}

func autostartSet(enable bool) error {
	path := launchAgentPath()
	if enable {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		f, err := os.Create(path)
		if err != nil {
			return err
		}
		defer f.Close()
		return plistTemplate.Execute(f, struct{ Label, Exe string }{launchAgentLabel, exe})
	}
	// Disable: remove the plist.
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
