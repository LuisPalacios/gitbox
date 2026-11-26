package remote

import (
	"context"
	"strings"
)

// Probe is what the host said about itself.
type Probe struct {
	// OS and Arch are Go's names for the host: "linux", "darwin", "arm64".
	OS, Arch string
	// RawOS and RawArch are what uname printed.
	RawOS, RawArch string
	// Shell and Home are the login shell and home folder of the remote
	// user.
	Shell, Home string
	// Libc is the first line of ldd --version, for diagnostics only: the
	// server is static, so musl hosts work too. It is empty on a host with
	// no ldd, such as macOS.
	Libc string
	// Banner is how many bytes the login shell printed before the answer.
	Banner int
}

// probeScript prints the facts after the marker, each one NUL-terminated.
const probeScript = `printf "\0GBX\0%s\0%s\0%s\0%s\0%s\0" "$(uname -s)" "$(uname -m)" "$SHELL" "$HOME" "$(command -v ldd >/dev/null 2>&1 && ldd --version 2>&1 | head -n 1)"`

// probe asks the host for its operating system, architecture, shell and
// home in one exec.
func (r *runner) probe(ctx context.Context) (Probe, error) {
	// The probe is the connection's first ssh, the one that logs in: run
	// reports a slow login and bounds it.
	res, err := r.run(ctx, "probe", probeScript, nil)
	if err != nil {
		// A Windows host whose shell is cmd or PowerShell cannot run sh.
		if res != nil && strings.Contains(strings.ToLower(string(res.stderr)), "is not recognized") {
			return Probe{}, newError(CodeUnsupportedOS, "%s runs Windows without a POSIX shell, supported from a later version", r.alias)
		}
		return Probe{}, err
	}
	if len(res.fields) < 4 {
		return Probe{}, newError(CodeSSHFailed, "the probe of %s gave an incomplete answer", r.alias)
	}
	p := Probe{RawOS: res.fields[0], RawArch: res.fields[1], Shell: res.fields[2], Home: res.fields[3], Banner: res.banner}
	if len(res.fields) > 4 {
		p.Libc = res.fields[4]
	}
	if p.Libc != "" {
		r.log.Printf("probe: %s %s, shell %s, libc %s", clean(p.RawOS), clean(p.RawArch), clean(p.Shell), clean(p.Libc))
	} else {
		r.log.Printf("probe: %s %s, shell %s", clean(p.RawOS), clean(p.RawArch), clean(p.Shell))
	}
	if err := p.resolve(r.alias); err != nil {
		return p, err
	}
	return p, nil
}

// resolve maps uname's answer to Go's names, or says why the host is not
// supported yet.
func (p *Probe) resolve(alias string) error {
	switch os := p.RawOS; {
	case os == "Linux":
		p.OS = "linux"
	case os == "Darwin":
		p.OS = "darwin"
	case strings.HasPrefix(os, "MINGW"), strings.HasPrefix(os, "MSYS"), strings.HasPrefix(os, "CYGWIN"):
		return newError(CodeUnsupportedOS, "%s runs Windows, supported from a later version", alias)
	default:
		return newError(CodeUnsupportedOS, "%s runs an operating system that remote mode does not support", alias)
	}
	switch p.RawArch {
	case "x86_64", "amd64":
		p.Arch = "amd64"
	case "aarch64", "arm64":
		p.Arch = "arm64"
	default:
		return newError(CodeUnsupportedArch, "%s has a processor architecture that remote mode does not support", alias)
	}
	return nil
}
