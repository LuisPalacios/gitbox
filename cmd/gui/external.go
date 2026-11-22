package main

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"unicode"

	"github.com/LuisPalacios/gitbox/pkg/git"
)

// The browser funnel (#240): the only way this app opens a link. A link may
// come from a gitbox-server on another host, from a provider API or from the
// config, so it is untrusted: only https opens at once, http opens after
// the user accepts the exact URL, and everything else is refused. Wails'
// own BrowserOpenURL validates with a denylist, which lets vscode:,
// ms-settings: and the like through, so the frontend never calls it.

// maxExternalURL caps the length of a link the funnel opens.
const maxExternalURL = 2048

// externalCode says why the funnel did not open a link. Each code has an
// error.url.<code> entry in both catalogs of i18n.ts.
type externalCode string

const (
	// externalNeedsConfirmation: an http link, which opens only after the
	// user accepts it.
	externalNeedsConfirmation externalCode = "needs_confirmation"
	// externalRejected: a link the funnel never opens.
	externalRejected externalCode = "rejected"
)

// externalError is a refusal of the funnel. Its text never holds the link.
type externalError struct {
	Code   externalCode
	Detail string
}

func (e *externalError) Error() string { return e.Detail }

// openBrowser hands an accepted link to the system. Tests replace it.
var openBrowser = git.OpenInBrowser

// OpenExternal opens link in the default browser when it is an https URL,
// or an http one that the user confirmed, with a host, no user info, no
// control characters and at most maxExternalURL bytes. An unconfirmed http
// link returns needs_confirmation and opens nothing.
func OpenExternal(link string, confirmed bool) error {
	if err := checkExternal(link, confirmed); err != nil {
		return err
	}
	return openBrowser(link)
}

func checkExternal(link string, confirmed bool) error {
	reject := func(why string) error {
		// The link itself may carry anything; only its scheme is logged.
		scheme, _, _ := strings.Cut(link, ":")
		if scheme == "" || len(scheme) > 16 || strings.ContainsFunc(scheme, func(r rune) bool { return !unicode.IsLetter(r) && r != '-' }) {
			scheme = "unnamed"
		}
		fmt.Fprintf(os.Stderr, "browser: refused a %s link: %s\n", strings.ToLower(scheme), why)
		return &externalError{externalRejected, "the link was not opened: " + why}
	}
	if len(link) > maxExternalURL {
		return reject("it is too long")
	}
	if strings.ContainsFunc(link, func(r rune) bool { return unicode.IsControl(r) || r == ' ' }) {
		return reject("it holds control characters or spaces")
	}
	u, err := url.Parse(link)
	if err != nil {
		return reject("it is not a URL")
	}
	switch {
	case u.Scheme != "https" && u.Scheme != "http":
		return reject("only https links open")
	case u.Opaque != "" || u.Host == "" || u.Hostname() == "":
		return reject("it names no host")
	case u.User != nil:
		return reject("it carries user information")
	case u.Scheme == "http" && !confirmed:
		return &externalError{externalNeedsConfirmation, "an http link opens only after it is confirmed"}
	}
	return nil
}

// ExternalOpenDTO is the outcome of opening a link. When NeedsConfirm is
// set nothing was opened: URL is the http link the user must accept, and
// the caller asks before it calls again with confirmed.
type ExternalOpenDTO struct {
	NeedsConfirm bool   `json:"needsConfirm"`
	URL          string `json:"url,omitempty"`
}

// openExternalDTO runs the funnel for a bound method.
func openExternalDTO(link string, confirmed bool) (ExternalOpenDTO, error) {
	err := OpenExternal(link, confirmed)
	if e, ok := errors.AsType[*externalError](err); ok && e.Code == externalNeedsConfirmation {
		return ExternalOpenDTO{NeedsConfirm: true, URL: link}, nil
	}
	return ExternalOpenDTO{}, err
}
