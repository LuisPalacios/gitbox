package main

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/provider"
	"github.com/LuisPalacios/gitbox/pkg/remote"
	"github.com/LuisPalacios/gitbox/pkg/rpc"
	"github.com/LuisPalacios/gitbox/pkg/usererr"
)

// maxUserErrorRunes caps the text of an error that leaves the process.
const maxUserErrorRunes = 300

// toUserError turns any error into the one shape that may leave the process:
// to the window, to an event or to the config file. Every Error and Message
// field of a DTO and every "error" event payload goes through it (or through
// userMsg and userText, its string forms).
//
// The errors of pkg/git and pkg/provider are already built from allowlisted
// parts and supply the code and the hint. The text of anything else, and of
// whatever wraps them, is scrubbed as a second line of defence: credentials
// in URLs, authorization headers, bearer values and known token formats are
// removed, control characters are dropped and the result is capped.
func toUserError(err error) usererr.UserError {
	if err == nil {
		return usererr.UserError{}
	}
	out := usererr.UserError{Detail: userText(err.Error())}
	if re, ok := errors.AsType[*rpc.Error](err); ok {
		// An error that came from a gitbox-server is already a UserError,
		// but the server is untrusted: its words get the same scrub, and
		// its code and hint must look like the identifiers they are.
		out.Code, out.Hint = identifier(re.Data.Code), identifier(re.Data.Hint)
		return out
	}
	if ge, ok := errors.AsType[*git.GitError](err); ok {
		out.Code, out.Hint = "git."+string(ge.Code), ge.Hint
	} else if pe, ok := errors.AsType[*provider.ProviderError](err); ok {
		out.Code, out.Hint = "provider."+string(pe.Class), "error.provider."+string(pe.Class)
	} else if ee, ok := errors.AsType[*externalError](err); ok {
		out.Code, out.Hint = "url."+string(ee.Code), "error.url."+string(ee.Code)
	} else if _, ok := errors.AsType[*config.ChangedError](err); ok {
		out.Code, out.Hint = "config.changed", "error.config.changed"
	} else if errors.Is(err, errCloneTransferActive) {
		out.Code, out.Hint = "repo.busy", "error.repo.busy"
	} else if errors.Is(err, config.ErrPathCollision) {
		out.Code, out.Hint = "config.path_collision", "error.config.path_collision"
	} else if errors.Is(err, config.ErrPathUnavailable) {
		out.Code, out.Hint = "config.path_unavailable", "error.config.path_unavailable"
	} else if errors.Is(err, config.ErrExists) {
		out.Code, out.Hint = "config.exists", "error.config.exists"
	} else if errors.Is(err, config.ErrNotFound) {
		out.Code, out.Hint = "config.not_found", "error.config.not_found"
	} else if ge, ok := errors.AsType[*gcmRemoteError](err); ok {
		out.Code, out.Hint = "gcm_unsupported_remote", ge.hint()
	} else if re, ok := errors.AsType[*remoteError](err); ok {
		out.Code, out.Hint = "remote."+string(re.Code), "error.remote."+string(re.Code)
	} else if re, ok := errors.AsType[*remote.Error](err); ok {
		out.Code, out.Hint = "remote."+string(re.Code), "error.remote."+string(re.Code)
	}
	return out
}

// ProblemDTO preserves an observed cause and the earliest allowed retry.
type ProblemDTO struct {
	usererr.UserError
	RetryAt int64 `json:"retryAt,omitempty"`
}

func (p *ProblemDTO) UnmarshalJSON(data []byte) error {
	type payload ProblemDTO
	var wire payload
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*p = ProblemDTO(wire)
	p.Code, p.Hint = identifier(p.Code), identifier(p.Hint)
	p.Detail = userText(p.Detail)
	return nil
}

func problem(err error) *ProblemDTO {
	if err == nil {
		return nil
	}
	result := &ProblemDTO{UserError: toUserError(err)}
	if errors.Is(err, errServerUnreachable) {
		result.Code = "provider.offline"
	}
	if pe, ok := errors.AsType[*provider.ProviderError](err); ok && pe.RetryAfter > 0 {
		result.RetryAt = time.Now().Add(pe.RetryAfter).UnixMilli()
	}
	return result
}

// UnsafeConfigDTO names the setting behind a git.unsafe_config failure and
// where the clone stores it, so the window can explain how to review it.
// Both fields come from fixed lists; the setting's value never leaves the
// process.
type UnsafeConfigDTO struct {
	Setting string `json:"setting"`
	Scope   string `json:"scope"`
}

// UnmarshalJSON keeps a finding from an untrusted server only when both
// fields are names this build knows.
func (u *UnsafeConfigDTO) UnmarshalJSON(data []byte) error {
	type payload UnsafeConfigDTO
	var wire payload
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*u = UnsafeConfigDTO{}
	if knownTrustFinding(git.TrustFinding{Key: wire.Setting, Scope: wire.Scope}) {
		*u = UnsafeConfigDTO(wire)
	}
	return nil
}

