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

import (
	"slices"
	"testing"
)

func TestCommandNameAndArgs(t *testing.T) {
	tests := []struct {
		name   string
		c      *CommandNameAndArgs
		argv   []string
		string string
	}{
		{
			name:   "NiladicString",
			c:      CommandString("echo"),
			argv:   []string{"echo"},
			string: "echo",
		},
		{
			name:   "MultipleArgsString",
			c:      CommandString("echo foo bar"),
			argv:   []string{"echo", "foo", "bar"},
			string: "echo foo bar",
		},
		{
			name:   "QuotedArgsString",
			c:      CommandString("echo 'foo bar' \"baz\""),
			argv:   []string{"echo", "foo bar", "baz"},
			string: "echo 'foo bar' \"baz\"",
		},
		{
			name:   "StringVars",
			c:      CommandString("echo $left $right"),
			argv:   []string{"echo", "$left", "$right"},
			string: "echo $left $right",
		},
		{
			name:   "NiladicArgs",
			c:      CommandArgv("echo", nil, nil),
			argv:   []string{"echo"},
			string: "echo",
		},
		{
			name:   "MultipleArgsWithoutSpecial",
			c:      CommandArgv("echo", []string{"foo", "bar"}, nil),
			argv:   []string{"echo", "foo", "bar"},
			string: "echo foo bar",
		},
		{
			name:   "MultipleArgsWithSpecial",
			c:      CommandArgv("echo", []string{"foo bar", "baz"}, nil),
			argv:   []string{"echo", "foo bar", "baz"},
			string: "echo 'foo bar' baz",
		},
		{
			name:   "Env",
			c:      CommandArgv("echo", nil, map[string]string{"FOO": "BAR"}),
			argv:   []string{"echo"},
			string: "FOO=BAR echo",
		},
	}

	t.Run("Argv", func(t *testing.T) {
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				if got := test.c.Argv(); !slices.Equal(got, test.argv) {
					t.Errorf("c.Argv() = %q; want %q", got, test.argv)
				}
			})
		}
	})

	t.Run("String", func(t *testing.T) {
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				if got := test.c.String(); got != test.string {
					t.Errorf("c.String() = %q; want %q", got, test.string)
				}
			})
		}
	})
}
