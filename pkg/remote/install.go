package remote

import (
	"bytes"
	"context"
	"io"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// Target is one server binary: the bytes, loaded only for an install, and
// the SHA-256 they must have.
type Target struct {
	// SHA256 is the pin, in lowercase hex.
	SHA256 string
	// Size is the size of the binary as installed.
	Size int64
	// Load returns the binary. The installer checks it against SHA256.
	Load func() ([]byte, error)
}

// BinaryName is the file name of the server inside its build folder.
const BinaryName = "gitbox-server"

var pinRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

// BuildNameRE matches the name of a build folder under the server root:
// <version>-<commit>-<pin8>. Garbage collection and uninstall touch only
// folders with such a name.
var BuildNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*-[0-9a-f]{40}-[0-9a-f]{8}$`)

// BuildName is the folder of one server build. The pin is part of it: two
// builds of the same commit can carry different bytes, and then they
// install side by side.
func BuildName(version, commit, pin string) string {
	return version + "-" + commit + "-" + pin[:min(8, len(pin))]
}

// layout says where the server lives on the host, as the scripts take it:
// a mode, "h" for a path under the home folder or "a" for an absolute one,
// and the path.
type layout struct {
	mode, path string
}

func newLayout(serverDir string) (layout, error) {
	if serverDir == "" {
		serverDir = DefaultServerDir
	}
	var l layout
	switch {
	case strings.HasPrefix(serverDir, "~/"):
		l = layout{"h", strings.TrimPrefix(serverDir, "~/")}
		if c := path.Clean("/" + l.path); c == "/" {
			return l, newError(CodeServerDirUnsafe, "server_dir %s is the home folder itself; choose a folder of its own", serverDir)
		}
	case strings.HasPrefix(serverDir, "/"):
		l = layout{"a", serverDir}
		if path.Clean(serverDir) == "/" {
			return l, newError(CodeServerDirUnsafe, "server_dir %s is the root folder; choose a folder of its own", serverDir)
		}
	default:
		return l, newError(CodeServerDirUnsafe, "server_dir %s must be absolute or start with ~/", serverDir)
	}
	return l, nil
}

// ValidateServerDir reports a server_dir that remote mode refuses: one
// that is not absolute or under the home, or that is the home or the root
// folder itself.
func ValidateServerDir(dir string) error {
	_, err := newLayout(dir)
	return err
}

func (l layout) args(more ...string) []string { return append([]string{l.mode, l.path}, more...) }

// rootPrelude sets r to the server root from the first two arguments.
const rootPrelude = `
if [ "$1" = h ]; then r="$HOME/$2"; else r="$2"; fi;
`

// refuseRoot stops a script whose root is the filesystem root or the home
// folder itself: everything under it would count as the server's. It needs
// r to exist.
const refuseRoot = `
real=$(cd "$r" && pwd -P) && h=$(cd "$HOME" && pwd -P) || exit 1;
if [ "$real" = / ] || [ "$real" = "$h" ]; then printf "\0GBX\0refused\0"; exit 0; fi;
`

// hashFunc defines gbxhash, which prints "<hex> ..." for a file with the
// first hash tool the host has, and returns 127 when it has none. shasum
// comes first: every macOS release has it, and sha256sum only recent ones.
const hashFunc = `
gbxhash() { if command -v shasum >/dev/null 2>&1; then shasum -a 256 "$1";
elif command -v sha256sum >/dev/null 2>&1; then sha256sum "$1";
elif command -v openssl >/dev/null 2>&1; then openssl dgst -sha256 -r "$1";
else return 127; fi; };
`

// prepareScript creates the build folder and prints its path and the free
// space there, in KiB. Arguments: mode, path, build.
const prepareScript = `umask 077;` + rootPrelude + `
mkdir -p "$r" || exit 1;` + refuseRoot + `
d="$r/$3";
mkdir -p "$d" || exit 1;
p=$(cd "$d" && pwd -P) || exit 1;
set -- $(df -Pk "$d" | tail -n 1);
printf "\0GBX\0ok\0%s\0%s\0" "$p" "$4"
`

// uploadScript writes stdin to a temporary file in the build folder.
// Arguments: mode, path, build, random suffix. Nothing goes to /tmp.
const uploadScript = `umask 077;` + rootPrelude + `
cat > "$r/$3/.gitbox-server.$4.tmp"
`

// finalizeScript checks the hash of the uploaded file first and stops on a
// mismatch, so a file with the wrong hash is never executed. Then it runs
// the file once, which fails with 126 on a noexec mount, and renames it into
// place. Arguments: mode, path, build, random suffix, pin.
const finalizeScript = `umask 077;` + rootPrelude + `
d="$r/$3"; t="$d/.gitbox-server.$4.tmp"; f="$d/gitbox-server";` + hashFunc + `
x=$(gbxhash "$t"); c=$?;
if [ "$c" -eq 127 ]; then rm -f "$t"; printf "\0GBX\0nohash\0\0"; exit 0; fi;
if [ "$c" -ne 0 ]; then rm -f "$t"; printf "\0GBX\0missing\0\0"; exit 0; fi;
x=${x%% *};
[ "$x" = "$5" ] || { rm -f "$t"; printf "\0GBX\0mismatch\0%s\0" "$x"; exit 0; };
chmod 700 "$t" || exit 1;
o=$("$t" --version 2>&1); c=$?;
case "$c:$o" in 126:*|*"Permission denied"*) rm -f "$t"; printf "\0GBX\0noexec\0%s\0" "$x"; exit 0;; esac;
if [ "$c" -ne 0 ]; then rm -f "$t"; printf "\0GBX\0runfail\0%s\0" "$x"; exit 0; fi;
mv -f "$t" "$f" || exit 1;
printf "\0GBX\0ok\0%s\0" "$x"
`

// install uploads the server of t into the build folder. It runs only when
// the final file is missing: a final file with the wrong hash is never
// replaced silently, it fails verification instead.
func (r *runner) install(ctx context.Context, l layout, build string, t Target) error {
	if r.o.OnInstall != nil {
		r.o.OnInstall()
	}
	r.log.Printf("install: %s is missing, installing it (pin %s)", build, t.SHA256)

	res, err := r.run(ctx, "install: prepare", prepareScript, nil, l.args(build)...)
	if err != nil {
		return err
	}
	if status(res) == "refused" {
		return newError(CodeServerDirUnsafe, "server_dir %s is the home or root folder; choose a folder of its own", r.o.ServerDir)
	}
	if status(res) != "ok" || len(res.fields) < 3 {
		return newError(CodeSSHFailed, "the install folder on %s could not be prepared", r.alias)
	}
	dir := clean(res.fields[1])
	freeKiB, perr := strconv.ParseInt(strings.TrimSpace(res.fields[2]), 10, 64)
	need := 2 * t.Size / 1024
	r.log.Printf("install: %s has %s KiB free, needs %d", dir, clean(res.fields[2]), need)
	if perr == nil && freeKiB < need {
		return newError(CodeDiskFull, "%s on %s has %d KiB free and the server needs %d", dir, r.alias, freeKiB, need)
	}

	bin, err := t.Load()
	if err != nil {
		return newError(CodeServerTargetMissing, "the server binary for this host could not be read from this GitboxApp")
	}
	suffix := randomHex(8)
	var stream io.Reader = bytes.NewReader(bin)
	if r.o.uploadFilter != nil {
		stream = r.o.uploadFilter(stream)
	}
	if _, err := r.runPlain(ctx, "install: upload", uploadScript, stream, l.args(build, suffix)...); err != nil {
		return err
	}

	res, err = r.run(ctx, "install: verify", finalizeScript, nil, l.args(build, suffix, t.SHA256)...)
	if err != nil {
		return err
	}
	got := ""
	if len(res.fields) > 1 {
		got = clean(res.fields[1])
	}
	r.log.Printf("install: uploaded file hash %s, pin %s, result %s", got, t.SHA256, clean(status(res)))
	switch status(res) {
	case "ok":
		if got != t.SHA256 {
			return newError(CodeHashMismatch, "the server installed on %s does not match its pin", r.alias)
		}
		r.log.Printf("install: %s installed", build)
		return nil
	case "mismatch", "missing":
		return newError(CodeHashMismatch, "the server uploaded to %s does not match its pin and was deleted", r.alias)
	case "nohash":
		return newError(CodeNoHashTool, "%s has none of shasum, sha256sum or openssl to verify the server", r.alias)
	case "noexec":
		return newError(CodeNoexec, "%s cannot run programs from %s (mounted noexec); set server_dir to a folder that allows it", r.alias, dir)
	case "runfail":
		return newError(CodeServerCrashed, "the server uploaded to %s did not start", r.alias)
	}
	return newError(CodeSSHFailed, "the install on %s gave no answer", r.alias)
}

// runPlain runs a remote command whose output is not parsed and reports its
// exit.
func (r *runner) runPlain(ctx context.Context, step, script string, stdin io.Reader, args ...string) (int, error) {
	cmd := r.command(Argv(r.alias, r.o, remoteCommand(script, args...))...)
	cmd.Stdin = stdin
	stderr := newTail(64 << 10)
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return -1, newError(CodeSSHMissing, "ssh could not be started")
	}
	stop := context.AfterFunc(ctx, func() { _ = cmd.Process.Kill() })
	err := cmd.Wait()
	stop()
	code := exitCode(err)
	r.log.Printf("%s: exit %d", step, code)
	r.logStderr(step, stderr.Bytes(), stderr.total())
	if ctx.Err() != nil {
		return code, newError(CodeSSHTimeout, "%s was cancelled", step)
	}
	if err != nil {
		if code == 255 {
			return code, classifySSH(r.alias, code, stderr.Bytes())
		}
		return code, newError(CodeSSHFailed, "%s on %s failed with exit code %d", step, r.alias, code)
	}
	return 0, nil
}

func status(res *result) string {
	if res == nil || len(res.fields) == 0 {
		return ""
	}
	return res.fields[0]
}
