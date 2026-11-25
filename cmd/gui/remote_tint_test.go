//go:build !gitboxserver

package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

// The tint of remote windows is a preset saved in this computer's
// gitbox.json, and the default blue is saved as no key at all (#331).
func TestRemoteTintBindings(t *testing.T) {
	a, path := remoteHostsApp(t)
	if got := a.GetRemoteTint(); got != "#3b82f6" {
		t.Fatalf("GetRemoteTint without the key = %q, want the default blue", got)
	}
	if res := a.SetRemoteTint(" #8B5CF6 "); res.Error != "" {
		t.Fatalf("SetRemoteTint(violet) = %+v", res)
	}
	if got := loadConfig(t, path).Global.RemoteTint; got != "#8b5cf6" {
		t.Errorf("remote_tint on disk = %q", got)
	}
	if got := a.GetRemoteTint(); got != "#8b5cf6" {
		t.Errorf("GetRemoteTint after saving violet = %q", got)
	}

	if res := a.SetRemoteTint("#123456"); res.Hint != "error.remote.tint_unsupported" {
		t.Errorf("SetRemoteTint of a color that is not a preset = %+v", res)
	}
	if got := loadConfig(t, path).Global.RemoteTint; got != "#8b5cf6" {
		t.Errorf("a refused color changed the file: %q", got)
	}

	if res := a.SetRemoteTint("#3b82f6"); res.Error != "" {
		t.Fatalf("SetRemoteTint(blue) = %+v", res)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "remote_tint") {
		t.Error("the default blue was written as a key")
	}
}

// A remote window keeps the tint in this computer's file and learns about a
// change any process saves there, without reconnecting (#331).
func TestRemoteWindowWatchesTint(t *testing.T) {
	prev := remoteTintPoll
	remoteTintPoll = 20 * time.Millisecond
	t.Cleanup(func() { remoteTintPoll = prev })

	a, sink := remoteApp(t, nil)
	prefsFile := a.prefs.(*filePrefs).path
	a.cfgPath = prefsFile
	a.events = sink
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { a.watchRemoteTint(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })

	tints := func() []string {
		var out []string
		for _, e := range sink.named("remote:tint") {
			out = append(out, e.args[0].(string))
		}
		return out
	}
	waitTints := func(want int) []string {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for len(tints()) < want && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		return tints()
	}

	time.Sleep(100 * time.Millisecond)
	if got := tints(); len(got) != 0 {
		t.Fatalf("tint events before any change: %q", got)
	}

	// The Remote hosts window, another process, saves a new color.
	if _, err := config.UpdateFile(prefsFile, func(c *config.Config) error {
		c.Global.RemoteTint = "#22c55e"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got := waitTints(1); len(got) != 1 || got[0] != "#22c55e" {
		t.Fatalf("after another process saved green: %q", got)
	}

	// A change of another preference sends no tint.
	if _, err := config.UpdateFile(prefsFile, func(c *config.Config) error {
		c.Global.Theme = "light"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	if got := tints(); len(got) != 1 {
		t.Errorf("a theme change sent a tint: %q", got)
	}

	// Saving it from the remote window itself writes this computer's file,
	// not the host's: its backend is not even connected.
	if res := a.SetRemoteTint("#f59e0b"); res.Error != "" {
		t.Fatalf("SetRemoteTint in a remote window = %+v", res)
	}
	if got := loadConfig(t, prefsFile).Global.RemoteTint; got != "#f59e0b" {
		t.Errorf("remote_tint in the client file = %q", got)
	}
	if got := waitTints(2); len(got) != 2 || got[1] != "#f59e0b" {
		t.Errorf("after saving amber in the window: %q", got)
	}
}
