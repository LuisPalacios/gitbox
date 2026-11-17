// Command client drives the throwaway P0 spike (#234): it uploads the spike
// server over the SSH transport, verifies it, starts it, and measures the
// RPC round trip, status payload, banner tolerance and cold start. It also
// measures the cost of embedding six server binaries. Never merged.
package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
)

var magic = []byte{0x00, 0xFE, 'g', 'b'}

const bannerLimit = 1 << 20

type summary map[string]any

func main() {
	host := flag.String("host", "", "ssh alias of the remote")
	n := flag.Int("n", 1000, "ping calls")
	statusCalls := flag.Int("status", 50, "status(300) calls")
	banner := flag.Int("banner", 0, "bytes of banner noise injected on stdout and stderr before exec (POSIX)")
	sizeReport := flag.Bool("size-report", false, "build the six targets locally and report sizes")
	baseline := flag.String("baseline", "", "path of an existing GitboxApp binary for the size baseline")
	gcm := flag.Bool("gcm", false, "probe git credential fill in the SSH session")
	noexec := flag.String("noexec", "", "remote directory mounted noexec to test running the binary from")
	keep := flag.Bool("keep", false, "keep the remote install")
	wrap := flag.String("wrap", "", "remote command that starts the installed server instead of the default (e.g. a docker run -i wrapper)")
	flag.Parse()

	out := summary{}
	var err error
	switch {
	case *sizeReport:
		err = sizeRun(out, *baseline)
	case *host == "":
		err = errors.New("-host is required")
	default:
		err = remoteRun(out, *host, *n, *statusCalls, *banner, *gcm, *noexec, *keep, *wrap)
	}
	if err != nil {
		out["error"] = err.Error()
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
	if err != nil {
		os.Exit(1)
	}
}

// ---------- ssh helpers ----------

func sshArgs(host, remote string) []string {
	return []string{"-T", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "-o", "ServerAliveInterval=15", "-e", "none", host, remote}
}

type res struct {
	out, err string
	code     int
	dur      time.Duration
}

func run(host, remote string, stdin []byte, timeout time.Duration) res {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ssh", sshArgs(host, remote)...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	t := time.Now()
	err := cmd.Run()
	r := res{out: so.String(), err: se.String(), dur: time.Since(t)}
	var ee *exec.ExitError
	switch {
	case errors.As(err, &ee):
		r.code = ee.ExitCode()
	case err != nil:
		r.code = -1
		r.err += err.Error()
	}
	if ctx.Err() != nil {
		r.code = -2
		r.err += " [timeout]"
	}
	return r
}

// encodedPS returns a PowerShell invocation that works whatever the Windows
// DefaultShell is: -EncodedCommand avoids every layer of quoting.
func encodedPS(script string) string {
	u := utf16.Encode([]rune(script))
	b := make([]byte, 2*len(u))
	for i, c := range u {
		binary.LittleEndian.PutUint16(b[2*i:], c)
	}
	return "powershell -NoProfile -NonInteractive -EncodedCommand " + base64.StdEncoding.EncodeToString(b)
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

func pct(ds []time.Duration, p float64) float64 {
	if len(ds) == 0 {
		return 0
	}
	s := append([]time.Duration(nil), ds...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return ms(s[int(p*float64(len(s)-1))])
}

// ---------- remote run ----------

type target struct{ goos, goarch string }

func probe(host string) (target, string, error) {
	r := run(host, `sh -c 'printf "\0GBX\0%s %s\0" "$(uname -s)" "$(uname -m)"'`, nil, 30*time.Second)
	if i := strings.LastIndex(r.out, "\x00GBX\x00"); r.code == 0 && i >= 0 {
		f := strings.Fields(strings.TrimRight(r.out[i+5:], "\x00\n"))
		if len(f) == 2 {
			arch := map[string]string{"x86_64": "amd64", "amd64": "amd64", "aarch64": "arm64", "arm64": "arm64"}[f[1]]
			for _, pre := range []string{"MINGW", "MSYS", "CYGWIN"} {
				if strings.HasPrefix(f[0], pre) && arch != "" {
					// A Windows host whose DefaultShell (or PATH) is Git Bash.
					return target{"windows", arch}, "windows-bash", nil
				}
			}
			goos := map[string]string{"Linux": "linux", "Darwin": "darwin"}[f[0]]
			if goos != "" && arch != "" {
				return target{goos, arch}, "posix", nil
			}
		}
	}
	w := run(host, "cmd /c echo %PROCESSOR_ARCHITECTURE%", nil, 30*time.Second)
	if w.code == 0 {
		a := strings.ToUpper(strings.TrimSpace(w.out))
		arch := map[string]string{"AMD64": "amd64", "ARM64": "arm64"}[a]
		if arch != "" {
			return target{"windows", arch}, "windows", nil
		}
	}
	return target{}, "", fmt.Errorf("probe failed: posix=%.200q windows=%.200q", strings.TrimSpace(r.err), strings.TrimSpace(w.err))
}

func build(t target, dst string) error {
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags", "-s -w", "-o", dst, "./spike/remote/server")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+t.goos, "GOARCH="+t.goarch)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

type remoteCmds struct {
	clean, upload, hash, place, start, nap func() string
}

func posixCmds(darwin bool) remoteCmds {
	// The marker makes the parse immune to login-shell banners on stdout.
	hash := `sh -c 'printf "\0GBX\0"; sha256sum ~/.gitbox-spike/.s.tmp'`
	if darwin {
		hash = `sh -c 'printf "\0GBX\0"; shasum -a 256 ~/.gitbox-spike/.s.tmp'`
	}
	return remoteCmds{
		clean:  func() string { return "rm -rf ~/.gitbox-spike" },
		upload: func() string { return "sh -c 'umask 077 && mkdir -p ~/.gitbox-spike && cat > ~/.gitbox-spike/.s.tmp'" },
		hash:   func() string { return hash },
		place: func() string {
			return "chmod 700 ~/.gitbox-spike/.s.tmp && mv -f ~/.gitbox-spike/.s.tmp ~/.gitbox-spike/s"
		},
		start: func() string { return "exec ~/.gitbox-spike/s" },
	}
}

func windowsCmds(bash bool) remoteCmds {
	dir := `$d = Join-Path $env:USERPROFILE '.gitbox-spike'; `
	return remoteCmds{
		clean: func() string {
			return encodedPS(dir + `if (Test-Path $d) { Remove-Item -Recurse -Force $d }`)
		},
		upload: func() string {
			return encodedPS(dir + `New-Item -ItemType Directory -Force $d | Out-Null; $in = [Console]::OpenStandardInput(); $f = [IO.File]::Create((Join-Path $d '.s.tmp')); $in.CopyTo($f); $f.Close()`)
		},
		hash: func() string {
			return encodedPS(dir + `(Get-FileHash -Algorithm SHA256 (Join-Path $d '.s.tmp')).Hash`)
		},
		place: func() string {
			return encodedPS(dir + `Move-Item -Force (Join-Path $d '.s.tmp') (Join-Path $d 's.exe')`)
		},
		start: func() string {
			if bash {
				return `exec "$USERPROFILE/.gitbox-spike/s.exe"`
			}
			return `cmd /c "%USERPROFILE%\.gitbox-spike\s.exe"`
		},
	}
}

type session struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	r       *bufio.Reader
	stderr  *bytes.Buffer
	skipped int
	hello   map[string]any
	nextID  int64
}

func startSession(host, remote string) (*session, error) {
	cmd := exec.Command("ssh", sshArgs(host, remote)...)
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	se := &bytes.Buffer{}
	cmd.Stderr = se
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	s := &session{cmd: cmd, stdin: in, r: bufio.NewReaderSize(out, 64<<10), stderr: se}
	// Skip banner bytes until the magic, up to bannerLimit.
	window := make([]byte, 0, len(magic))
	for {
		b, err := s.r.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("before magic after %d bytes: %w (stderr: %.300s)", s.skipped, err, se.String())
		}
		window = append(window, b)
		if len(window) > len(magic) {
			window = window[1:]
		}
		if bytes.Equal(window, magic) {
			break
		}
		s.skipped++
		if s.skipped > bannerLimit+len(magic) {
			return nil, fmt.Errorf("banner_overflow after %d bytes", s.skipped)
		}
	}
	s.skipped -= len(magic) - 1
	hb, err := readFrame(s.r)
	if err != nil {
		return nil, fmt.Errorf("hello: %w", err)
	}
	if err := json.Unmarshal(hb, &s.hello); err != nil {
		return nil, fmt.Errorf("hello decode: %w", err)
	}
	return s, nil
}

func (s *session) call(method string, params any) ([]byte, time.Duration, error) {
	s.nextID++
	req := map[string]any{"jsonrpc": "2.0", "id": s.nextID, "method": method}
	if params != nil {
		req["params"] = params
	}
	b, _ := json.Marshal(req)
	t := time.Now()
	if _, err := fmt.Fprintf(s.stdin, "Content-Length: %d\r\n\r\n%s", len(b), b); err != nil {
		return nil, 0, err
	}
	resp, err := readFrame(s.r)
	return resp, time.Since(t), err
}

func (s *session) close() time.Duration {
	t := time.Now()
	_ = s.stdin.Close()
	done := make(chan struct{})
	go func() { _ = s.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		_ = s.cmd.Process.Kill()
	}
	return time.Since(t)
}

func readFrame(r *bufio.Reader) ([]byte, error) {
	n := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if k, v, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(k), "Content-Length") {
			if n, err = strconv.Atoi(strings.TrimSpace(v)); err != nil {
				return nil, err
			}
		}
	}
	if n < 0 || n > 16<<20 {
		return nil, fmt.Errorf("bad frame length %d", n)
	}
	buf := make([]byte, n)
	_, err := io.ReadFull(r, buf)
	return buf, err
}

