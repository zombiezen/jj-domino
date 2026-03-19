// Copyright 2026 Roxy Light and Benjamin Pollack
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is furnished
// to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice (including the next
// paragraph) shall be included in all copies or substantial portions of the
// Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS
// FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS
// OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY,
// WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF
// OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
//
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"testing"
	"time"
)

// testContext returns a [context.Context] that is canceled
// just before Cleanup-registered functions are called
// or shortly before the test deadline,
// whichever comes first.
func testContext(tb testing.TB) context.Context {
	ctx := tb.Context()
	deadline, ok := tbDeadline(tb)
	if !ok {
		return ctx
	}
	ctx, cancel := context.WithDeadline(ctx, deadline.Add(-10*time.Second))
	tb.Cleanup(cancel)
	return ctx
}

func tbDeadline(tb testing.TB) (deadline time.Time, ok bool) {
	d, ok := tb.(deadliner)
	if !ok {
		return time.Time{}, false
	}
	return d.Deadline()
}

type deadliner interface {
	Deadline() (deadline time.Time, ok bool)
}

var _ deadliner = (*testing.T)(nil)
