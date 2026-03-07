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
	"errors"
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

func quoteIfNeeded(s string) string {
	if s == "" {
		return `""`
	}
	for _, c := range s {
		if !IsIdentifierRune(c) {
			return Quote(s)
		}
	}
	return s
}

// Unquote interprets s as a revset symbol
// (i.e. an identifier or a string literal),
// returning the string value that s quotes.
func Unquote(s string) (string, error) {
	if len(s) == 0 {
		return "", fmt.Errorf("unquote symbol: empty")
	}

	p := &parser[string]{s}
	if _, err := p.symbol(); err != nil {
		return "", fmt.Errorf("unquote symbol: %v", err)
	}
	if len(p.s) > 0 {
		return "", fmt.Errorf("unquote symbol: trailing characters")
	}
	switch s[0] {
	case '\'':
		return s[1 : len(s)-1], nil
	case '"':
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
	default:
		return s, nil
	}
}

// RefSymbol is a bookmark or tag name.
// It is local if Remote is an empty string.
type RefSymbol struct {
	Name   string
	Remote string
}

// ParseRefSymbol parses a limited subset of the revset language
// for a ref symbol.
func ParseRefSymbol(s string) (RefSymbol, error) {
	return parseRefSymbol(s)
}

func parseRefSymbol[S ~[]byte | ~string](s S) (RefSymbol, error) {
	p := &parser[S]{s}
	p.skipWhitespace()
	refName, err := p.symbol()
	if err != nil {
		return RefSymbol{}, fmt.Errorf("parse ref symbol: %v", err)
	}
	var remote S
	if r, size := decodeRune(p.s); r == '@' {
		p.advance(size)
		var err error
		remote, err = p.symbol()
		if err != nil {
			return RefSymbol{}, fmt.Errorf("parse ref symbol: remote: %v", err)
		}
	}
	p.skipWhitespace()
	if len(p.s) > 0 {
		return RefSymbol{}, fmt.Errorf("parse ref symbol: trailing characters")
	}
	var ref RefSymbol
	ref.Name, err = Unquote(string(refName))
	if err != nil {
		return RefSymbol{}, fmt.Errorf("parse ref symbol: %v", err)
	}
	if len(remote) > 0 {
		var err error
		ref.Remote, err = Unquote(string(remote))
		if err != nil {
			return RefSymbol{}, fmt.Errorf("parse ref symbol: remote: %v", err)
		}
	}
	return ref, nil
}

// String returns the ref symbol in revset syntax.
func (ref RefSymbol) String() string {
	s, err := ref.AppendText(nil)
	if err != nil {
		panic(err)
	}
	return string(s)
}

// MarshalText implements [encoding.TextMarshaler]
// by returning the ref symbol in revset syntax.
func (ref RefSymbol) MarshalText(dst []byte) ([]byte, error) {
	return ref.AppendText(nil)
}

// AppendText implements [encoding.TextAppender]
// by appending the ref symbol in revset syntax.
func (ref RefSymbol) AppendText(dst []byte) ([]byte, error) {
	dst = append(dst, quoteIfNeeded(ref.Name)...)
	if ref.Remote != "" {
		dst = append(dst, '@')
		dst = append(dst, quoteIfNeeded(ref.Remote)...)
	}
	return dst, nil
}

// UnmarshalText implements [encoding.TextUnmarshaler]
// by parsing the ref symbol in the same revset syntax
// accepted by [ParseRefSymbol].
func (ref *RefSymbol) UnmarshalText(text []byte) error {
	newRef, err := parseRefSymbol(text)
	if err != nil {
		return err
	}
	*ref = newRef
	return nil
}

type parser[S ~string | ~[]byte] struct {
	s S
}

func (p *parser[S]) skipWhitespace() {
	for {
		r, size := decodeRune(p.s)
		if size == 0 || !unicode.IsSpace(r) {
			return
		}
		p.s = p.s[size:]
	}
}

func (p *parser[S]) advance(n int) S {
	result := p.s[:n]
	p.s = p.s[n:]
	return result
}

func (p *parser[S]) symbol() (S, error) {
	token, err := p.stringLiteral()
	if err == nil || !errors.Is(err, errNoMatch) {
		return token, err
	}
	token, err = p.rawLiteral()
	if err == nil || !errors.Is(err, errNoMatch) {
		return token, err
	}
	return p.identifier()
}

func (p *parser[S]) identifier() (S, error) {
	r, size := decodeRune(p.s)
	if !IsIdentifierRune(r) {
		return *new(S), errNoMatch
	}
	i := size

	for {
		r, size := decodeRune(p.s[i:])
		if size == 0 {
			return p.advance(i), nil
		}
		switch {
		case r == '.' || r == '+':
			r, size2 := decodeRune(p.s[i+size:])
			if !IsIdentifierRune(r) {
				return p.advance(i), nil
			}
			i += size + size2
		case r == '-':
			for {
				r, size2 := decodeRune(p.s[i+size:])
				size += size2
				if IsIdentifierRune(r) {
					i += size
					break
				}
				if r != '-' {
					return p.advance(i), nil
				}
			}
		case !IsIdentifierRune(r):
			return p.advance(i), nil
		default:
			i += size
		}
	}
}

func (p *parser[S]) rawLiteral() (S, error) {
	r, size := decodeRune(p.s)
	if r != '\'' {
		return *new(S), errNoMatch
	}
	i := size
	for {
		r, size := decodeRune(p.s[i:])
		i += size
		if size == 0 {
			result := p.s
			p.s = *new(S)
			return result, errors.New("unterminated single-quoted string")
		}
		if r == '\'' {
			return p.advance(i), nil
		}
	}
}

func (p *parser[S]) stringLiteral() (S, error) {
	r, size := decodeRune(p.s)
	if r != '"' {
		return *new(S), errNoMatch
	}
	i := size
	for {
		r, size := decodeRune(p.s[i:])
		i += size
		switch {
		case size == 0:
			result := p.s
			p.s = *new(S)
			return result, errors.New("unterminated double-quoted string")
		case r == '\\':
			_, size := decodeRune(p.s[i:])
			if size == 0 {
				result := p.s
				p.s = *new(S)
				return result, errors.New("unterminated double-quoted string")
			}
			i += size
		case r == '"':
			return p.advance(i), nil
		}
	}
}

var errNoMatch = errors.New("no match")

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

func decodeRune[S ~[]byte | ~string](s S) (r rune, size int) {
	var buf [utf8.UTFMax]byte
	n := copy(buf[:], s)
	return utf8.DecodeRune(buf[:n])
}

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
