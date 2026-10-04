package main

import (
	"strings"
	"testing"
)

// The AppImage build is notify-only: ApplyUpdate must refuse before any
// network call or download instead of failing inside the zip extractor.
func TestApplyUpdate_AppImageRefuses(t *testing.T) {
	t.Setenv("APPIMAGE", "/home/me/Applications/gitbox-x86_64.AppImage")
	t.Setenv("APPDIR", "")

	a := &App{}
	err := a.ApplyUpdate()
	if err == nil {
		t.Fatal("ApplyUpdate() inside an AppImage succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "release page") {
		t.Errorf("ApplyUpdate() error = %q, want it to point at the release page", err)
	}
}
