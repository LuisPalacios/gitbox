package doctor

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"
)

const fakeWSLSentinelEnv = "GITBOX_TEST_FAKE_WSL_SENTINEL"

// TestMain turns the test binary into a fake wsl.exe when a test runs a copy of
// it, so the tests can tell whether a probe executed the file.
func TestMain(m *testing.M) {
	if sentinel := os.Getenv(fakeWSLSentinelEnv); sentinel != "" {
		_ = os.WriteFile(sentinel, nil, 0o644)
		fmt.Println("WSL version: 9.9.9")
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// installFakeWSL copies the test binary to dir\wsl.exe and returns the sentinel
// file that appears if anything runs it.
func installFakeWSL(t *testing.T, dir string) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.Open(self)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = src.Close() }()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	dst, err := os.Create(filepath.Join(dir, "wsl.exe"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		t.Fatal(err)
	}
	if err := dst.Close(); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(t.TempDir(), "wsl-was-run")
	t.Setenv(fakeWSLSentinelEnv, sentinel)
	return sentinel
}

// useFakeLxss points lxssKeyPath at a temporary HKCU key holding the given
// subkeys and removes it afterwards.
func useFakeLxss(t *testing.T, subkeys map[string]map[string]any) {
	t.Helper()
	parent := `Software\GitboxTest`
	base := fmt.Sprintf(`%s\%d-%s`, parent, os.Getpid(), strings.ReplaceAll(t.Name(), "/", "-"))
	createKey := func(path string) registry.Key {
		k, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.ALL_ACCESS)
		if err != nil {
			t.Fatal(err)
		}
		return k
	}
	_ = createKey(parent).Close()
	_ = createKey(base).Close()
	for id, values := range subkeys {
		k := createKey(base + `\` + id)
		for name, v := range values {
			var err error
			switch val := v.(type) {
			case string:
				err = k.SetStringValue(name, val)
			case int:
				err = k.SetDWordValue(name, uint32(val))
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		_ = k.Close()
	}
	previous := lxssKeyPath
	lxssKeyPath = base
	t.Cleanup(func() {
		lxssKeyPath = previous
		for id := range subkeys {
			_ = registry.DeleteKey(registry.CURRENT_USER, base+`\`+id)
		}
		_ = registry.DeleteKey(registry.CURRENT_USER, base)
		_ = registry.DeleteKey(registry.CURRENT_USER, parent) // fails while a sibling exists
	})
}

func TestWSLDistros_ReadsInstalledDistrosFromRegistry(t *testing.T) {
	useFakeLxss(t, map[string]map[string]any{
		"{b}":           {"DistributionName": "Ubuntu-26.04", "State": wslStateInstalled},
		"{a}":           {"DistributionName": "Debian", "State": wslStateInstalled},
		"{installing}":  {"DistributionName": "Half-Done", "State": 3},
		"{no-state}":    {"DistributionName": "Alpine"},
		"Notifications": {},
	})
	want := []string{"Alpine", "Debian", "Ubuntu-26.04"}
	if got := WSLDistros(); !reflect.DeepEqual(got, want) {
		t.Errorf("WSLDistros() = %v, want %v", got, want)
	}
}

func TestWSLDistros_MissingKeyIsNotAnError(t *testing.T) {
	previous := lxssKeyPath
	lxssKeyPath = `Software\GitboxTest\does-not-exist`
	t.Cleanup(func() { lxssKeyPath = previous })
	if got := WSLDistros(); got != nil {
		t.Errorf("WSLDistros() = %v, want nil", got)
	}
}

// The freshly installed PC: System32\wsl.exe exists, the WSL package does not.
// Running the stub is what opens the install prompt, so the probe must not.
func TestCheckWSL_InstallStubIsNeverRun(t *testing.T) {
	stubDir := t.TempDir()
	sentinel := installFakeWSL(t, stubDir)
	t.Setenv("PATH", stubDir)
	t.Setenv("ProgramFiles", t.TempDir())
	useFakeLxss(t, nil)

	r := CheckOne(toolWSL())

	if r.Found {
		t.Errorf("stub without package reported as installed: %+v", r)
	}
	if _, err := os.Stat(sentinel); err == nil {
		t.Error("the probe executed wsl.exe")
	}
}

func TestCheckWSL_RegisteredDistroWithoutPackageIsFoundButNotRun(t *testing.T) {
	stubDir := t.TempDir()
	sentinel := installFakeWSL(t, stubDir)
	t.Setenv("PATH", stubDir)
	t.Setenv("ProgramFiles", t.TempDir())
	useFakeLxss(t, map[string]map[string]any{
		"{a}": {"DistributionName": "Ubuntu", "State": wslStateInstalled},
	})

	r := CheckOne(toolWSL())

	if !r.Found || !strings.EqualFold(r.Path, filepath.Join(stubDir, "wsl.exe")) || r.Version != "" {
		t.Errorf("unexpected result: %+v", r)
	}
	if _, err := os.Stat(sentinel); err == nil {
		t.Error("the probe executed wsl.exe")
	}
}

func TestCheckWSL_PackageBinaryReportsVersion(t *testing.T) {
	programFiles := t.TempDir()
	sentinel := installFakeWSL(t, filepath.Join(programFiles, "WSL"))
	t.Setenv("ProgramFiles", programFiles)
	useFakeLxss(t, nil)

	r := CheckOne(toolWSL())

	if !r.Found || r.Path != filepath.Join(programFiles, "WSL", "wsl.exe") {
		t.Errorf("unexpected result: %+v", r)
	}
	if r.Version != "WSL version: 9.9.9" {
		t.Errorf("Version = %q", r.Version)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Errorf("control: the package binary should have run: %v", err)
	}
}
