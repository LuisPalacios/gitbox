package remote

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// startScript verifies the installed server and starts it, in one exec. It
// prints the hash after the marker and runs the file only when the hash is
// the pin the client passed. Only a final file that does not exist answers
// "missing", which leads to an install; one that exists but cannot be hashed
// answers "unreadable", so it is never replaced silently. Arguments: mode, path, build, pin, action
// ("serve" or "doctor"), client id.
const startScript = rootPrelude + `
f="$r/$3/gitbox-server";
[ -f "$f" ] || { printf "\0GBX\0missing\0\0"; exit 0; };` + hashFunc + `
x=$(gbxhash "$f"); c=$?;
if [ "$c" -eq 127 ]; then printf "\0GBX\0nohash\0\0"; exit 0; fi;
if [ "$c" -ne 0 ]; then printf "\0GBX\0unreadable\0\0"; exit 97; fi;
x=${x%% *};
[ "$x" = "$4" ] || { printf "\0GBX\0mismatch\0%s\0" "$x"; exit 97; };
printf "\0GBX\0ok\0%s\0" "$x";
if [ "$5" = doctor ]; then exec "$f" --doctor; fi;
exec "$f" serve --stdio --client-id "$6"
`

// Session is a running gitbox-server on a remote, verified against its pin
// before it started.
type Session struct {
	r      *runner
	probe  Probe
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *os.File // the read end of the server's stdout
	out    *bufio.Reader
	errs   *tail

	done    chan struct{}
	waitErr error

	closeOnce sync.Once
	closing   bool
	mu        sync.Mutex
}

// Dial connects to alias and starts the server of this build there,
// installing it first when it is missing. The returned session's Conn
// carries the server's protocol; the handshake is the caller's.
func Dial(ctx context.Context, alias string, o Options) (*Session, error) {
	r, err := newRunner(alias, o)
	if err != nil {
		return nil, err
	}
	if !clientIDRE.MatchString(o.ClientID) {
		return nil, newError(CodeSSHConfig, "invalid client id")
	}
	if o.Target == nil {
		return nil, newError(CodeServerTargetMissing, "this GitboxApp carries no server")
	}
	l, err := newLayout(o.ServerDir)
	if err != nil {
		return nil, err
	}
	t0 := time.Now()
	r.setupControl(ctx)
	probe, err := r.probe(ctx)
	if err != nil {
		return nil, err
	}
	t, err := o.Target(probe.OS, probe.Arch)
	if err != nil {
		if e, ok := errors.AsType[*Error](err); ok {
			return nil, e
		}
		return nil, newError(CodeServerTargetMissing, "this GitboxApp carries no server for %s/%s", probe.OS, probe.Arch)
	}
	build, err := buildFor(o, t)
	if err != nil {
		return nil, err
	}
	for attempt := 0; ; attempt++ {
		s, missing, err := r.start(ctx, l, build, t.SHA256, "serve")
		if err != nil {
			return nil, err
		}
		if !missing {
			s.probe = probe
			r.log.Printf("connect: server %s started in %s", build, time.Since(t0).Round(time.Millisecond))
			return s, nil
		}
		if attempt > 0 {
			return nil, newError(CodeSSHFailed, "the server installed on %s disappeared before it started", alias)
		}
		if err := r.install(ctx, l, build, t); err != nil {
			return nil, err
		}
	}
}

func buildFor(o Options, t Target) (string, error) {
	if !pinRE.MatchString(t.SHA256) {
		return "", newError(CodeServerTargetMissing, "the server this GitboxApp carries has no valid pin")
	}
	build := BuildName(o.Version, o.Commit, t.SHA256)
	if !BuildNameRE.MatchString(build) {
		return "", newError(CodeUnstampedBuild, "this GitboxApp has no build stamp, which remote mode needs")
	}
	return build, nil
}

