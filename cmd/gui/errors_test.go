package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/provider"
)

func TestToUserErrorScrubsSecrets(t *testing.T) {
	const secret = "s3cr3tValue0123456789abcdef"
	tests := []struct {
		name, in string
		gone     []string // must not survive
		want     string   // exact result; empty skips the check
	}{
		{"authorization header with scheme", "request failed: Authorization: token abc", []string{"abc"}, "request failed: Authorization: [redacted]"},
		{"authorization header bare", "header authorization=" + secret + " rejected", []string{secret}, ""},
		{"bearer value", "sent Bearer " + secret + " to host", []string{secret}, "sent Bearer [redacted] to host"},
		{"token value", "git said: token " + secret, []string{secret}, ""},
		{"url with password", "cloning https://alice:" + secret + "@example.com/o/r.git failed", []string{secret, "alice"}, "cloning https://example.com/o/r.git failed"},
		{"url with user only", "remote https://" + secret + "@example.com/o/r.git", []string{secret}, "remote https://example.com/o/r.git"},
		{"credentials outside a url", "login alice:" + secret + "@example.com refused", []string{secret}, ""},
		{"github token", "bad credentials for ghp_" + strings.Repeat("A1", 18), []string{"ghp_"}, "bad credentials for [redacted]"},
		{"gitlab token", "glpat-" + strings.Repeat("x9", 12) + " expired", []string{"glpat-"}, "[redacted] expired"},
		{"control characters", "line one\nline two\x00\x1b[31m\ttabbed\x7f", []string{"\n", "\x00", "\x1b", "\t", "\x7f"}, "line one line two[31m tabbed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, err := range []error{errors.New(tt.in), fmt.Errorf("github list repos: %w", errors.New(tt.in))} {
				ue := toUserError(err)
				for _, text := range []string{ue.Detail, ue.Code, ue.Hint, ue.String(), userMsg(err)} {
					for _, gone := range tt.gone {
						if strings.Contains(text, gone) {
							t.Errorf("%q survived in %q", gone, text)
						}
					}
				}
			}
			if got := userMsg(errors.New(tt.in)); tt.want != "" && got != tt.want {
				t.Errorf("userMsg = %q, want %q", got, tt.want)
			}
		})
	}
}

// Plain prose that mentions tokens is not a credential and must read as
// written: the frontend even matches on one of these.
func TestToUserErrorKeepsProse(t *testing.T) {
	for _, in := range []string{
		"missing API token in github-MyAccount",
		"authentication failed (HTTP 401) — token is invalid or expired",
		"insufficient permissions (HTTP 403) — token needs broader scopes",
		"git fetch: offline (exit 128)",
		"github get repo: HTTP 500 from api.example.com (GET)",
		`repo "org/name" not found in source "github-MyAccount"`,
		"WARNING: API rejected the GCM credential (HTTP 401).",
		"saving config: open /home/me/.config/gitbox/gitbox.json: permission denied",
	} {
		if got := userText(in); got != in {
			t.Errorf("userText changed prose:\n got %q\nwant %q", got, in)
		}
	}
}

func TestToUserErrorCap(t *testing.T) {
	long := userText(strings.Repeat("ñ", 1000))
	if n := utf8.RuneCountInString(long); n != maxUserErrorRunes {
		t.Errorf("capped text has %d runes, want %d", n, maxUserErrorRunes)
	}
	if !utf8.ValidString(long) || !strings.HasSuffix(long, "…") {
		t.Errorf("the cap must cut on a rune boundary and mark the cut: %q", long[len(long)-8:])
	}
	if got := toUserError(nil); got.Code != "" || got.Detail != "" || userMsg(nil) != "" {
		t.Errorf("a nil error = %+v", got)
	}
}

