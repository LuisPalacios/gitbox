package main

import (
	"errors"
	"regexp"
	"strings"

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
	} else if re, ok := errors.AsType[*remoteError](err); ok {
		out.Code, out.Hint = "remote."+string(re.Code), "error.remote."+string(re.Code)
	} else if re, ok := errors.AsType[*remote.Error](err); ok {
		out.Code, out.Hint = "remote."+string(re.Code), "error.remote."+string(re.Code)
	}
	return out
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
