package credential

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/git"
)

func credentialRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(root, "global"))
	t.Setenv("GIT_TOKEN", "")
	cmd := exec.Command(git.GitBin(), "init", root)
	cmd.Env = git.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("init: %v: %s", err, out)
	}
	return root
}

func credentialAccount() config.Account {
	return config.Account{URL: "https://example.org", Username: "account-a", Provider: "github", GCM: &config.GCMConfig{Provider: "github"}}
}

func TestConfigureRepoCredential_UnchangedAndTypeTransitions(t *testing.T) {
	root := credentialRepo(t)
	t.Setenv(EnvVarName("account-a"), "token-a")
	acct := credentialAccount()
	global := config.GlobalConfig{}
	if err := os.WriteFile(os.Getenv("GIT_CONFIG_GLOBAL"), []byte("[credential]\n\thelper = unrelated-global-account\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, typ := range []string{"gcm", "token", "ssh", "gcm"} {
		t.Run(typ, func(t *testing.T) {
			if err := ConfigureRepoCredential(root, acct, "account-a", typ, global); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, ".git", "config")
			old := time.Unix(1_000_000_000, 0)
			if err := os.Chtimes(path, old, old); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := ConfigureRepoCredential(root, acct, "account-a", typ, global); err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) || !info.ModTime().Equal(old) {
				t.Fatal("unchanged credential configuration was rewritten")
			}
			snapshot, err := git.ReadConfigSnapshot(root)
			if err != nil {
				t.Fatal(err)
			}
			want := []string{""}
			switch typ {
			case "token":
				want = append(want, "store --file "+filepath.ToSlash(CredentialFilePath("account-a")))
			case "gcm":
				want = append(want, "manager")
			}
			if !reflect.DeepEqual(snapshot.All("credential.helper"), append([]string{"unrelated-global-account"}, want...)) {
				t.Fatalf("global helper not canceled: %v", snapshot.All("credential.helper"))
			}
			if typ != "gcm" && (len(snapshot.LocalAll("credential.https://example.org.username")) != 0 || len(snapshot.LocalAll("credential.https://example.org.provider")) != 0 || len(snapshot.LocalAll("credential.https://example.org.useHttpPath")) != 0) {
				t.Fatal("type transition retained GCM host configuration")
			}
		})
	}
}

func TestConfigureRepoCredential_UnknownTypeDoesNotWrite(t *testing.T) {
	root := credentialRepo(t)
	path := filepath.Join(root, ".git", "config")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := ConfigureRepoCredential(root, credentialAccount(), "account-a", "unknown", config.GlobalConfig{}); err == nil {
		t.Fatal("unknown type accepted")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("unknown type changed repository configuration")
	}
}

func TestWriteCredentialFile_UnchangedRotationAndConcurrentReaders(t *testing.T) {
	credentialRepo(t)
	t.Setenv(EnvVarName("account-a"), "token-a")
	acct, path := credentialAccount(), CredentialFilePath("account-a")
	if err := WriteCredentialFile(path, acct, "account-a"); err != nil {
		t.Fatal(err)
	}
	old := time.Unix(1_000_000_000, 0)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			if err := WriteCredentialFile(path, acct, "account-a"); err != nil {
				t.Error(err)
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != "https://account-a:token-a@example.org\n" {
				t.Errorf("partial credential read: %q, %v", data, err)
			}
		})
	}
	wg.Wait()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(old) {
		t.Fatal("unchanged credential file was rewritten")
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := WriteCredentialFile(path, acct, "account-a"); err != nil {
			t.Fatal(err)
		}
		info, err = os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatal("credential permissions not repaired")
		}
	}
	t.Setenv(EnvVarName("account-a"), "rotated-token")
	if err := WriteCredentialFile(path, acct, "account-a"); err != nil {
		t.Fatal(err)
	}
	got, err := GetToken("account-a")
	if err != nil || got != "rotated-token" {
		t.Fatalf("rotation failed: %q, %v", got, err)
	}
	t.Setenv(EnvVarName("account-b"), "token-b")
	if err := WriteCredentialFile(CredentialFilePath("account-b"), acct, "account-b"); err != nil {
		t.Fatal(err)
	}
	if got, err := GetToken("account-a"); err != nil || got != "rotated-token" {
		t.Fatal("account B overwrote account A")
	}
}

func TestConfigureRepoCredential_RejectsShadowedHelper(t *testing.T) {
	root := credentialRepo(t)
	included := filepath.Join(root, "included")
	if err := os.WriteFile(included, []byte("[credential]\n\thelper = unrelated-account\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The credential section survives removing helpers because it also has a
	// store key. Git adds new helpers there, before the later include.
	for _, setting := range [][2]string{{"credential.credentialStore", "keychain"}, {"include.path", included}} {
		if err := git.ConfigSet(root, setting[0], setting[1]); err != nil {
			t.Fatal(err)
		}
	}
	if err := ConfigureRepoCredential(root, credentialAccount(), "account-a", "gcm", config.GlobalConfig{}); err == nil {
		t.Fatal("accepted included helper after account-isolation cancellation")
	}
}

func TestConfigureRepoCredential_RepairsDuplicateHelpersAndHostDrift(t *testing.T) {
	root := credentialRepo(t)
	acct := credentialAccount()
	if err := ConfigureRepoCredential(root, acct, "account-a", "gcm", config.GlobalConfig{}); err != nil {
		t.Fatal(err)
	}
	if err := git.ConfigAdd(root, "credential.helper", "other-account"); err != nil {
		t.Fatal(err)
	}
	if err := git.ConfigSet(root, "credential.https://example.org.username", "other-account"); err != nil {
		t.Fatal(err)
	}
	if err := ConfigureRepoCredential(root, acct, "account-a", "gcm", config.GlobalConfig{}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := git.ReadConfigSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snapshot.All("credential.helper"), []string{"", "manager"}) || snapshot.Get("credential.https://example.org.username") != acct.Username {
		t.Fatalf("drift not repaired: %+v", snapshot)
	}
}