func TestToUserErrorCodes(t *testing.T) {
	gitErr := &git.GitError{Op: "fetch", Code: git.CodeAuthFailed, Hint: "error.git.auth_failed", Detail: "exit 128"}
	ue := toUserError(fmt.Errorf("refresh: %w", gitErr))
	if ue.Code != "git.auth_failed" || ue.Hint != "error.git.auth_failed" || ue.Detail != "refresh: git fetch: auth_failed (exit 128)" {
		t.Errorf("git error = %+v", ue)
	}
	provErr := &provider.ProviderError{Status: 404, Class: provider.ClassNotFound, Detail: "HTTP 404 from api.example.com (GET)"}
	ue = toUserError(fmt.Errorf("github get repo: %w", provErr))
	if ue.Code != "provider.not_found" || ue.Hint != "error.provider.not_found" || ue.Detail != "github get repo: HTTP 404 from api.example.com (GET)" {
		t.Errorf("provider error = %+v", ue)
	}
	if ue := toUserError(errors.New("plain")); ue.Code != "" || ue.Hint != "" || ue.String() != "plain" {
		t.Errorf("untyped error = %+v", ue)
	}
}

// TestDescribeFetchFailure runs a real fetch against a remote that refuses
// the connection and whose URL carries a token: the payload names the code
// and the host as data, flags an instant refusal, and no value holds the
// token.
func TestDescribeFetchFailure(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	const token = "CANARY-TOKEN-165"
	repo := t.TempDir()
	for _, args := range [][]string{{"init", "--quiet"}, {"remote", "add", "origin", "https://user:" + token + "@127.0.0.1:1/owner/x.git"}} {
		cmd := exec.Command(git.GitBin(), args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v: %s", args[0], err, out)
		}
	}
	_, err := git.FetchCapturedContext(t.Context(), repo, "origin")
	if err == nil {
		t.Fatal("the fetch must fail")
	}

	payload := fetchEventDTO{Source: "s", Repo: "r"}
	describeFetchFailure(&payload, fmt.Errorf("refresh: %w", err))
	if payload.Code != "git.offline" || payload.Host != "127.0.0.1" {
		t.Errorf("payload = %v, want code git.offline and host 127.0.0.1", payload)
	}
	// How fast the OS refuses a loopback connect depends on the platform
	// (Windows retries for about two seconds), so the flag only has to
	// agree with the classifier.
	if flagged := payload.RefusedFast; flagged != git.IsRefusedFast(err) {
		t.Errorf("refusedFast in payload = %v, IsRefusedFast = %v", flagged, git.IsRefusedFast(err))
	}
	if payload.Error != "refresh: git fetch: offline (exit 128)" {
		t.Errorf("error text = %q", payload.Error)
	}
	encoded := asJSON(t, payload)
	if strings.Contains(encoded, token) || strings.Contains(encoded, "user:") || strings.Contains(encoded, "x.git") {
		t.Errorf("payload leaks the remote URL: %s", encoded)
	}

	var plain fetchEventDTO
	describeFetchFailure(&plain, errors.New("repository maintenance: x"))
	if plain != (fetchEventDTO{Error: "repository maintenance: x"}) {
		t.Errorf("an untyped failure = %v, want only its text", plain)
	}
}

// TestHintKeysInCatalog checks that every hint the boundary can return has
// an entry in both the English and the Spanish catalog.
func TestHintKeysInCatalog(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("frontend", "src", "lib", "i18n.ts"))
	if err != nil {
		t.Fatal(err)
	}
	catalog := string(data)
	var keys []string
	for _, c := range []git.GitCode{git.CodeAuthFailed, git.CodeNotFastForward, git.CodeNoUpstream, git.CodeOffline,
		git.CodeTimeout, git.CodeCancelled, git.CodeNotARepo, git.CodeLockHeld, git.CodeRepoNotFound, git.CodeUnsafeConfig, git.CodeUnknown} {
		keys = append(keys, toUserError(&git.GitError{Code: c, Hint: "error.git." + string(c)}).Hint)
	}
	for _, c := range []provider.ProviderClass{provider.ClassUnauthorized, provider.ClassScopeDenied, provider.ClassForbidden, provider.ClassRateLimited,
		provider.ClassNotFound, provider.ClassServerError, provider.ClassOffline, provider.ClassTimeout, provider.ClassUnknown} {
		keys = append(keys, toUserError(&provider.ProviderError{Class: c}).Hint)
	}
	for _, c := range []remoteCode{remoteUnstampedBuild, remoteServerTargetMissing, remoteServerBuildMismatch,
		remoteNotConnected, remoteNotImplemented, remoteServerCrashed, remoteNotEnabled, remoteUnknownAlias, remoteReadOnly, remoteWindowClosed} {
		keys = append(keys, toUserError(&remoteError{Code: c}).Hint)
	}
	for _, c := range []externalCode{externalNeedsConfirmation, externalRejected} {
		keys = append(keys, toUserError(&externalError{Code: c}).Hint)
	}
	for _, key := range keys {
		if n := strings.Count(catalog, "'"+key+"':"); n != 2 {
			t.Errorf("hint %q has %d catalog entries, want one in en and one in es", key, n)
		}
	}
}

