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
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Exact syntax pulled from https://github.com/jj-vcs/jj/blob/v0.39.0/lib/src/revset.pest

// Quote escapes a string as a revset or templating [string literal].
//
// [string literal]: https://docs.jj-vcs.dev/latest/templates/#stringliteral-type
func Quote(s string) string {
	const hexTable = "0123456789abcdef"

	sb := new(strings.Builder)
	sb.Grow(len(s) + len(`""`))
	sb.WriteString(`"`)
	for _, c := range s {
		switch {
		case c == '\\' || c == '"':
			sb.WriteByte('\\')
			sb.WriteByte(byte(c))
		case c == '\t':
			sb.WriteString(`\t`)
		case c == '\r':
			sb.WriteString(`\r`)
		case c == '\n':
			sb.WriteString(`\n`)
		case unicode.IsPrint(c):
			sb.WriteRune(c)
		default:
			// See https://github.com/jj-vcs/jj/issues/9041
			sb.WriteString(`\x`)
			sb.WriteByte(hexTable[(c>>4)&0x0f])
			sb.WriteByte(hexTable[c&0x0f])
		}
	}
	sb.WriteString(`"`)
	return sb.String()
}

// Unquote interprets s as a revset symbol
// (i.e. an identifier or a string literal),
// returning the string value that s quotes.
func Unquote(s string) (string, error) {
	if len(s) == 0 {
		return "", fmt.Errorf("unquote symbol: empty")
	}

	switch s[0] {
	case '\'':
		if len(s) < 2 || s[len(s)-1] != '\'' {
			return "", fmt.Errorf("unquote symbol: invalid single-quoted syntax")
		}
		unquoted := s[1 : len(s)-1]
		if strings.Contains(unquoted, "'") {
			return "", fmt.Errorf("unquote symbol: invalid single-quoted syntax")
		}
		return unquoted, nil
	case '"':
		if len(s) < 2 || s[len(s)-1] != '"' {
			return "", fmt.Errorf("unquote symbol: invalid double-quoted syntax")
		}
		inner := s[1 : len(s)-1]
		if !strings.ContainsAny(inner, `\"`) {
			return inner, nil
		}
		sb := new(strings.Builder)
		sb.Grow(len(inner))
		for i := 0; i < len(inner); i++ {
			switch c := inner[i]; c {
			case '\\':
				i++
				if i >= len(inner) {
					return "", fmt.Errorf("unquote symbol: invalid double-quoted syntax")
				}
				switch c = inner[i]; c {
				case '\\', '"':
					sb.WriteByte(c)
				case 't':
					sb.WriteByte('\t')
				case 'r':
					sb.WriteByte('\r')
				case 'n':
					sb.WriteByte('\n')
				case '0':
					sb.WriteByte(0)
				case 'e':
					sb.WriteByte(0x1b)
				case 'x':
					if i+2 >= len(inner) {
						return "", fmt.Errorf("unquote symbol: invalid double-quoted syntax")
					}
					x1, ok1 := hexDigit(inner[i+1])
					x2, ok2 := hexDigit(inner[i+2])
					if !ok1 || !ok2 {
						return "", fmt.Errorf("unquote symbol: invalid double-quoted syntax")
					}
					// See https://github.com/jj-vcs/jj/issues/9041
					sb.WriteRune(rune(x1<<4 | x2))
					i += 2
				default:
					return "", fmt.Errorf("unquote symbol: invalid escape")
				}
			case '"':
				return "", fmt.Errorf("unquote symbol: invalid double-quoted syntax")
			default:
				sb.WriteByte(c)
			}
		}
		return sb.String(), nil
	}

	// Try for identifier.
	r, i := utf8.DecodeRuneInString(s)
	if !IsIdentifierRune(r) {
		return "", fmt.Errorf("unquote symbol: invalid identifier syntax")
	}
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		switch {
		case r == '.' || r == '+':
			r, size := utf8.DecodeRuneInString(s[i:])
			i += size
			if !IsIdentifierRune(r) {
				return "", fmt.Errorf("unquote symbol: invalid identifier syntax")
			}
		case r == '-':
			for {
				r, size := utf8.DecodeRuneInString(s[i:])
				i += size
				if IsIdentifierRune(r) {
					break
				}
				if r != '-' {
					return "", fmt.Errorf("unquote symbol: invalid identifier syntax")
				}
			}
		case !IsIdentifierRune(r):
			return "", fmt.Errorf("unquote symbol: invalid identifier syntax")
		}
	}

	return s, nil
}

// IsIdentifierRune reports whether c can be used in a revset identifier.
func IsIdentifierRune(c rune) bool {
	return c == '_' || c == '*' || c == '/' ||
		unicode.In(c, idContinueRanges...) && !unicode.In(c, idContinueExcludeRanges...)
}

var (
	idContinueRanges = []*unicode.RangeTable{
		unicode.L,
		unicode.Nl,
		unicode.Other_ID_Start,
		unicode.Mn,
		unicode.Mc,
		unicode.Nd,
		unicode.Pc,
		unicode.Other_ID_Continue,
	}
	idContinueExcludeRanges = []*unicode.RangeTable{
		unicode.Pattern_Syntax,
		unicode.Pattern_White_Space,
	}
)

func hexDigit(c byte) (byte, bool) {
	switch {
	case '0' <= c && c <= '9':
		return c - '0', true
	case 'a' <= c && c <= 'f':
		return c - 'a' + 0xa, true
	case 'A' <= c && c <= 'F':
		return c - 'A' + 0xa, true
	default:
		return 0, false
	}
}
