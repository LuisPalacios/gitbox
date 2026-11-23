package remote

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// TestNoConfigOverrideInProduction pins that the argv production code builds
// never weakens or replaces the user's ssh configuration.
func TestNoConfigOverrideInProduction(t *testing.T) {
	o := Options{ServerDir: "~/x", ClientID: "c1", ControlDir: t.TempDir()}
	// The production path: a runner whose control options were chosen.
	reuse := o
	reuse.control = []string{"-o", "ControlMaster=auto", "-o", "ControlPersist=60", "-o", "ControlPath=/tmp/cm-%C"}
	for _, args := range [][]string{
		Argv("my-server", o, remoteCommand(probeScript)),
		Argv("my-server", reuse, remoteCommand(startScript, "h", "x")),
	} {
		for i, a := range args {
			low := strings.ToLower(a)
			if a == "-F" || strings.Contains(low, "stricthostkeychecking") || strings.Contains(low, "userknownhostsfile") {
				t.Errorf("argv[%d] = %q overrides the user's ssh configuration: %q", i, a, args)
			}
		}
		for _, want := range []string{"-T", "BatchMode=yes", "ServerAliveInterval=15", "ServerAliveCountMax=3", "ConnectTimeout=10"} {
			if !slices.Contains(args, want) {
				t.Errorf("argv lacks %q: %q", want, args)
			}
		}
		if i := slices.Index(args, "-e"); i < 0 || args[i+1] != "none" {
			t.Errorf("argv lacks -e none: %q", args)
		}
		if args[len(args)-2] != "my-server" {
			t.Errorf("alias is not the last argument before the command: %q", args)
		}
	}
	// The test hook is what adds -F.
	SetConfigFile(&o, "/tmp/cfg")
	if args := Argv("my-server", o, "true"); !slices.Contains(args, "-F") {
		t.Errorf("the test hook did not add -F: %q", args)
	}
}

func requireSh(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("needs a POSIX sh")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
}

// TestShellQuoteRoundTrip runs quoted words through a real sh.
func TestShellQuoteRoundTrip(t *testing.T) {
	requireSh(t)
	for _, s := range []string{"", "plain", "with space", "it's", `"double"`, "$HOME", "`date`", "a'b'c", "back\\slash", "~/x"} {
		out, err := exec.Command("sh", "-c", "printf %s "+shellQuote(s)).Output()
		if err != nil {
			t.Fatal(err)
		}
		if string(out) != s {
			t.Errorf("quote(%q) came back as %q", s, out)
		}
	}
}

// TestScriptsParse checks every remote script with sh -n, joined to one
// line the way it is sent, and keeps them free of what fish and csh read
// differently inside single quotes.
func TestScriptsParse(t *testing.T) {
	requireSh(t)
	scripts := map[string]string{
		"probe": probeScript, "prepare": prepareScript, "upload": uploadScript, "finalize": finalizeScript,
		"start": startScript, "reinstall": reinstallScript, "markers": markersScript, "uninstall": uninstallScript,
	}
	for name, s := range scripts {
		line := oneLine(s)
		if out, err := exec.Command("sh", "-n", "-c", line).CombinedOutput(); err != nil {
			t.Errorf("%s does not parse: %v\n%s\n%s", name, err, out, line)
		}
		if strings.Contains(line, `\\`) || strings.Contains(line, "!") {
			t.Errorf("%s contains a double backslash or a '!', which fish or csh change inside quotes", name)
		}
		if strings.ContainsAny(line, "\n\r") {
			t.Errorf("%s is not one line", name)
		}
	}
}

func TestProbeResolve(t *testing.T) {
	cases := []struct {
		os, arch       string
		wantOS, wantAr string
		code           Code
	}{
		{"Linux", "x86_64", "linux", "amd64", ""},
		{"Linux", "amd64", "linux", "amd64", ""},
		{"Linux", "aarch64", "linux", "arm64", ""},
		{"Linux", "arm64", "linux", "arm64", ""},
		{"Linux", "riscv64", "", "", CodeUnsupportedArch},
		{"Linux", "armv7l", "", "", CodeUnsupportedArch},
		{"Darwin", "arm64", "", "", CodeUnsupportedOS},
		{"MINGW64_NT-10.0-26300", "x86_64", "", "", CodeUnsupportedOS},
		{"MSYS_NT-10.0", "x86_64", "", "", CodeUnsupportedOS},
		{"CYGWIN_NT-10.0", "x86_64", "", "", CodeUnsupportedOS},
		{"FreeBSD", "amd64", "", "", CodeUnsupportedOS},
	}
	for _, c := range cases {
		p := Probe{RawOS: c.os, RawArch: c.arch}
		err := p.resolve("h")
		if c.code != "" {
			if e, ok := errors.AsType[*Error](err); !ok || e.Code != c.code {
				t.Errorf("%s/%s: err = %v, want %s", c.os, c.arch, err, c.code)
			}
			continue
		}
		if err != nil || p.OS != c.wantOS || p.Arch != c.wantAr {
			t.Errorf("%s/%s = %s/%s, %v", c.os, c.arch, p.OS, p.Arch, err)
		}
	}
}