// TestErrorsLeaveThroughTheBoundary fails when cmd/gui turns an error into
// text on its own. An Error or Message field and an "error" payload take
// userMsg(err) or userText(…), never err.Error(), so a new binding cannot
// skip the scrub by accident.
func TestErrorsLeaveThroughTheBoundary(t *testing.T) {
	// The only places that may call .Error(): the boundary itself, a startup
	// failure printed to the terminal, the load error of the local
	// preferences file (never sent to the window) and a fixed sentinel.
	allowed := map[string]string{
		"errors.go":  "out := usererr.UserError{Detail: userText(err.Error())}",
		"main.go":    `println("Error:", err.Error())`,
		"prefs.go":   "f.loadErr = err.Error()",
		"refresh.go": `return CredentialStatus{Status: "offline", Message: context.Canceled.Error()}`,
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			code, _, _ := strings.Cut(line, "//")
			if !strings.Contains(code, ".Error()") || strings.Contains(code, ") Error() string") {
				continue
			}
			seen++
			if strings.TrimSpace(line) != allowed[file] {
				t.Errorf("%s:%d calls .Error() directly; use userMsg(err) or userText(…):\n\t%s", file, i+1, strings.TrimSpace(line))
			}
		}
	}
	if seen != len(allowed) {
		t.Errorf("found %d allowed .Error() calls, want %d: the allowlist is stale", seen, len(allowed))
	}
}

// A remote window's bound methods fail with the scrubbed text, the code and
// the catalog hint (#240), so a disconnection reads in the user's language
// instead of the backend's English, and nothing secret crosses.
func TestWindowErrorCarriesCodeAndHint(t *testing.T) {
	decode := func(v any) WindowErrorDTO {
		t.Helper()
		text, ok := v.(string)
		if !ok {
			t.Fatalf("windowError returned %T, want the JSON text the Wails runtime keeps", v)
		}
		var dto WindowErrorDTO
		if err := json.Unmarshal([]byte(text), &dto); err != nil {
			t.Fatalf("windowError returned %q: %v", text, err)
		}
		return dto
	}
	got := decode(windowError(&remoteError{remoteNotConnected, "not connected to the remote"}))
	if got.Code != "remote.not_connected" || got.Hint != "error.remote.not_connected" || got.Message != "not connected to the remote" {
		t.Errorf("not_connected = %+v", got)
	}
	leak := decode(windowError(fmt.Errorf("push https://bob:ghp_CANARYCANARYCANARYCANARY0123456789@host/x.git failed")))
	if strings.Contains(leak.Message, "ghp_") || strings.Contains(leak.Message, "bob:") || leak.Code != "" || leak.Hint != "" {
		t.Errorf("an untyped error crossed as %+v", leak)
	}
}

func TestProblemCarriesTypedDiagnosticAndRetry(t *testing.T) {
	before := time.Now()
	rateLimited := problem(&provider.ProviderError{
		Class: provider.ClassRateLimited, Detail: "HTTP 429", RetryAfter: 3 * time.Second,
	})
	if rateLimited.Code != "provider.rate_limited" || rateLimited.Detail != "HTTP 429" ||
		rateLimited.RetryAt < before.Add(3*time.Second).UnixMilli() ||
		rateLimited.RetryAt > time.Now().Add(3*time.Second).UnixMilli() {
		t.Fatalf("rate limit = %+v", rateLimited)
	}
	if got := problem(errServerUnreachable); got.Code != "provider.offline" || got.RetryAt != 0 {
		t.Fatalf("unreachable = %+v", got)
	}
	if got := problem(errors.New("offline 401")); got.Code != "" {
		t.Fatalf("guessed a code from an unknown error: %+v", got)
	}
}
