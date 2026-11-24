package remote

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// sshHome writes files under a temporary home's .ssh folder and returns the
// home.
func sshHome(t *testing.T, files map[string]string) string {
	t.Helper()
	home := t.TempDir()
	for name, body := range files {
		path := filepath.Join(home, ".ssh", filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

func TestConfigAliases(t *testing.T) {
	cases := map[string]struct {
		files map[string]string
		want  []string
	}{
		"hosts in file order without duplicates": {
			files: map[string]string{"config": "Host build-box\n  HostName 10.0.0.2\nHost nas lab\nHost build-box\n"},
			want:  []string{"build-box", "nas", "lab"},
		},
		"patterns, negations and invalid names are left out": {
			files: map[string]string{"config": "Host *\nHost *.example.com web?\nHost !bastion jump\nHost -opt ok_1 bad/name\n"},
			want:  []string{"jump", "ok_1"},
		},
		"keyword case, equals sign, quotes, comments and CRLF": {
			files: map[string]string{"config": "# Host commented\r\nHOST=alpha\r\nhost = \"beta\" gamma # trailing\r\n  host\tdelta\r\n"},
			want:  []string{"alpha", "beta", "gamma", "delta"},
		},
		"Include relative to .ssh, with globs, in order": {
			files: map[string]string{
				"config":        "Host first\nInclude conf.d/*.conf\nHost last\n",
				"conf.d/a.conf": "Host from-a\n",
				"conf.d/b.conf": "Host from-b\n",
				"conf.d/c.txt":  "Host not-matched\n",
			},
			want: []string{"first", "from-a", "from-b", "last"},
		},
		"Include with ~/ and an include cycle": {
			files: map[string]string{
				"config":   "Include ~/.ssh/extra\nHost main\n",
				"extra":    "Host extra\nInclude config\nInclude extra\n",
				"unused":   "Host unused\n",
				"conf.d/x": "Host x\n",
			},
			want: []string{"extra", "main"},
		},
		"missing configuration": {
			files: map[string]string{"known_hosts": "x"},
			want:  nil,
		},
		"oversized file contributes nothing": {
			files: map[string]string{
				"config": "Host small\nInclude big\n",
				"big":    "Host big\n" + strings.Repeat("#\n", maxConfigBytes),
			},
			want: []string{"small"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := ConfigAliases(sshHome(t, tc.files))
			if !slices.Equal(got, tc.want) {
				t.Fatalf("ConfigAliases = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestConfigAliasesAbsoluteInclude(t *testing.T) {
	other := filepath.Join(t.TempDir(), "hosts.conf")
	if err := os.WriteFile(other, []byte("Host elsewhere\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	home := sshHome(t, map[string]string{"config": "Include \"" + other + "\"\n"})
	if got := ConfigAliases(home); !slices.Equal(got, []string{"elsewhere"}) {
		t.Fatalf("ConfigAliases = %q", got)
	}
}

func TestConfigAliasesUnreadable(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("file modes do not stop this user from reading")
	}
	home := sshHome(t, map[string]string{"config": "Host open\nInclude locked\n", "locked": "Host locked\n"})
	locked := filepath.Join(home, ".ssh", "locked")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o600) })
	if got := ConfigAliases(home); !slices.Equal(got, []string{"open"}) {
		t.Fatalf("ConfigAliases = %q", got)
	}
	if got := ConfigAliases(""); got != nil {
		t.Fatalf("ConfigAliases(\"\") = %q", got)
	}
}

// The reader never changes the configuration it reads.
func TestConfigAliasesReadOnly(t *testing.T) {
	home := sshHome(t, map[string]string{"config": "Host one\n"})
	path := filepath.Join(home, ".ssh", "config")
	before, _ := os.Stat(path)
	_ = ConfigAliases(home)
	after, _ := os.Stat(path)
	if !before.ModTime().Equal(after.ModTime()) || before.Size() != after.Size() {
		t.Fatal("the ssh configuration changed")
	}
	entries, _ := os.ReadDir(filepath.Join(home, ".ssh"))
	if len(entries) != 1 {
		t.Fatalf("the reader created files: %v", entries)
	}
}
