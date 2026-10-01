// Copyright (c) 2026-present Antimatter contributors.
// See LICENSE.txt for license information.

package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConnLimiter(t *testing.T) {
	l := newConnLimiter(3, 2, 10*time.Millisecond)

	a1, err := l.acquire("a")
	require.NoError(t, err)
	a2, err := l.acquire("a")
	require.NoError(t, err)
	_, err = l.acquire("a")
	assert.Equal(t, errTooManyUserConnections, err)

	b1, err := l.acquire("b")
	require.NoError(t, err)
	_, err = l.acquire("b")
	assert.Equal(t, errTooManyConnections, err, "all slots are taken")

	a1()
	a1() // releasing twice is harmless
	b2, err := l.acquire("b")
	require.NoError(t, err)

	a2()
	b1()
	b2()
	assert.Empty(t, l.perUser)
	assert.Empty(t, l.slots)
}