func channelRTT(host string, rounds int) ([]time.Duration, error) {
	cmd := exec.Command("ssh", sshArgs(host, "cat")...)
	in, _ := cmd.StdinPipe()
	out, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	defer func() { _ = in.Close(); _ = cmd.Wait() }()
	r := bufio.NewReader(out)
	var ds []time.Duration
	for i := 0; i < rounds; i++ {
		t := time.Now()
		if _, err := in.Write([]byte("x\n")); err != nil {
			return ds, err
		}
		if _, err := r.ReadString('\n'); err != nil {
			return ds, err
		}
		if i > 0 { // first round includes remote cat startup
			ds = append(ds, time.Since(t))
		}
	}
	return ds, nil
}

func remoteRun(out summary, host string, n, statusCalls, banner int, gcm bool, noexecDir string, keep bool, wrap string) error {
	t, family, err := probe(host)
	if err != nil {
		return err
	}
	out["target"] = t.goos + "/" + t.goarch
	out["family"] = family
	var rc remoteCmds
	if strings.HasPrefix(family, "windows") {
		if family == "windows-bash" {
			// Git Bash answers: the POSIX path works, with an .exe name.
			rc = posixCmds(false)
			rc.place = func() string {
				return "mv -f ~/.gitbox-spike/.s.tmp ~/.gitbox-spike/s.exe"
			}
			rc.start = func() string { return "exec ~/.gitbox-spike/s.exe" }
		} else {
			rc = windowsCmds(false)
		}
		ds := run(host, encodedPS(`$ProgressPreference = 'SilentlyContinue'; $v = (Get-ItemProperty 'HKLM:\SOFTWARE\OpenSSH' -ErrorAction SilentlyContinue).DefaultShell; if ($v) { $v } else { '<unset: cmd.exe>' }`), nil, 60*time.Second)
		out["windows_defaultshell"] = strings.TrimSpace(ds.out)
		out["windows_defaultshell_stderr"] = strings.TrimSpace(ds.err)
	} else {
		rc = posixCmds(t.goos == "darwin")
	}

	bin := filepath.Join(os.TempDir(), fmt.Sprintf("gbx-spike-%s-%s", t.goos, t.goarch))
	if err := build(t, bin); err != nil {
		return fmt.Errorf("build: %w", err)
	}
	data, err := os.ReadFile(bin)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	want := hex.EncodeToString(sum[:])
	out["binary_bytes"] = len(data)

	// raw connection setup cost
	var connect []time.Duration
	for i := 0; i < 3; i++ {
		connect = append(connect, run(host, "exit 0", nil, 30*time.Second).dur)
	}
	out["ssh_connect_ms_p50"] = pct(connect, 0.5)

	// cold start: clean, upload, verify, place, start, first ping
	if r := run(host, rc.clean(), nil, 60*time.Second); r.code != 0 {
		return fmt.Errorf("clean: %s", r.err)
	}
	cold := time.Now()
	up := run(host, rc.upload(), data, 5*time.Minute)
	if up.code != 0 {
		return fmt.Errorf("upload exit %d: %.400s", up.code, up.err)
	}
	hv := run(host, rc.hash(), nil, 60*time.Second)
	hashOut := hv.out
	if i := strings.LastIndex(hashOut, "\x00GBX\x00"); i >= 0 {
		hashOut = hashOut[i+5:]
	}
	got := strings.ToLower(strings.Fields(hashOut + " x")[0])
	out["hash_banner_bytes"] = len(hv.out) - len(hashOut)
	out["upload_ms"], out["verify_ms"] = ms(up.dur), ms(hv.dur)
	out["hash_ok"] = got == want
	if got != want {
		return fmt.Errorf("hash mismatch: got %q (stderr %.200s)", got, hv.err)
	}
	pl := run(host, rc.place(), nil, 60*time.Second)
	if pl.code != 0 {
		return fmt.Errorf("place exit %d: %.400s", pl.code, pl.err)
	}
	startCmd := rc.start()
	if wrap != "" {
		startCmd = wrap
		out["wrap"] = true
	}
	if banner > 0 {
		startCmd = fmt.Sprintf(`sh -c 'yes "Welcome to my-server: noisy login banner line" | head -c %d; yes "warning: noisy stderr" | head -c %d >&2; exec ~/.gitbox-spike/s'`, banner, banner)
		out["banner_bytes"] = banner
	}
	s, err := startSession(host, startCmd)
	if err != nil {
		return fmt.Errorf("start: %w", err)
	}
	if _, _, err := s.call("ping", nil); err != nil {
		return fmt.Errorf("first ping: %w", err)
	}
	out["cold_start_ms"] = ms(time.Since(cold))
	out["banner_skipped_bytes"] = s.skipped
	out["hello"] = s.hello

	// channel baseline inside an established ssh session
	base, err := channelRTT(host, 201)
	if err != nil {
		return fmt.Errorf("channel rtt: %w", err)
	}
	out["channel_rtt_ms_p50"], out["channel_rtt_ms_p95"] = pct(base, 0.5), pct(base, 0.95)

	var pings []time.Duration
	for i := 0; i < n; i++ {
		_, d, err := s.call("ping", nil)
		if err != nil {
			return fmt.Errorf("ping %d: %w", i, err)
		}
		pings = append(pings, d)
	}
	out["ping_ms_p50"], out["ping_ms_p95"] = pct(pings, 0.5), pct(pings, 0.95)
	out["ping_overhead_ms_p50"] = pct(pings, 0.5) - pct(base, 0.5)

	var stats, decodes []time.Duration
	var payload int
	for i := 0; i < statusCalls; i++ {
		b, d, err := s.call("status", []int{300})
		if err != nil {
			return fmt.Errorf("status %d: %w", i, err)
		}
		payload = len(b)
		t0 := time.Now()
		var resp struct {
			Result []map[string]any `json:"result"`
		}
		if err := json.Unmarshal(b, &resp); err != nil || len(resp.Result) != 300 {
			return fmt.Errorf("status decode: %v (%d)", err, len(resp.Result))
		}
		decodes = append(decodes, time.Since(t0))
		stats = append(stats, d)
	}
	out["status300_bytes"] = payload
	out["status300_ms_p50"], out["status300_ms_p95"] = pct(stats, 0.5), pct(stats, 0.95)
	out["status300_decode_ms_p50"] = pct(decodes, 0.5)

	if b, _, err := s.call("doctor", nil); err == nil {
		out["doctor"] = json.RawMessage(b)
	}
	if b, _, err := s.call("realstatus", nil); err == nil {
		out["realstatus"] = json.RawMessage(b)
	}
	out["exit_after_stdin_close_ms"] = ms(s.close())
	out["server_stderr"] = strings.TrimSpace(s.stderr.String())

	if gcm {
		out["gcm"] = gcmProbe(host, family)
	}
	if noexecDir != "" && family == "posix" {
		r := run(host, fmt.Sprintf(`sh -c 'cp ~/.gitbox-spike/s %[1]s/gbx-s && chmod 700 %[1]s/gbx-s && %[1]s/gbx-s --version; echo "exit=$?"; rm -f %[1]s/gbx-s'`, noexecDir), nil, 60*time.Second)
		out["noexec"] = map[string]any{"stdout": strings.TrimSpace(r.out), "stderr": strings.TrimSpace(r.err)}
	}
	if !keep {
		run(host, rc.clean(), nil, 60*time.Second)
	}
	return nil
}

