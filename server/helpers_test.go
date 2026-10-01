// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

package main

import (
	"bytes"
	"errors"
	"strings"
	"sync"

	"github.com/mattermost/mattermost/server/public/model"
)

// fakeKV is an in-memory kvAPI.
type fakeKV struct {
	mu   sync.Mutex
	data map[string][]byte
}

func newFakeKV() *fakeKV {
	return &fakeKV{data: map[string][]byte{}}
}

func (f *fakeKV) KVGet(key string) ([]byte, *model.AppError) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if v, ok := f.data[key]; ok {
		return bytes.Clone(v), nil
	}
	return nil, nil
}

func (f *fakeKV) KVSetWithOptions(key string, value []byte, options model.PluginKVSetOptions) (bool, *model.AppError) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if options.Atomic {
		current, exists := f.data[key]
		if options.OldValue == nil && exists || options.OldValue != nil && (!exists || !bytes.Equal(current, options.OldValue)) {
			return false, nil
		}
	}
	if value == nil {
		delete(f.data, key)
	} else {
		f.data[key] = bytes.Clone(value)
	}
	return true, nil
}

func (f *fakeKV) KVDelete(key string) *model.AppError {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.data, key)
	return nil
}

// validTestToken returns whether the test mail servers accept an OAuth access token.
func validTestToken(token string) bool {
	return strings.HasPrefix(token, "valid-")
}

// testXOAuth2Server is the server side of the XOAUTH2 mechanism, for the test mail servers.
type testXOAuth2Server struct {
	login  func(username string) error
	failed bool
}

func (s *testXOAuth2Server) Next(response []byte) ([]byte, bool, error) {
	if s.failed {
		return nil, true, errors.New("invalid token")
	}
	fields := strings.Split(string(response), "\x01")
	username, okUser := strings.CutPrefix(fields[0], "user=")
	token, okToken := "", false
	if len(fields) > 1 {
		token, okToken = strings.CutPrefix(fields[1], "auth=Bearer ")
	}
	if !okUser || !okToken || !validTestToken(token) {
		// The error details, answered with an empty response
		s.failed = true
		return []byte(`{"status":"401","schemes":"bearer"}`), false, nil
	}
	if err := s.login(username); err != nil {
		return nil, true, err
	}
	return nil, true, nil
}
