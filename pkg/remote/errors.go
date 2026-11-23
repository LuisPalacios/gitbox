// Package remote starts a gitbox-server on another host over the system ssh
// (#233): it probes the host, installs the server that matches this build
// when it is missing, verifies it before every start and hands back the
// server's stdin and stdout as one connection. It also uninstalls the server
// and collects old versions on the host.
//
// Everything runs through the user's own ssh: their ssh_config, known_hosts,
// ProxyJump and agent apply unchanged, and nothing here weakens host-key
// checking. The package knows nothing about the app: the server binary and
// its pin are parameters.
package remote

import "fmt"

// Code is a stable class of failure. Each one has an error.remote.<code>
// entry in both catalogs of the frontend.
type Code string

const (
	// SSH and the connection.
	CodeSSHMissing     Code = "ssh_missing"
	CodeSSHAuth        Code = "ssh_auth"
	CodeSSHAgent       Code = "ssh_agent"
	CodeSSHHostKey     Code = "ssh_hostkey"
	CodeSSHTimeout     Code = "ssh_timeout"
	CodeSSHConfig      Code = "ssh_config"
	CodeSSHFailed      Code = "ssh_failed"
	CodeBannerOverflow Code = "banner_overflow"

	// Host support.
	CodeUnsupportedOS   Code = "unsupported_os"
	CodeUnsupportedArch Code = "unsupported_arch"

	// The server binary and its build.
	CodeServerTargetMissing Code = "server_target_missing"
	CodeUnstampedBuild      Code = "unstamped_build"
	CodeHashMismatch        Code = "hash_mismatch"

	// The remote host.
	CodeDiskFull        Code = "disk_full"
	CodeNoHashTool      Code = "no_hash_tool"
	CodeNoexec          Code = "noexec"
	CodeServerDirUnsafe Code = "server_dir_unsafe"

	// Runtime.
	CodeServerCrashed Code = "server_crashed"
)

// Error is a failure of a remote step. Its Detail is built from this
// package's own words, numbers and the paths it chose: never from what the
// host printed, which goes to the connection log only.
type Error struct {
	Code   Code
	Detail string
}

func (e *Error) Error() string { return e.Detail }

func newError(code Code, format string, args ...any) *Error {
	return &Error{Code: code, Detail: fmt.Sprintf(format, args...)}
}
