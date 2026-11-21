// Package usererr holds the one error shape that may leave the process: to
// the window, to an event, to the config file or, in remote mode, over the
// RPC wire. It has no dependencies so that any package can carry it.
package usererr

// UserError is an error that is safe to show. Whoever builds one guarantees
// that no field holds a secret, a raw URL, command arguments or the output
// of an external program.
type UserError struct {
	// Code is a stable machine-readable class, such as "git.auth_failed".
	// Empty for an error that has no class.
	Code string `json:"code,omitempty"`
	// Hint is a catalog key for a translated explanation, not prose.
	Hint string `json:"hint,omitempty"`
	// Detail is a short English description built from allowlisted parts.
	Detail string `json:"detail,omitempty"`
}

// String returns the one-line form shown where only text fits.
func (e UserError) String() string {
	if e.Detail != "" {
		return e.Detail
	}
	return e.Code
}
