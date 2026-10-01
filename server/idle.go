// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

package main

import (
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/pkg/errors"
)

const (
	// idleRecount is how often the unread count of an inbox watched with IDLE is counted again
	// even without updates, in case the server missed telling some.
	idleRecount = 10 * time.Minute
	// idleSettle is how long to wait after an update for the ones that come with it (a new
	// message comes with its flags), so the inbox is counted once.
	idleSettle = 500 * time.Millisecond
)

// idleInbox watches a user's inbox with IMAP IDLE (RFC 2177) until stop is closed, reporting its
// unread count at first and after each update from the server. The connection takes one of the
// connection slots, only if one is free right away.
func (p *Plugin) idleInbox(userID string, stop <-chan struct{}, report func(unread int)) error {
	account, err := p.store.GetAccount(userID)
	if err != nil {
		return err
	}
	if account == nil {
		return errNoAccount
	}
	release, err := p.limiter.tryAcquire(userID)
	if err != nil {
		return errIdleNoSlot
	}
	defer release()

	updated := make(chan struct{}, 1)
	notify := func() {
		select {
		case updated <- struct{}{}:
		default:
		}
	}
	opts := p.imapOptions()
	opts.unilateral = &imapclient.UnilateralDataHandler{
		Expunge: func(uint32) { notify() },
		Mailbox: func(*imapclient.UnilateralDataMailbox) { notify() },
		Fetch:   func(*imapclient.FetchMessageData) { notify() },
	}
	session, err := dialIMAP(account.IMAP, account.IMAPPassword, opts)
	if err != nil {
		return err
	}
	defer func() {
		session.begin(logoutTimeout)
		session.Close()
	}()
	if !session.client.Caps().Has(imap.CapIdle) {
		return errIdleUnsupported
	}

	count := func() error {
		session.begin(operationTimeout)
		defer session.end()
		// Updates up to now are counted
		select {
		case <-updated:
		default:
		}
		unread, err := session.unreadSelected()
		if err != nil {
			return err
		}
		report(unread)
		return nil
	}

	session.begin(operationTimeout)
	_, err = session.selectFolder("INBOX", true)
	session.end()
	if err != nil {
		return err
	}
	if err := count(); err != nil {
		return err
	}

	recount := time.NewTimer(idleRecount)
	defer recount.Stop()
	for {
		session.begin(operationTimeout)
		idle, err := session.client.Idle()
		session.end()
		if err != nil {
			return errors.Wrap(err, "failed to start IDLE")
		}

		stopped := false
		select {
		case <-stop:
			stopped = true
		case <-updated:
			// Let the updates that come with this one arrive
			select {
			case <-stop:
				stopped = true
			case <-time.After(idleSettle):
			}
		case <-recount.C:
		case <-session.client.Closed():
		}
		recount.Reset(idleRecount)

		session.begin(operationTimeout)
		err = idle.Close()
		if err == nil {
			err = idle.Wait()
		}
		session.end()
		if stopped {
			return nil
		}
		if err != nil {
			return errors.Wrap(err, "IDLE failed")
		}
		if err := count(); err != nil {
			return err
		}
	}
}