func TestSkipMarker(t *testing.T) {
	cases := []struct {
		in      string
		found   bool
		discard int
		rest    string
	}{
		{"\x00GBX\x00tail", true, 0, "tail"},
		{"banner\n\x00GBX\x00x", true, 7, "x"},
		{"\x00\x00GBX\x00x", true, 1, "x"},
		{"\x00GB\x00GBX\x00x", true, 3, "x"},
		{"no marker", false, 9, ""},
	}
	for _, c := range cases {
		br := bufio.NewReader(strings.NewReader(c.in))
		found, n, _ := skipMarker(br, maxBanner)
		rest, _ := br.ReadString(0xff)
		if found != c.found || n != c.discard || (found && rest != c.rest) {
			t.Errorf("%q: found %v discarded %d rest %q", c.in, found, n, rest)
		}
	}
	// One byte over the limit fails; exactly the limit passes.
	ok := bytes.Repeat([]byte("x"), maxBanner)
	if found, _, err := skipMarker(bufio.NewReader(bytes.NewReader(append(ok, marker...))), maxBanner); !found || err != nil {
		t.Errorf("a banner of exactly the limit failed: %v", err)
	}
	over := append(bytes.Repeat([]byte("x"), maxBanner+1), marker...)
	if found, _, err := skipMarker(bufio.NewReader(bytes.NewReader(over)), maxBanner); found || !errors.Is(err, errOverflow) {
		t.Errorf("a banner one byte over the limit: found %v, err %v", found, err)
	}
}

func TestClassifySSH(t *testing.T) {
	cases := map[string]Code{
		"Host key verification failed.":                                           CodeSSHHostKey,
		"No ED25519 host key is known for 10.0.0.2 and you have requested strict": CodeSSHFailed,
		"@@@ WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED! @@@":                CodeSSHHostKey,
		"test@10.0.0.2: Permission denied (publickey).":                           CodeSSHAuth,
		"sign_and_send_pubkey: signing failed for ED25519 \"/k.pub\" from agent: communication with agent failed\ntest@10.0.0.2: Permission denied (publickey).": CodeSSHAgent,
		"sign_and_send_pubkey: signing failed for ED25519 \"/k.pub\" from agent: agent refused operation\ntest@10.0.0.2: Permission denied (publickey).":         CodeSSHAgent,
		"ssh: connect to host 10.0.0.2 port 22: Connection timed out":                                                                                            CodeSSHTimeout,
		"Timeout, server 10.0.0.2 not responding.":                        CodeSSHTimeout,
		"/tmp/cfg: line 3: Bad configuration option: foo":                 CodeSSHConfig,
		"ssh: Could not resolve hostname nope: Name or service not known": CodeSSHFailed,
		"ssh: connect to host 10.0.0.2 port 22: Connection refused":       CodeSSHFailed,
	}
	// "No ... host key is known" comes with "Host key verification failed".
	cases["No ED25519 host key is known for 10.0.0.2\nHost key verification failed."] = CodeSSHHostKey
	for stderr, want := range cases {
		if got := classifySSH("h", 255, []byte(stderr)); got.Code != want {
			t.Errorf("%q = %s, want %s", stderr, got.Code, want)
		}
		if strings.Contains(classifySSH("h", 255, []byte(stderr)).Detail, "10.0.0.2") {
			t.Errorf("the error repeats what ssh printed: %q", stderr)
		}
	}
}

func TestNewLayout(t *testing.T) {
	for _, bad := range []string{"~/", "~/.", "~//", "/", "//", "relative", "~"} {
		if _, err := newLayout(bad); err == nil {
			t.Errorf("server_dir %q was accepted", bad)
		} else if e, ok := errors.AsType[*Error](err); !ok || e.Code != CodeServerDirUnsafe {
			t.Errorf("server_dir %q: %v", bad, err)
		}
	}
	for in, want := range map[string]layout{
		"":                 {"h", ".gitbox-server"},
		"~/srv/gitbox":     {"h", "srv/gitbox"},
		"/opt/me/gitbox":   {"a", "/opt/me/gitbox"},
		"~/.gitbox-server": {"h", ".gitbox-server"},
	} {
		got, err := newLayout(in)
		if err != nil || got != want {
			t.Errorf("newLayout(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
}

func TestBuildName(t *testing.T) {
	pin := strings.Repeat("ab", 32)
	commit := strings.Repeat("c", 40)
	name := BuildName("v2.1.0-3-gabc-dirty-dev", commit, pin)
	if name != "v2.1.0-3-gabc-dirty-dev-"+commit+"-abababab" || !BuildNameRE.MatchString(name) {
		t.Errorf("BuildName = %q", name)
	}
	if BuildNameRE.MatchString(BuildName("dev", "none", pin)) {
		t.Error("an unstamped build has a valid build name")
	}
	if _, err := buildFor(Options{Version: "dev", Commit: "none"}, Target{SHA256: pin}); err == nil {
		t.Error("buildFor accepted an unstamped build")
	}
}

func TestLogRing(t *testing.T) {
	var seen int
	l := NewLog(func(string) { seen++ })
	for i := range LogLines + 5 {
		l.Printf("line %d\x1b[31m", i)
	}
	lines := l.Lines()
	if len(lines) != LogLines || seen != LogLines+5 {
		t.Fatalf("kept %d lines, saw %d", len(lines), seen)
	}
	if !strings.HasSuffix(lines[0], "line 5[31m") || !strings.HasSuffix(lines[len(lines)-1], fmt.Sprintf("line %d[31m", LogLines+4)) {
		t.Errorf("ring order wrong: %q … %q", lines[0], lines[len(lines)-1])
	}
	_, _ = l.Write([]byte("partial "))
	_, _ = l.Write([]byte("done\nnext\n"))
	tail := l.Lines()[LogLines-2:]
	if !strings.HasSuffix(tail[0], "partial done") || !strings.HasSuffix(tail[1], "next") {
		t.Errorf("Write split lines wrong: %q", tail)
	}
	long := strings.Repeat("é", maxLogLine)
	l.Printf("%s", long)
	if last := l.Lines()[LogLines-1]; len(last) > maxLogLine+40 {
		t.Errorf("a long line was not cut: %d bytes", len(last))
	}
	var nilLog *Log
	nilLog.Printf("ignored")
}
