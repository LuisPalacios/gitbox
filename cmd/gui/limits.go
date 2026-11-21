package main

import "time"

// How long each kind of work may take. Everything is also cancelled when
// the app closes, and network work when its server stops answering; these
// bounds catch the command that neither finishes nor fails.
const (
	// statusTimeout bounds the local status check of one clone.
	statusTimeout = 20 * time.Second
	// fetchTimeout and pullTimeout bound one fetch and one pull.
	fetchTimeout = 120 * time.Second
	pullTimeout  = 120 * time.Second
	// healReadTimeout bounds the reads of a clone's configuration before a
	// fetch or a pull.
	healReadTimeout = 15 * time.Second
	// shutdownWait is how long closing the app waits for work in flight to
	// stop. That work was already cancelled, and its processes with it.
	shutdownWait = 5 * time.Second
)

// A clone has no bound: its size is not known in advance. It is cancelled
// when the app closes.
