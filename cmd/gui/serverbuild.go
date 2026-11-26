package main

import (
	"fmt"
	"io"

	"github.com/LuisPalacios/gitbox/cmd/gui/serverbin"
)

// remoteCode says why this GitboxApp refuses remote mode before it connects
// to anything. Each code has an error.remote.<code> entry in both catalogs
// of i18n.ts.
type remoteCode string

const (
	// remoteUnstampedBuild: the app was built without a version stamp, so
	// it cannot tell which gitbox-server is its own.
	remoteUnstampedBuild remoteCode = "unstamped_build"
	// remoteServerTargetMissing: the app carries no gitbox-server binary.
	remoteServerTargetMissing remoteCode = "server_target_missing"
	// remoteServerBuildMismatch: the binaries the app carries come from
	// another build.
	remoteServerBuildMismatch remoteCode = "server_build_mismatch"
	// remoteNotConnected: a remote window called the backend before the
	// connection was up, or after it ended.
	remoteNotConnected remoteCode = "not_connected"
	// remoteNotImplemented: the method has no remote side yet (#233).
	remoteNotImplemented remoteCode = "not_implemented_remote"
	// remoteReadOnly: a read-only server refused to save the host's
	// config (#240).
	remoteReadOnly remoteCode = "read_only"
	// remoteServerCrashed: the server ended while connected.
	remoteServerCrashed remoteCode = "server_crashed"
	// remoteUnknownAlias: no remote with that alias is configured.
	remoteUnknownAlias remoteCode = "unknown_alias"
	// remoteWindowClosed: the action needs the remote's open window (#330).
	remoteWindowClosed remoteCode = "window_closed"
	// remoteTintUnsupported: the color asked for the remote windows' tint
	// is not one of config.RemoteTintPresets (#331).
	remoteTintUnsupported remoteCode = "tint_unsupported"
)

// remoteError is a refusal of remote mode. Its text names builds and
// commands, never anything from a host.
type remoteError struct {
	Code   remoteCode
	Detail string
}

func (e *remoteError) Error() string { return e.Detail }

// rebuildCommand builds a GitboxApp that carries its own gitbox-server.
const rebuildCommand = `eval "$(scripts/stamp.sh)" && scripts/build-server.sh && (cd cmd/gui && wails build -tags serverbin -ldflags "$LDFLAGS")`

// serverBuild is what a GitboxApp knows about its own build and about the
// gitbox-server binaries it carries.
type serverBuild struct {
	Version, Commit                 string // this GitboxApp
	EmbeddedVersion, EmbeddedCommit string // the build of the embedded server
	Targets                         []serverbin.Target
}

// thisServerBuild describes the running binary.
func thisServerBuild() serverBuild {
	return serverBuild{
		Version: version, Commit: commit,
		EmbeddedVersion: serverbin.Version, EmbeddedCommit: serverbin.Commit,
		Targets: serverbin.Targets(),
	}
}

// check reports why this build cannot run remote mode, or nil. The server it
// installs must be the build it was compiled with: the handshake refuses any
// other, so a mismatch here would only fail later, on the other host.
func (b serverBuild) check() error {
	switch {
	case b.Commit == "" || b.Commit == "none":
		return &remoteError{remoteUnstampedBuild,
			"this GitboxApp has no build stamp, which remote mode needs; rebuild it with: " + rebuildCommand}
	case len(b.Targets) == 0:
		return &remoteError{remoteServerTargetMissing,
			"this GitboxApp carries no gitbox-server; rebuild it with: " + rebuildCommand}
	case b.EmbeddedVersion != b.Version || b.EmbeddedCommit != b.Commit:
		return &remoteError{remoteServerBuildMismatch, fmt.Sprintf(
			"this GitboxApp is build %s (%s) and the gitbox-server it carries is build %s (%s); rebuild both from one commit",
			b.Version, shortCommit(b.Commit), b.EmbeddedVersion, shortCommit(b.EmbeddedCommit))}
	}
	return nil
}

func shortCommit(c string) string { return c[:minInt(7, len(c))] }

// verify reads every embedded binary through read, which returns one only
// when it matches its pin. It is how a build script proves that the artifact
// it just made really carries what its table promises.
func (b serverBuild) verify(read func(serverbin.Target) ([]byte, error)) error {
	for _, t := range b.Targets {
		binary, err := read(t)
		if err != nil {
			return err
		}
		if int64(len(binary)) != t.Size {
			return fmt.Errorf("%w: %s", serverbin.ErrCorrupt, t.Name())
		}
	}
	return nil
}

// list writes what --list-server-targets prints: the build of the embedded
// server, then one line per target with its size and SHA-256 as installed.
// Build scripts compare it with the stamp they built from.
func (b serverBuild) list(w io.Writer) {
	orNone := func(s string) string {
		if s == "" {
			return "none"
		}
		return s
	}
	fmt.Fprintf(w, "commit %s version %s\n", orNone(b.EmbeddedCommit), orNone(b.EmbeddedVersion))
	for _, t := range b.Targets {
		fmt.Fprintf(w, "%s\t%d\t%s\n", t.Name(), t.Size, t.SHA256)
	}
}