// start runs the start script. missing reports a server that is not
// installed yet; the session is nil then.
func (r *runner) start(ctx context.Context, l layout, build, pin, action string) (s *Session, missing bool, err error) {
	t0 := time.Now()
	cmd := r.command(Argv(r.alias, r.o, remoteCommand(startScript, l.args(build, pin, action, r.o.ClientID)...))...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, false, err
	}
	// The session owns the read end of stdout. With StdoutPipe, the Wait
	// below would close it as soon as ssh exits, and a short answer still
	// in the pipe after a long banner would be lost.
	stdout, stdoutW, err := os.Pipe()
	if err != nil {
		return nil, false, err
	}
	cmd.Stdout = stdoutW
	s = &Session{r: r, cmd: cmd, stdin: stdin, stdout: stdout, errs: newTail(64 << 10), done: make(chan struct{})}
	pr, pw := io.Pipe()
	cmd.Stderr = pw
	err = cmd.Start()
	_ = stdoutW.Close()
	if err != nil {
		_ = stdout.Close()
		return nil, false, newError(CodeSSHMissing, "ssh could not be started")
	}
	go s.readStderr(pr)
	go func() {
		s.waitErr = cmd.Wait()
		_ = pw.Close()
		close(s.done)
	}()
	stop := context.AfterFunc(ctx, s.kill)
	defer stop()

	s.out = bufio.NewReaderSize(stdout, 64<<10)
	found, banner, readErr := skipMarker(s.out, maxBanner)
	var st, hash string
	if found {
		st, readErr = readField(s.out)
		if readErr == nil {
			hash, readErr = readField(s.out)
		}
	}
	r.log.Printf("start: %s in %s [banner: %d bytes]", clean(st), time.Since(t0).Round(time.Millisecond), banner)

	fail := func(e *Error) (*Session, bool, error) {
		s.kill()
		<-s.done
		_ = stdout.Close()
		return nil, false, e
	}
	switch {
	case ctx.Err() != nil:
		return fail(newError(CodeSSHTimeout, "connecting to %s was cancelled", r.alias))
	case !found && errors.Is(readErr, errOverflow):
		return fail(newError(CodeBannerOverflow, "the login shell of %s printed more than 1 MiB before the answer", r.alias))
	case !found || readErr != nil:
		s.kill()
		<-s.done
		_ = stdout.Close()
		return nil, false, s.exitError()
	}
	switch st {
	case "missing":
		_ = stdin.Close()
		<-s.done
		_ = stdout.Close()
		return nil, true, nil
	case "nohash":
		return fail(newError(CodeNoHashTool, "%s has none of shasum, sha256sum or openssl to verify the server", r.alias))
	case "unreadable":
		r.log.Printf("start: the installed file cannot be hashed: not started, not reinstalled")
		return fail(newError(CodeHashMismatch, "the server installed on %s cannot be read to verify it and was not started", r.alias))
	case "mismatch":
		r.log.Printf("start: installed file hash %s, pin %s: not started", clean(hash), pin)
		return fail(newError(CodeHashMismatch, "the server installed on %s does not match its pin and was not started", r.alias))
	case "ok":
	default:
		return fail(newError(CodeSSHFailed, "the start on %s gave an unexpected answer", r.alias))
	}
	// The host says the hash matched; the client checks it too before it
	// speaks to the server.
	if hash != pin {
		r.log.Printf("start: installed file hash %s, pin %s: refused", clean(hash), pin)
		return fail(newError(CodeHashMismatch, "the server installed on %s does not match its pin", r.alias))
	}
	r.log.Printf("start: hash %s matches the pin", pin)
	return s, false, nil
}

// readField reads one NUL-terminated field of at most 4 KiB.
func readField(r *bufio.Reader) (string, error) {
	var b []byte
	for len(b) < 4096 {
		c, err := r.ReadByte()
		if err != nil {
			return "", err
		}
		if c == 0 {
			return string(b), nil
		}
		b = append(b, c)
	}
	return "", errors.New("field too long")
}

func (s *Session) readStderr(r io.Reader) {
	br := bufio.NewReaderSize(io.TeeReader(r, s.errs), 4096)
	skipping := false
	for {
		line, isPrefix, err := br.ReadLine()
		if err != nil {
			return
		}
		if !skipping {
			s.r.log.Printf("remote: %s", line)
		}
		// The rest of an overlong line is dropped.
		skipping = isPrefix
	}
}

func (s *Session) kill() {
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
}

// exitError classifies how the ssh process ended.
func (s *Session) exitError() error {
	code := exitCode(s.waitErr)
	if s.waitErr == nil {
		return newError(CodeServerCrashed, "the server on %s ended", s.r.alias)
	}
	if code == 255 {
		return classifySSH(s.r.alias, code, s.errs.Bytes())
	}
	return newError(CodeServerCrashed, "the server on %s ended with exit code %d", s.r.alias, code)
}