func gcmProbe(host, family string) map[string]any {
	var r res
	if strings.HasPrefix(family, "windows") {
		r = run(host, encodedPS("$h = git config --global --get credential.helper; $s = git config --global --get credential.credentialStore; \"helper=$h store=$s\"; $env:GIT_TERMINAL_PROMPT='0'; $env:GCM_INTERACTIVE='never'; \"protocol=https`nhost=example.invalid`n\" | git credential fill 2>&1; \"exit=$LASTEXITCODE\""), nil, 40*time.Second)
	} else {
		r = run(host, `sh -c 'echo "helper=$(git config --global --get credential.helper) store=$(git config --global --get credential.credentialStore)"; printf "protocol=https\nhost=example.invalid\n\n" | GIT_TERMINAL_PROMPT=0 GCM_INTERACTIVE=never timeout 30 git credential fill 2>&1; echo "exit=$?"'`, nil, 40*time.Second)
	}
	return map[string]any{"stdout": strings.TrimSpace(r.out), "stderr": strings.TrimSpace(r.err), "code": r.code}
}

// ---------- size report ----------

func sizeRun(out summary, baseline string) error {
	targets := []target{{"linux", "amd64"}, {"linux", "arm64"}, {"darwin", "amd64"}, {"darwin", "arm64"}, {"windows", "amd64"}, {"windows", "arm64"}}
	dir, err := os.MkdirTemp("", "gbx-size")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	var rows []map[string]any
	var totalRaw, totalGz int
	start := time.Now()
	for _, t := range targets {
		p := filepath.Join(dir, t.goos+"-"+t.goarch)
		tb := time.Now()
		if err := build(t, p); err != nil {
			return err
		}
		bd := time.Since(tb)
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		tg := time.Now()
		var buf bytes.Buffer
		zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
		_, _ = zw.Write(raw)
		_ = zw.Close()
		rows = append(rows, map[string]any{"target": t.goos + "/" + t.goarch, "raw_bytes": len(raw), "gzip_bytes": buf.Len(), "build_ms": ms(bd), "gzip_ms": ms(time.Since(tg))})
		totalRaw += len(raw)
		totalGz += buf.Len()
	}
	out["targets"] = rows
	out["total_raw_bytes"], out["total_gzip_bytes"] = totalRaw, totalGz
	out["total_wall_ms"] = ms(time.Since(start))
	if baseline != "" {
		if fi, err := os.Stat(baseline); err == nil {
			out["baseline_gui_bytes"] = fi.Size()
			out["gui_growth_pct"] = 100 * float64(totalGz) / float64(fi.Size())
		}
	}
	return nil
}
