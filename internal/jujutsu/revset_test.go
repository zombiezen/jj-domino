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

var quoteTests = []struct {
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

func TestQuote(t *testing.T) {
	for _, test := range quoteTests {
		if got := Quote(test.s); got != test.want {
			t.Errorf("Quote(%q) = %q; want %q", test.s, got, test.want)
		}
	}
}

var unquoteTests = []struct {
	symbol string
	want   string
	err    bool
}{
	{symbol: "", err: true},
	{symbol: "0", want: "0"},
	{symbol: `foo_bar/baz`, want: "foo_bar/baz"},
	{symbol: `*/foo/**`, want: "*/foo/**"},
	{symbol: `foo.bar-v1+7`, want: "foo.bar-v1+7"},
	{symbol: `foo.bar-v1+7-`, err: true},
	{symbol: `foo--bar`, want: "foo--bar"},
	{symbol: `foo----bar`, want: "foo----bar"},
	{symbol: `.foo`, err: true},
	{symbol: `foo.`, err: true},
	{symbol: `foo.+bar`, err: true},
	{symbol: `foo++bar`, err: true},
	{symbol: `foo+-bar`, err: true},
	{symbol: `柔術+jj`, want: "柔術+jj"},
	{symbol: `"\t\r\n\"\\\0\e"`, want: "\t\r\n\"\\\x00\x1b"},
	{symbol: `"\y"`, err: true},
	{symbol: `''`, want: ""},
	{symbol: `'a\n'`, want: `a\n`},
	{symbol: `'"'`, want: `"`},
	{symbol: `""`, want: ""},
	{symbol: `"\x61\x65\x69\x6f\x75"`, want: "aeiou"},
	{symbol: `"\xe0\xe8\xec\xf0\xf9"`, want: "àèìðù"},
	{symbol: `"\x"`, err: true},
	{symbol: `"\xf"`, err: true},
	{symbol: `"\xgg"`, err: true},
}

func TestUnquote(t *testing.T) {
	for _, test := range quoteTests {
		if got, err := Unquote(test.want); got != test.s || err != nil {
			t.Errorf("Unquote(%q) = %q, %v; want %q, <nil>", test.want, got, err, test.s)
		}
	}
	for _, test := range unquoteTests {
		if got, err := Unquote(test.symbol); !test.err && (got != test.want || err != nil) {
			t.Errorf("Unquote(%q) = %q, %v; want %q, <nil>", test.symbol, got, err, test.want)
		} else if test.err && (got != test.want || err == nil) {
			t.Errorf("Unquote(%q) = %q, %v; want %q, <error>", test.symbol, got, err, test.want)
		}
	}
}

func FuzzUnquote(f *testing.F) {
	for _, test := range quoteTests {
		f.Add(test.want)
	}
	for _, test := range unquoteTests {
		f.Add(test.symbol)
	}

	f.Fuzz(func(t *testing.T, s string) {
		parsed1, err := Unquote(s)
		if err != nil {
			t.Skip(err)
		}
		quoted := Quote(parsed1)
		parsed2, err := Unquote(quoted)
		if parsed2 != parsed1 || err != nil {
			t.Errorf("Unquote(%q) = %q, %v; want %q, <nil>", quoted, parsed2, err, parsed1)
		}
	})
}

func TestParseRefSymbol(t *testing.T) {
	tests := []struct {
		s    string
		want RefSymbol
		err  bool
	}{
		{s: "", err: true},
		{s: "foo", want: RefSymbol{Name: "foo"}},
		{s: `"foo++bar"`, want: RefSymbol{Name: "foo++bar"}},
		{s: "foo@bar", want: RefSymbol{Name: "foo", Remote: "bar"}},
		{s: "  foo@bar  ", want: RefSymbol{Name: "foo", Remote: "bar"}},
	}

	for _, test := range tests {
		got, err := parseRefSymbol(test.s)
		if !test.err && (got != test.want || err != nil) {
			t.Errorf("ParseRefSymbol(%q) = %+v, %v; want %+v, <nil>", test.s, got, err, test.want)
		} else if test.err && (got != test.want || err == nil) {
			t.Errorf("ParseRefSymbol(%q) = %+v, %v; want %+v, <error>", test.s, got, err, test.want)
		}
	}
}
