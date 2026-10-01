// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

package main

import (
	"sync"
	"time"
)

const (
	// watchIdleTimeout is how long a user's inbox is watched after their last request. The
	// webapp asks for the unread count every few minutes while it's open.
	watchIdleTimeout = 15 * time.Minute
	// maxWatchedUsers bounds the number of inboxes watched at once.
	maxWatchedUsers = 2000
	// maxConcurrentChecks bounds the number of inboxes checked at once.
	maxConcurrentChecks = 10
	// maxCheckBackoff is the longest wait between the checks of an inbox that fail.
	maxCheckBackoff = time.Hour
	// watchTick is how often the watcher looks for inboxes to check.
	watchTick = 15 * time.Second
)

// watchedInbox is the state of the inbox of an online user.
type watchedInbox struct {
	lastSeen  time.Time
	lastCheck time.Time
	unread    int
	known     bool
	failures  int
	checking  bool
}

// unreadWatcher keeps the unread counts of the inboxes of online users up to date, checking them
// periodically and telling users when their count changes.
type unreadWatcher struct {
	mu      sync.Mutex
	inboxes map[string]*watchedInbox
	running chan struct{}

	// check returns the unread count of a user's inbox.
	check func(userID string) (int, error)
	// publish tells a user their unread count changed.
	publish func(userID string, unread int)
	// interval returns the time between two checks of an inbox.
	interval func() time.Duration
	now      func() time.Time
}

func newUnreadWatcher(check func(string) (int, error), publish func(string, int), interval func() time.Duration) *unreadWatcher {
	return &unreadWatcher{
		inboxes:  map[string]*watchedInbox{},
		running:  make(chan struct{}, maxConcurrentChecks),
		check:    check,
		publish:  publish,
		interval: interval,
		now:      time.Now,
	}
}

// Touch records that a user is online, starting to watch their inbox.
func (w *unreadWatcher) Touch(userID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if inbox, ok := w.inboxes[userID]; ok {
		inbox.lastSeen = w.now()
		return
	}
	if len(w.inboxes) < maxWatchedUsers {
		w.inboxes[userID] = &watchedInbox{lastSeen: w.now()}
	}
}

// Forget stops watching a user's inbox, e.g. when they disconnect their account.
func (w *unreadWatcher) Forget(userID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.inboxes, userID)
}

// Cached returns the last known unread count of a user's inbox, if it's recent enough.
func (w *unreadWatcher) Cached(userID string) (int, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	inbox, ok := w.inboxes[userID]
	if !ok || !inbox.known || w.now().Sub(inbox.lastCheck) > w.interval() {
		return 0, false
	}
	return inbox.unread, true
}

// Record stores an unread count known from elsewhere (such as a request), telling the user if it
// changed.
func (w *unreadWatcher) Record(userID string, unread int) {
	w.mu.Lock()
	inbox, ok := w.inboxes[userID]
	if !ok {
		w.mu.Unlock()
		return
	}
	changed := inbox.known && inbox.unread != unread
	inbox.unread, inbox.known, inbox.lastCheck, inbox.failures = unread, true, w.now(), 0
	w.mu.Unlock()

	if changed {
		w.publish(userID, unread)
	}
}

func (w *unreadWatcher) backoff(failures int) time.Duration {
	wait := w.interval()
	for range failures {
		wait *= 2
		if wait >= maxCheckBackoff {
			return maxCheckBackoff
		}
	}
	return wait
}

// tick drops the inboxes of users that went offline, and checks the inboxes that are due. It
// returns the users whose inbox it started checking.
func (w *unreadWatcher) tick() []string {
	now := w.now()
	var due []string

	w.mu.Lock()
	for userID, inbox := range w.inboxes {
		if now.Sub(inbox.lastSeen) > watchIdleTimeout {
			delete(w.inboxes, userID)
			continue
		}
		if inbox.checking || now.Sub(inbox.lastCheck) < w.backoff(inbox.failures) {
			continue
		}
		select {
		case w.running <- struct{}{}:
			inbox.checking = true
			due = append(due, userID)
		default:
			// Enough checks running, the others wait for the next tick
		}
	}
	w.mu.Unlock()

	for _, userID := range due {
		go w.checkInbox(userID)
	}
	return due
}

func (w *unreadWatcher) checkInbox(userID string) {
	defer func() { <-w.running }()
	unread, err := w.check(userID)

	w.mu.Lock()
	inbox, ok := w.inboxes[userID]
	if !ok {
		w.mu.Unlock()
		return
	}
	inbox.checking = false
	inbox.lastCheck = w.now()
	if err != nil {
		inbox.failures++
		w.mu.Unlock()
		return
	}
	changed := !inbox.known || inbox.unread != unread
	inbox.unread, inbox.known, inbox.failures = unread, true, 0
	w.mu.Unlock()

	if changed {
		w.publish(userID, unread)
	}
}

// Run checks inboxes until stop is closed.
func (w *unreadWatcher) Run(stop <-chan struct{}) {
	ticker := time.NewTicker(watchTick)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			w.tick()
		}
	}
}
