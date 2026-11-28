package main

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/LuisPalacios/gitbox/cmd/gui/serverbin"
)

const (
	buildVersion = "v9.9.9-3-gabcdef0-dev"
	buildCommit  = "0123456789abcdef0123456789abcdef01234567"
)

func stampedBuild() serverBuild {
	return serverBuild{
		Version: buildVersion, Commit: buildCommit,
		EmbeddedVersion: buildVersion, EmbeddedCommit: buildCommit,
		Targets: []serverbin.Target{
			{OS: "linux", Arch: "amd64", Size: 9732258, SHA256: strings.Repeat("ab", 32)},
			{OS: "darwin", Arch: "arm64", Size: 9218048, SHA256: strings.Repeat("cd", 32)},
		},
	}
}

// A GitboxApp only runs remote mode with the gitbox-server it was built
// with: stamped, carried inside, and of the very same build.
func TestServerBuildMismatch(t *testing.T) {
	if err := stampedBuild().check(); err != nil {
		t.Fatalf("a matching build is refused: %v", err)
	}

	for name, tt := range map[string]struct {
		edit func(*serverBuild)
		want remoteCode
		says string
	}{
		"not stamped":                        {func(b *serverBuild) { b.Commit = "none" }, remoteUnstampedBuild, "scripts/stamp.sh"},
		"no commit at all":                   {func(b *serverBuild) { b.Commit = "" }, remoteUnstampedBuild, "scripts/stamp.sh"},
		"not stamped and no server":          {func(b *serverBuild) { b.Commit, b.Targets = "none", nil }, remoteUnstampedBuild, "scripts/stamp.sh"},
		"no server inside":                   {func(b *serverBuild) { b.Targets, b.EmbeddedVersion, b.EmbeddedCommit = nil, "", "" }, remoteServerTargetMissing, "-tags serverbin"},
		"server of another commit":           {func(b *serverBuild) { b.EmbeddedCommit = strings.Repeat("f", 40) }, remoteServerBuildMismatch, "fffffff"},
		"server of another version":          {func(b *serverBuild) { b.EmbeddedVersion = "v9.9.8" }, remoteServerBuildMismatch, "v9.9.8"},
		"app rebuilt, server was not":        {func(b *serverBuild) { b.Commit = strings.Repeat("e", 40) }, remoteServerBuildMismatch, "eeeeeee"},
		"a commit that only starts the same": {func(b *serverBuild) { b.EmbeddedCommit = buildCommit[:7] }, remoteServerBuildMismatch, "0123456"},
	} {
		build := stampedBuild()
		tt.edit(&build)
		err := build.check()
		refusal, ok := errors.AsType[*remoteError](err)
		if !ok || refusal.Code != tt.want {
			t.Errorf("%s: check = %v, want %s", name, err, tt.want)
			continue
		}
		// What the window gets: the code, a catalog key and a text that
		// survives the boundary whole and says what to do.
		ue := toUserError(err)
		if ue.Code != "remote."+string(tt.want) || ue.Hint != "error.remote."+string(tt.want) {
			t.Errorf("%s: crossed the boundary as %+v", name, ue)
		}
		if ue.Detail != refusal.Detail || !strings.Contains(ue.Detail, tt.says) {
			t.Errorf("%s: the text the window gets is %q; want the refusal whole, naming %q", name, ue.Detail, tt.says)
		}
	}
}

// --list-server-targets prints the embedded build and one line per target,
// in the shape the build scripts compare.
func TestListServerTargets(t *testing.T) {
	var out bytes.Buffer
	stampedBuild().list(&out)
	want := "commit " + buildCommit + " version " + buildVersion + "\n" +
		"linux/amd64\t9732258\t" + strings.Repeat("ab", 32) + "\n" +
		"darwin/arm64\t9218048\t" + strings.Repeat("cd", 32) + "\n"
	if out.String() != want {
		t.Errorf("list printed\n%q\nwant\n%q", out.String(), want)
	}

	out.Reset()
	serverBuild{Version: buildVersion, Commit: buildCommit}.list(&out)
	if out.String() != "commit none version none\n" {
		t.Errorf("a build with no server printed %q", out.String())
	}
}

// verify proves the artifact carries every binary of its table.
func TestServerBuildVerify(t *testing.T) {
	build := stampedBuild()
	var asked []string
	whole := func(target serverbin.Target) ([]byte, error) {
		asked = append(asked, target.Name())
		return make([]byte, target.Size), nil
	}
	if err := build.verify(whole); err != nil {
		t.Errorf("verify = %v", err)
	}
	if strings.Join(asked, " ") != "linux/amd64 darwin/arm64" {
		t.Errorf("verify read %v, want every target", asked)
	}

	corrupt := func(target serverbin.Target) ([]byte, error) {
		if target.OS == "darwin" {
			return nil, fmt.Errorf("%w: %s", serverbin.ErrCorrupt, target.Name())
		}
		return make([]byte, target.Size), nil
	}
	if err := build.verify(corrupt); !errors.Is(err, serverbin.ErrCorrupt) {
		t.Errorf("verify with a corrupt target = %v, want ErrCorrupt", err)
	}
	short := func(target serverbin.Target) ([]byte, error) { return make([]byte, target.Size-1), nil }
	if err := build.verify(short); !errors.Is(err, serverbin.ErrCorrupt) {
		t.Errorf("verify with short binaries = %v, want ErrCorrupt", err)
	}
}

// The running test binary is an ordinary build: not stamped, no server. It
// must say so instead of pretending.
func TestThisServerBuild(t *testing.T) {
	build := thisServerBuild()
	if build.Version != version || build.Commit != commit {
		t.Errorf("thisServerBuild = %+v, want this binary's %s %s", build, version, commit)
	}
	if build.EmbeddedCommit != serverbin.Commit || len(build.Targets) != len(serverbin.Targets()) {
		t.Errorf("thisServerBuild does not report the embedded server: %+v", build)
	}
	if commit == "none" {
		if refusal, ok := errors.AsType[*remoteError](build.check()); !ok || refusal.Code != remoteUnstampedBuild {
			t.Errorf("an unstamped build check = %v, want %s", build.check(), remoteUnstampedBuild)
		}
	}
}
