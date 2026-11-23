package config

import (
	"errors"
	"fmt"
)

// Typed outcomes of the CRUD methods (#294). A remote window can send the
// same change twice across a reconnect; ErrExists and ErrNotFound let the
// app recognise a repeat whose effect is already there, and tell the user,
// without matching on the message.
var (
	// ErrExists: the entry to add, or the new key of a rename, exists.
	ErrExists = errors.New("already exists")
	// ErrNotFound: the entry to change or delete does not exist.
	ErrNotFound = errors.New("not found")
)

// keyError keeps the exact message of a CRUD failure and unwraps to its kind.
type keyError struct {
	msg  string
	kind error
}

func (e *keyError) Error() string { return e.msg }
func (e *keyError) Unwrap() error { return e.kind }

func existsError(format string, a ...any) error {
	return &keyError{msg: fmt.Sprintf(format, a...), kind: ErrExists}
}

func notFoundError(format string, a ...any) error {
	return &keyError{msg: fmt.Sprintf(format, a...), kind: ErrNotFound}
}
