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

// fakeIdler is an idleFunc the tests drive: each call waits for counts to report on its channel.
type fakeIdler struct {
	mu      sync.Mutex
	err     error
	running map[string]chan int
	calls   int
}

func (f *fakeIdler) idle(userID string, stop <-chan struct{}, report func(int)) error {
	f.mu.Lock()
	f.calls++
	if f.err != nil {
		err := f.err
		f.mu.Unlock()
		return err
	}
	counts := make(chan int)
	f.running[userID] = counts
	f.mu.Unlock()

	defer func() {
		f.mu.Lock()
		delete(f.running, userID)
		f.mu.Unlock()
	}()
	for {
		select {
		case <-stop:
			return nil
		case unread, ok := <-counts:
			if !ok {
				return errors.New("connection lost")
			}
			report(unread)
		}
	}
}

func (f *fakeIdler) channel(userID string) chan int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.running[userID]
}

func (f *fakeIdler) setErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func (f *fakeIdler) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func TestUnreadWatcherIdles(t *testing.T) {
	w, inboxes, now := newTestWatcher()
	idler := &fakeIdler{running: map[string]chan int{}}
	w.idle = idler.idle
	inboxes.set("ada", 2, false)
	w.Touch("ada")

	assert.Empty(t, tickAndWait(t, w), "the inbox is watched with IDLE instead of checked")
	require.Eventually(t, func() bool { return idler.channel("ada") != nil }, time.Second, time.Millisecond)
	idler.channel("ada") <- 4
	idler.channel("ada") <- 4
	idler.channel("ada") <- 5
	require.Eventually(t, func() bool { return len(inboxes.publishedTo("ada")) == 2 }, time.Second, time.Millisecond)
	assert.Equal(t, []int{4, 5}, inboxes.publishedTo("ada"))

	// The count stays fresh while IDLE runs, without checks
	now.Add(time.Hour)
	w.Touch("ada")
	assert.Empty(t, tickAndWait(t, w))
	unread, ok := w.Cached("ada")
	assert.True(t, ok)
	assert.Equal(t, 5, unread)
	inboxes.mu.Lock()
	assert.Zero(t, inboxes.checks["ada"])
	inboxes.mu.Unlock()

	// When IDLE fails the inbox is checked again, and IDLE is retried later
	close(idler.channel("ada"))
	require.Eventually(t, func() bool {
		w.mu.Lock()
		defer w.mu.Unlock()
		return w.inboxes["ada"].stopIdle == nil
	}, time.Second, time.Millisecond)
	assert.Equal(t, []string{"ada"}, tickAndWait(t, w))
	assert.Equal(t, 1, idler.callCount())
	now.Add(idleRetryAfter)
	assert.Empty(t, tickAndWait(t, w))
	require.Eventually(t, func() bool { return idler.channel("ada") != nil }, time.Second, time.Millisecond)
	assert.Equal(t, 2, idler.callCount())

	// Going offline stops IDLE
	now.Add(watchIdleTimeout + time.Second)
	tickAndWait(t, w)
	require.Eventually(t, func() bool {
		w.mu.Lock()
		defer w.mu.Unlock()
		return w.idlers == 0
	}, time.Second, time.Millisecond)
}

func TestUnreadWatcherPollsServersWithoutIdle(t *testing.T) {
	w, inboxes, now := newTestWatcher()
	idler := &fakeIdler{running: map[string]chan int{}, err: errIdleUnsupported}
	w.idle = idler.idle
	inboxes.set("ada", 2, false)
	w.Touch("ada")

	tickAndWait(t, w)
	require.Eventually(t, func() bool {
		w.mu.Lock()
		defer w.mu.Unlock()
		return w.inboxes["ada"].stopIdle == nil
	}, time.Second, time.Millisecond)
	for range 3 {
		w.Touch("ada")
		assert.Equal(t, []string{"ada"}, tickAndWait(t, w))
		now.Add(2 * time.Minute)
	}
	assert.Equal(t, 1, idler.callCount(), "IDLE isn't tried again soon")
	assert.Equal(t, []int{2}, inboxes.publishedTo("ada"))

	idler.setErr(nil)
	now.Add(noIdleRecheck)
	w.Touch("ada")
	tickAndWait(t, w)
	require.Eventually(t, func() bool { return idler.channel("ada") != nil }, time.Second, time.Millisecond)

	w.Forget("ada")
	require.Eventually(t, func() bool { return idler.channel("ada") == nil }, time.Second, time.Millisecond)
}
