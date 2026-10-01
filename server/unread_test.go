// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

package main

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeInboxes struct {
	mu        sync.Mutex
	unread    map[string]int
	failing   map[string]bool
	checks    map[string]int
	published map[string][]int
}

func (f *fakeInboxes) check(userID string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checks[userID]++
	if f.failing[userID] {
		return 0, errors.New("login failed")
	}
	return f.unread[userID], nil
}

func (f *fakeInboxes) publish(userID string, unread int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.published[userID] = append(f.published[userID], unread)
}

func (f *fakeInboxes) set(userID string, unread int, failing bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unread[userID] = unread
	f.failing[userID] = failing
}

func (f *fakeInboxes) publishedTo(userID string) []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int(nil), f.published[userID]...)
}

// testClock is a clock the tests move forward.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newTestWatcher() (*unreadWatcher, *fakeInboxes, *testClock) {
	inboxes := &fakeInboxes{unread: map[string]int{}, failing: map[string]bool{}, checks: map[string]int{}, published: map[string][]int{}}
	clock := &testClock{now: time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)}
	w := newUnreadWatcher(inboxes.check, inboxes.publish, func() time.Duration { return 2 * time.Minute })
	w.now = clock.Now
	return w, inboxes, clock
}

// tickAndWait runs a tick and waits for the checks it started.
func tickAndWait(t *testing.T, w *unreadWatcher) []string {
	t.Helper()
	due := w.tick()
	require.Eventually(t, func() bool { return len(w.running) == 0 }, time.Second, time.Millisecond)
	return due
}

func TestUnreadWatcher(t *testing.T) {
	w, inboxes, now := newTestWatcher()
	inboxes.set("ada", 2, false)

	assert.Empty(t, tickAndWait(t, w), "nobody is online")

	w.Touch("ada")
	assert.Equal(t, []string{"ada"}, tickAndWait(t, w))
	assert.Equal(t, []int{2}, inboxes.publishedTo("ada"), "the first count is published")
	unread, ok := w.Cached("ada")
	assert.True(t, ok)
	assert.Equal(t, 2, unread)

	// Not checked again before the interval, nor published when unchanged
	now.Add(time.Minute)
	assert.Empty(t, tickAndWait(t, w))
	now.Add(time.Minute)
	assert.Equal(t, []string{"ada"}, tickAndWait(t, w))
	assert.Equal(t, []int{2}, inboxes.publishedTo("ada"))

	inboxes.set("ada", 3, false)
	now.Add(2 * time.Minute)
	w.Touch("ada")
	tickAndWait(t, w)
	assert.Equal(t, []int{2, 3}, inboxes.publishedTo("ada"))

	// Counts known from requests are published when they change
	w.Record("ada", 1)
	assert.Equal(t, []int{2, 3, 1}, inboxes.publishedTo("ada"))
	w.Record("ada", 1)
	assert.Equal(t, []int{2, 3, 1}, inboxes.publishedTo("ada"))

	// Users that went offline aren't watched any more
	now.Add(watchIdleTimeout + time.Second)
	assert.Empty(t, tickAndWait(t, w))
	_, ok = w.Cached("ada")
	assert.False(t, ok)
}

func TestUnreadWatcherBacksOff(t *testing.T) {
	w, inboxes, now := newTestWatcher()
	inboxes.set("ada", 0, true)
	w.Touch("ada")

	checkAfter := func(d time.Duration) bool {
		now.Add(d)
		w.Touch("ada")
		return len(tickAndWait(t, w)) > 0
	}
	assert.True(t, checkAfter(0))
	assert.False(t, checkAfter(2*time.Minute), "waits twice the interval after a failure")
	assert.True(t, checkAfter(2*time.Minute))
	assert.False(t, checkAfter(4*time.Minute), "then four times")
	assert.True(t, checkAfter(4*time.Minute))
	assert.Empty(t, inboxes.publishedTo("ada"))

	inboxes.set("ada", 0, false)
	assert.True(t, checkAfter(16*time.Minute))
	assert.True(t, checkAfter(2*time.Minute), "back to the interval after a success")

	w.Forget("ada")
	now.Add(time.Hour)
	assert.Empty(t, tickAndWait(t, w))
}

func TestUnreadWatcherLimitsChecks(t *testing.T) {
	w, _, _ := newTestWatcher()
	release := make(chan struct{})
	w.check = func(string) (int, error) {
		<-release
		return 0, nil
	}
	for i := range maxConcurrentChecks + 5 {
		w.Touch(string(rune('a' + i)))
	}
	assert.Len(t, w.tick(), maxConcurrentChecks)
	assert.Empty(t, w.tick(), "the running checks hold all the slots")
	close(release)
}
