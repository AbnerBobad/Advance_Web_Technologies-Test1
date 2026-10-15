package main

import "sync"

// jobBroadcaster wakes every goroutine that is waiting on a particular job.
// It implements the notification half of the "Counter + Version" pattern:
// a committed change closes the job's current signal channel (a broadcast,
// not a single receive), and the next subscriber receives a fresh channel.
// The database remains the source of truth; a wake-up is only an instruction
// to re-read the persisted representation (VER-03, WAIT-01).
type jobBroadcaster struct {
	mu    sync.Mutex
	state map[string]*jobSignal
}

// jobSignal is the shared notification channel for one job together with the
// number of waiters holding it. Reference counting lets the broadcaster drop
// an idle job's entry once no observer still needs it.
type jobSignal struct {
	ch   chan struct{}
	refs int
}

func newJobBroadcaster() *jobBroadcaster {
	return &jobBroadcaster{state: make(map[string]*jobSignal)}
}

// subscribe returns the current signal channel for publicID and records the
// caller as a waiter. Callers must subscribe before reading the job's version
// so that a change committed after the read cannot slip between the check and
// the wait. The matching cleanup is release.
func (b *jobBroadcaster) subscribe(publicID string) *jobSignal {
	b.mu.Lock()
	defer b.mu.Unlock()

	sig, ok := b.state[publicID]
	if !ok {
		sig = &jobSignal{ch: make(chan struct{})}
		b.state[publicID] = sig
	}
	sig.refs++
	return sig
}

// release removes one waiter. When the last waiter leaves, the entry is
// dropped unless a broadcast already replaced it.
func (b *jobBroadcaster) release(publicID string, sig *jobSignal) {
	b.mu.Lock()
	defer b.mu.Unlock()

	sig.refs--
	if sig.refs == 0 {
		if current, ok := b.state[publicID]; ok && current == sig {
			delete(b.state, publicID)
		}
	}
}

// broadcast closes the job's current channel after a change has committed,
// waking all waiters at once. The entry is removed so the next subscriber
// builds a fresh, open channel; waiters already holding the closed channel
// still observe the wake-up and re-read state.
func (b *jobBroadcaster) broadcast(publicID string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	sig, ok := b.state[publicID]
	if !ok {
		return
	}
	close(sig.ch)
	delete(b.state, publicID)
}