func knownTrustFinding(f git.TrustFinding) bool {
	switch f.Key {
	case git.TrustFSMonitor, git.TrustHooksPath, git.TrustInsteadOf:
	default:
		return false
	}
	switch f.Scope {
	case git.ScopeLocal, git.ScopeWorktree, git.ScopeInclude:
		return true
	}
	return false
}

// keptUnsafeConfig drops a server's finding that UnmarshalJSON emptied.
func keptUnsafeConfig(u *UnsafeConfigDTO) *UnsafeConfigDTO {
	if u == nil || u.Setting == "" {
		return nil
	}
	return u
}

// unsafeConfigOf is the finding beneath err, or nil.
func unsafeConfigOf(err error) *UnsafeConfigDTO {
	f, ok := errors.AsType[git.TrustFinding](err)
	if !ok || !knownTrustFinding(f) {
		return nil
	}
	return &UnsafeConfigDTO{Setting: f.Key, Scope: f.Scope}
}

// gcmRemoteError refuses Git Credential Manager on a host (#242). OS is
// the host's, so the reason names the store GCM would use there.
type gcmRemoteError struct{ OS string }

// gcmStores names, per OS, the store GCM keeps credentials in.
var gcmStores = map[string]string{
	"linux":   "the Secret Service keyring (secretservice)",
	"darwin":  "the macOS keychain",
	"windows": "Windows Credential Manager (wincredman)",
}

func (e *gcmRemoteError) Error() string {
	store, ok := gcmStores[e.OS]
	if !ok {
		store = "the system's credential store"
	}
	return "Git Credential Manager is not available for this host: it would keep credentials in " + store +
		", which a session over ssh cannot use reliably. Use a token instead."
}

// hint is the catalog key of the reason for e's OS.
func (e *gcmRemoteError) hint() string {
	if _, ok := gcmStores[e.OS]; ok {
		return "error.gcm_unsupported_remote." + e.OS
	}
	return "error.gcm_unsupported_remote"
}

// identifier keeps s only if it is a short dotted name such as "git.offline"
// or "error.git.offline": letters, digits, '.', '_' and '-'.
func identifier(s string) string {
	if len(s) == 0 || len(s) > 64 {
		return ""
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '.' && r != '_' && r != '-' {
			return ""
		}
	}
	return s
}

// WindowErrorDTO is how a remote window's bound methods fail (#240): the
// scrubbed text with its code and catalog hint, so the frontend shows the
// translated hint instead of the backend's English.
type WindowErrorDTO struct {
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
	Hint    string `json:"hint,omitempty"`
	Detail  string `json:"detail,omitempty"`
	RetryAt int64  `json:"retryAt,omitempty"`
}

// windowError is the Wails ErrorFormatter of a remote window. The Wails
// runtime turns whatever it returns into the message of a JavaScript Error,
// so it returns the DTO as JSON text, which the bridge decodes
// (readonly.ts, windowError).
func windowError(err error) any {
	ue := problem(err)
	if ue == nil {
		return ""
	}
	data, jerr := json.Marshal(WindowErrorDTO{Message: ue.String(), Code: ue.Code, Hint: ue.Hint, Detail: ue.Detail, RetryAt: ue.RetryAt})
	if jerr != nil {
		return ue.String()
	}
	return string(data)
}

// userMsg is the text of err that a string field or payload may carry.
func userMsg(err error) string { return toUserError(err).String() }

const redacted = "[redacted]"

// scrubbers remove what may be a credential from free text. They only match
// values that look like secrets, so plain prose such as "token is invalid"
// passes through unchanged.
var scrubbers = []struct {
	re   *regexp.Regexp
	with string
}{
	// user:password@ and user@ in a URL: https://user:secret@host/…
	{regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://)[^/\s@]+@`), "${1}"},
	// user:password@host outside a URL
	{regexp.MustCompile(`[^\s/@:"']+:[^\s/@"']+@`), ""},
	// Authorization: <scheme> <value>, and Authorization=<value>
	{regexp.MustCompile(`(?i)(authorization\s*[:=]\s*)\S+(\s+\S+)?`), "${1}" + redacted},
	// Bearer <value>, token <value>, Basic <value>, when the value is long
	// enough to be a credential
	{regexp.MustCompile(`(?i)\b(bearer|token|basic)(\s+)[A-Za-z0-9._~+/=-]{20,}`), "${1}${2}" + redacted},
	// token formats of the supported providers
	{regexp.MustCompile(`\b(gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|glpat-[A-Za-z0-9_-]{20,})`), redacted},
}

// userText scrubs a string that is about to leave the process: see
// toUserError.
func userText(s string) string {
	for _, sc := range scrubbers {
		s = sc.re.ReplaceAllString(s, sc.with)
	}
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t' || r == '\r':
			return ' '
		case r < 0x20 || r == 0x7f:
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if runes := []rune(s); len(runes) > maxUserErrorRunes {
		s = string(runes[:maxUserErrorRunes-1]) + "…"
	}
	return s
}