// Probe is what the host said about itself before the server started.
func (s *Session) Probe() Probe { return s.probe }

// Conn is the server's protocol: reads come from its stdout, writes go to
// its stdin, and Close closes its stdin, which tells it to stop.
func (s *Session) Conn() io.ReadWriteCloser { return sessionConn{s} }

type sessionConn struct{ s *Session }

func (c sessionConn) Read(p []byte) (int, error)  { return c.s.out.Read(p) }
func (c sessionConn) Write(p []byte) (int, error) { return c.s.stdin.Write(p) }
func (c sessionConn) Close() error {
	c.s.markClosing()
	return c.s.stdin.Close()
}

func (s *Session) markClosing() {
	s.mu.Lock()
	s.closing = true
	s.mu.Unlock()
}

// Done is closed when the ssh process has ended.
func (s *Session) Done() <-chan struct{} { return s.done }

// Err is how the session ended: nil after Close or a clean exit, otherwise
// the classified failure. It is meaningful once Done is closed.
func (s *Session) Err() error {
	s.mu.Lock()
	closing := s.closing
	s.mu.Unlock()
	if closing {
		return nil
	}
	return s.exitError()
}

// Close stops the server: its stdin closes, and the process is killed when
// it has not ended within five seconds.
func (s *Session) Close() error {
	s.closeOnce.Do(func() {
		s.markClosing()
		_ = s.stdin.Close()
		select {
		case <-s.done:
		case <-time.After(5 * time.Second):
			s.kill()
			<-s.done
		}
		_ = s.stdout.Close()
	})
	return nil
}

// Doctor runs gitbox-server --doctor on the host, verified against the pin
// like a start, and returns its report.
func Doctor(ctx context.Context, alias string, o Options) ([]byte, error) {
	r, err := newRunner(alias, o)
	if err != nil {
		return nil, err
	}
	l, err := newLayout(o.ServerDir)
	if err != nil {
		return nil, err
	}
	r.setupControl(ctx)
	probe, err := r.probe(ctx)
	if err != nil {
		return nil, err
	}
	if o.Target == nil {
		return nil, newError(CodeServerTargetMissing, "this GitboxApp carries no server")
	}
	t, err := o.Target(probe.OS, probe.Arch)
	if err != nil {
		return nil, newError(CodeServerTargetMissing, "this GitboxApp carries no server for %s/%s", probe.OS, probe.Arch)
	}
	build, err := buildFor(o, t)
	if err != nil {
		return nil, err
	}
	s, missing, err := r.start(ctx, l, build, t.SHA256, "doctor")
	if err != nil {
		return nil, err
	}
	if missing {
		return nil, newError(CodeSSHFailed, "the server is not installed on %s; connect first", alias)
	}
	_ = s.stdin.Close()
	report, rerr := io.ReadAll(io.LimitReader(s.out, maxParsed))
	<-s.done
	_ = s.stdout.Close()
	if rerr != nil || s.waitErr != nil {
		return nil, newError(CodeServerCrashed, "gitbox-server --doctor failed on %s", alias)
	}
	r.log.Printf("doctor: %d bytes of report", len(report))
	return bytes.TrimSpace(report), nil
}

// reinstallScript deletes one build folder. Arguments: mode, path, build.
const reinstallScript = rootPrelude + `
[ -d "$r" ] || { printf "\0GBX\0done\0"; exit 0; };` + refuseRoot + `
rm -rf -- "$r/$3";
printf "\0GBX\0done\0"
`

// Reinstall deletes the build folder of this build on the host, so the next
// connect installs it again.
func Reinstall(ctx context.Context, alias string, o Options) error {
	r, err := newRunner(alias, o)
	if err != nil {
		return err
	}
	l, err := newLayout(o.ServerDir)
	if err != nil {
		return err
	}
	r.setupControl(ctx)
	probe, err := r.probe(ctx)
	if err != nil {
		return err
	}
	if o.Target == nil {
		return newError(CodeServerTargetMissing, "this GitboxApp carries no server")
	}
	t, err := o.Target(probe.OS, probe.Arch)
	if err != nil {
		return newError(CodeServerTargetMissing, "this GitboxApp carries no server for %s/%s", probe.OS, probe.Arch)
	}
	build, err := buildFor(o, t)
	if err != nil {
		return err
	}
	res, err := r.run(ctx, "reinstall", reinstallScript, nil, l.args(build)...)
	if err != nil {
		return err
	}
	if status(res) == "refused" {
		return newError(CodeServerDirUnsafe, "server_dir %s is the home or root folder; nothing was deleted", o.ServerDir)
	}
	r.log.Printf("reinstall: removed %s", build)
	return nil
}

