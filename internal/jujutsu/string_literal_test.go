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

package jujutsu

import "testing"

func TestQuote(t *testing.T) {
	tests := []struct {
		s    string
		want string
	}{
		{"", `""`},
		{"foo", `"foo"`},
		{"foo bar", `"foo bar"`},
		{"foo\nbar", `"foo\nbar"`},
		{"foo\\bar", `"foo\\bar"`},
		{"foo\\bar", `"foo\\bar"`},
		{"foo\x00bar", `"foo\x00bar"`},
	}

	for _, test := range tests {
		if got := Quote(test.s); got != test.want {
			t.Errorf("Quote(%q) = %q; want %q", test.s, got, test.want)
		}
	}
}
