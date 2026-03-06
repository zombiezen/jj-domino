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

import "strings"

// Quote escapes a string as a revset or templating [string literal].
//
// [string literal]: https://docs.jj-vcs.dev/latest/templates/#stringliteral-type
func Quote(s string) string {
	const hexTable = "0123456789abcdef"

	sb := new(strings.Builder)
	sb.Grow(len(s) + len(`""`))
	sb.WriteString(`"`)
	for _, c := range []byte(s) {
		switch {
		case c == '\\' || c == '"':
			sb.WriteByte('\\')
			sb.WriteByte(c)
		case c == '\t':
			sb.WriteString(`\t`)
		case c == '\r':
			sb.WriteString(`\r`)
		case c == '\n':
			sb.WriteString(`\n`)
		case 0x20 <= c && c < 0x7f: // Printable.
			sb.WriteByte(c)
		default:
			sb.WriteString(`\x`)
			sb.WriteByte(hexTable[c>>4])
			sb.WriteByte(hexTable[c&0x0f])
		}
	}
	sb.WriteString(`"`)
	return sb.String()
}