// markersScript lists the client ids of the live markers under the root.
// Arguments: mode, path.
const markersScript = rootPrelude + `
printf "\0GBX\0";
[ -d "$r" ] || exit 0;
find "$r" -mindepth 2 -maxdepth 2 -type f -name "in-use.*" -mmin -10 2>/dev/null | while read -r p; do printf "%s\0" "${p##*/in-use.}"; done
`

// uninstallScript deletes what gitbox put under the root and the root when
// it is then empty. Anything else in the root stays: in logs/ it deletes
// only build logs, like garbage collection, and the folder when it is then
// empty. Arguments: mode, path.
const uninstallScript = rootPrelude + `
[ -d "$r" ] || { printf "\0GBX\0absent\0"; exit 0; };` + refuseRoot + `
cd "$real" || exit 1;
for e in * .gc.lock .gitbox-server.*.tmp; do
[ -e "$e" ] || continue;
case "$e" in
-*) ;;
.gc.lock) rm -rf -- "$e";;
logs) if [ -d logs ] && [ -L logs ]; then :; elif [ -d logs ]; then
for g in logs/*.log logs/*.log.1 logs/*.log.2 logs/*.log.3; do
[ -f "$g" ] || continue;
n=${g#logs/}; b=${n%.log*};
if expr "$b" : "[A-Za-z0-9][A-Za-z0-9._+-]*-[0-9a-f]\{40\}-[0-9a-f]\{8\}\$" >/dev/null; then rm -f -- "$g"; fi;
done;
rmdir logs 2>/dev/null; fi;;
.gitbox-server.*.tmp) rm -f -- "$e";;
*) if expr "$e" : "[A-Za-z0-9][A-Za-z0-9._+-]*-[0-9a-f]\{40\}-[0-9a-f]\{8\}\$" >/dev/null; then rm -rf -- "$e"; fi;;
esac;
done;
cd / || exit 1;
if rmdir -- "$real" 2>/dev/null; then printf "\0GBX\0done\0removed\0"; else printf "\0GBX\0done\0kept\0"; fi
`

// UninstallResult says what Uninstall did.
type UninstallResult struct {
	// OtherClients is how many other clients have a live server there.
	OtherClients int
	// Done is false when Uninstall stopped to ask, because OtherClients is
	// not zero and force was not set.
	Done bool
	// RootKept is true when the root held files that gitbox did not put
	// there, which stay.
	RootKept bool
}

// Uninstall removes the server from alias. When another client has a live
// server there, it stops and reports it unless force is set.
func Uninstall(ctx context.Context, alias string, o Options, force bool) (UninstallResult, error) {
	var out UninstallResult
	r, err := newRunner(alias, o)
	if err != nil {
		return out, err
	}
	l, err := newLayout(o.ServerDir)
	if err != nil {
		return out, err
	}
	r.setupControl(ctx)
	res, err := r.run(ctx, "uninstall: markers", markersScript, nil, l.args()...)
	if err != nil {
		return out, err
	}
	for _, id := range res.fields {
		if id != "" && id != o.ClientID {
			out.OtherClients++
		}
	}
	r.log.Printf("uninstall: %d other live clients", out.OtherClients)
	if out.OtherClients > 0 && !force {
		return out, nil
	}
	res, err = r.run(ctx, "uninstall", uninstallScript, nil, l.args()...)
	if err != nil {
		return out, err
	}
	switch status(res) {
	case "refused":
		return out, newError(CodeServerDirUnsafe, "server_dir %s is the home or root folder; nothing was deleted", o.ServerDir)
	case "absent":
		r.log.Printf("uninstall: nothing installed")
	case "done":
		out.RootKept = len(res.fields) > 1 && res.fields[1] == "kept"
		if out.RootKept {
			r.log.Printf("uninstall: removed; the root keeps files gitbox did not create")
		} else {
			r.log.Printf("uninstall: removed")
		}
	default:
		return out, newError(CodeSSHFailed, "the uninstall on %s gave no answer", alias)
	}
	out.Done = true
	return out, nil
}
