package rpc

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/LuisPalacios/gitbox/pkg/usererr"
)

// JSON-RPC 2.0 error codes used on the wire.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
	// CodeCancelled answers a request that was cancelled with
	// $/cancelRequest.
	CodeCancelled = -32800
	// CodeApplication carries an error returned by a handler. Its data is
	// the UserError that the server's conversion produced.
	CodeApplication = -32000
)

// UserError codes that the protocol itself produces. Handlers add their own
// through the server's conversion.
const (
	ErrCodeCancelled       = "cancelled"
	ErrCodeInternal        = "internal"
	ErrCodeDisconnected    = "disconnected"
	ErrCodeUnsupported     = "unsupported"
	ErrCodeInvalidParams   = "invalid_params"
	ErrCodeInvalidRequest  = "invalid_request"
	ErrCodeBannerOverflow  = "banner_overflow"
	ErrCodeVersionMismatch = "version_mismatch"
)

// Error is a failed call as the client sees it. Code is the JSON-RPC code
// and Data is the only description of the failure that crossed the wire: a
// UserError, which by construction holds no secret, no raw URL and no output
// of an external program.
type Error struct {
	Code int
	Data usererr.UserError
}

func (e *Error) Error() string {
	if s := e.Data.String(); s != "" {
		return s
	}
	return fmt.Sprintf("rpc error %d", e.Code)
}

// IsCode reports whether err is an *Error whose UserError has the given code.
func IsCode(err error, code string) bool {
	e, ok := errors.AsType[*Error](err)
	return ok && e.Data.Code == code
}

func newError(code int, userCode, detail string) *Error {
	return &Error{Code: code, Data: usererr.UserError{Code: userCode, Detail: detail}}
}

// wireError is the "error" member of a response.
type wireError struct {
	Code    int                `json:"code"`
	Message string             `json:"message"`
	Data    *usererr.UserError `json:"data,omitempty"`
}

func (e *Error) wire() *wireError {
	msg := e.Data.Hint
	if msg == "" {
		msg = e.Data.Code
	}
	data := e.Data
	return &wireError{Code: e.Code, Message: msg, Data: &data}
}

func (w *wireError) asError() *Error {
	e := &Error{Code: w.Code}
	if w.Data != nil {
		e.Data = *w.Data
	} else {
		e.Data = usererr.UserError{Code: w.Message}
	}
	return e
}

// message is one JSON-RPC 2.0 message: a request (Method and ID), a
// notification (Method, no ID) or a response (ID and Result or Error).
type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Meta    *messageMeta    `json:"meta,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *wireError      `json:"error,omitempty"`
}

// messageMeta is the envelope data that is not a parameter of the method.
type messageMeta struct {
	// RequestID identifies one attempt at a mutation. A request that
	// repeats it gets the stored result instead of a second effect.
	RequestID string `json:"requestId,omitempty"`
}

var jsonNull = json.RawMessage("null")

func response(id *int64, result json.RawMessage) message {
	if len(result) == 0 {
		result = jsonNull
	}
	return message{JSONRPC: "2.0", ID: id, Result: result}
}

func errorResponse(id *int64, err *Error) message {
	return message{JSONRPC: "2.0", ID: id, Error: err.wire()}
}
