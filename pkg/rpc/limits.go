// Package rpc is the wire protocol between GitboxApp and gitbox-server:
// JSON-RPC 2.0 in frames of the LSP base protocol, over one pair of byte
// streams (the server's stdin and stdout). It knows nothing about git,
// providers or the app's types: handlers and the error conversion are handed
// to it.
package rpc

import "time"

// Every bound of the protocol, in one place. Both ends treat what the other
// sends as untrusted input, so nothing is read, queued or remembered without
// a limit.
const (
	// MaxFrameBytes caps the body of one frame.
	MaxFrameBytes = 16 << 20
	// MaxHeaderBytes caps the header block of one frame.
	MaxHeaderBytes = 1 << 10
	// MaxBannerBytes caps what the client discards before the handshake
	// magic: a login shell may print a banner first.
	MaxBannerBytes = 1 << 20
	// MaxQueuedMessages caps the server's outgoing queue.
	MaxQueuedMessages = 1024
	// MaxRememberedRequests is how many request ids the server remembers,
	// with their results, to answer a repeated mutation.
	MaxRememberedRequests = 256
	// ProgressInterval is the least time between two progress events of the
	// same operation; the latest value wins.
	ProgressInterval = 100 * time.Millisecond
	// PingInterval is how often the client checks the link, and
	// MaxMissedPings how many unanswered checks declare it dead.
	PingInterval   = 10 * time.Second
	MaxMissedPings = 3
)
