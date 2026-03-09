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
	"bytes"
	"encoding"
	"encoding/hex"
	"fmt"
	"slices"

	jsonv2 "github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
)

// ChangeID holds a [change ID].
//
// [change ID]: https://www.jj-vcs.dev/latest/templates/#changeid-type
type ChangeID []byte

var _ interface {
	encoding.TextMarshaler
	encoding.TextAppender
	encoding.BinaryMarshaler
	encoding.BinaryAppender
	jsonv2.MarshalerTo
} = ChangeID(nil)

var _ interface {
	encoding.TextUnmarshaler
	encoding.BinaryUnmarshaler
	jsonv2.UnmarshalerFrom
} = (*ChangeID)(nil)

// IsZero reports whether the length of id is zero.
func (id ChangeID) IsZero() bool {
	return len(id) == 0
}

// String returns the change ID in lowercase "reverse" hex.
func (id ChangeID) String() string {
	return string(appendReverseHex(nil, id))
}

// Equal reports whether id and id2 are the same length and contain the same bytes.
func (id ChangeID) Equal(id2 ChangeID) bool {
	return bytes.Equal(id, id2)
}

// Short returns the first 8 characters of the change ID in lowercase "reverse" hex.
func (id ChangeID) Short() string {
	return string(appendReverseHex(nil, id[:min(4, len(id))]))
}

// MarshalText implements [encoding.TextMarshaler]
// by returning the lowercase "reverse" hexadecimal encoding of the ID.
// "Reverse" hexadecimal uses the letters z-k to represent 0-9a-f.
func (id ChangeID) MarshalText() ([]byte, error) {
	return id.AppendText(nil)
}

// AppendText implements [encoding.TextAppender]
// by appending the lowercase "reverse" hexadecimal encoding of the ID.
// "Reverse" hexadecimal uses the letters z-k to represent 0-9a-f.
func (id ChangeID) AppendText(dst []byte) ([]byte, error) {
	return appendReverseHex(dst, id), nil
}

// UnmarshalText implements [encoding.TextUnmarshaler]
// by decoding a "reverse" hexadecimal string.
// "Reverse" hexadecimal uses the letters z-k to represent 0-9a-f.
func (id *ChangeID) UnmarshalText(text []byte) error {
	n := hex.DecodedLen(len(text))
	newBuffer := slices.Grow((*id)[:0], n)[:n]
	if _, err := decodeReverseHex(newBuffer, text); err != nil {
		return fmt.Errorf("unmarshal change ID: %v", err)
	}
	*id = newBuffer
	return nil
}

// MarshalJSONTo implements [jsonv2.MarshalerTo]
// by writing a string with the lowercase "reverse" hexadecimal encoding of the ID
// or null if the id is empty.
// "Reverse" hexadecimal uses the letters z-k to represent 0-9a-f.
func (id ChangeID) MarshalJSONTo(enc *jsontext.Encoder) error {
	if len(id) == 0 {
		return enc.WriteToken(jsontext.Null)
	}
	return enc.WriteToken(jsontext.String(id.String()))
}

// UnmarshalJSONFrom implements [jsonv2.UnmarshalerFrom]
// by decoding a "reverse" hexadecimal encoding of the ID.
// "Reverse" hexadecimal uses the letters z-k to represent 0-9a-f.
// UnmarshalJSONFrom will also set id to nil from a null.
func (id *ChangeID) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	switch dec.PeekKind() {
	case jsontext.KindNull:
		*id = nil
		// Consume null token. Should never error.
		if _, err := dec.ReadToken(); err != nil {
			return fmt.Errorf("unmarshal change ID: %v", err)
		}
	case jsontext.KindString:
		tok, err := dec.ReadToken()
		if err != nil {
			return err
		}
		text := tok.String()
		n := hex.DecodedLen(len(text))
		newBuffer := slices.Grow((*id)[:0], n)[:n]
		if _, err := decodeReverseHex(newBuffer, text); err != nil {
			return fmt.Errorf("unmarshal change ID: %v", err)
		}
		*id = newBuffer
	default:
		return fmt.Errorf("unmarshal change ID: must be a string or null")
	}
	return nil
}

// MarshalBinary implements [encoding.BinaryMarshaler]
// by returning a copy of id.
func (id ChangeID) MarshalBinary() ([]byte, error) {
	return id.AppendBinary(nil)
}

// AppendBinary implements [encoding.BinaryAppender]
// by appending the ID to dst.
func (id ChangeID) AppendBinary(dst []byte) ([]byte, error) {
	return append(dst, id...), nil
}

// UnmarshalBinary implements [encoding.TextUnmarshaler]
// by copying the data to id.
func (id *ChangeID) UnmarshalBinary(data []byte) error {
	*id = append((*id)[:0], data...)
	return nil
}

// CommitID holds a [commit ID].
// For Git-backed repositories, this is the same as the Git commit hash.
//
// [commit ID]: https://www.jj-vcs.dev/latest/glossary/#commit-id
type CommitID []byte

var _ interface {
	encoding.TextMarshaler
	encoding.TextAppender
	encoding.BinaryMarshaler
	encoding.BinaryAppender
	jsonv2.MarshalerTo
} = CommitID(nil)

var _ interface {
	encoding.TextUnmarshaler
	encoding.BinaryUnmarshaler
	jsonv2.UnmarshalerFrom
} = (*CommitID)(nil)

// IsZero reports whether the length of id is zero.
func (id CommitID) IsZero() bool {
	return len(id) == 0
}

// String returns the commit ID in lowercase hex.
func (id CommitID) String() string {
	return hex.EncodeToString(id)
}

// Equal reports whether id and id2 are the same length and contain the same bytes.
func (id CommitID) Equal(id2 CommitID) bool {
	return bytes.Equal(id, id2)
}

// MarshalText implements [encoding.TextMarshaler]
// by returning the lowercase hexadecimal encoding of the ID.
func (id CommitID) MarshalText() ([]byte, error) {
	return id.AppendText(nil)
}

// AppendText implements [encoding.TextAppender]
// by appending the lowercase hexadecimal encoding of the ID.
func (id CommitID) AppendText(dst []byte) ([]byte, error) {
	return hex.AppendEncode(dst, id), nil
}

// UnmarshalText implements [encoding.TextUnmarshaler]
// by decoding a hexadecimal string.
func (id *CommitID) UnmarshalText(text []byte) error {
	n := hex.DecodedLen(len(text))
	newBuffer := slices.Grow((*id)[:0], n)[:n]
	if _, err := hex.Decode(newBuffer, text); err != nil {
		return fmt.Errorf("unmarshal commit ID: %v", err)
	}
	*id = newBuffer
	return nil
}

// MarshalJSONTo implements [jsonv2.MarshalerTo]
// by writing a string with the lowercase hexadecimal encoding of the ID
// or null if the id is empty.
func (id CommitID) MarshalJSONTo(enc *jsontext.Encoder) error {
	if len(id) == 0 {
		return enc.WriteToken(jsontext.Null)
	}
	return enc.WriteToken(jsontext.String(id.String()))
}

// UnmarshalJSONFrom implements [jsonv2.UnmarshalerFrom]
// by decoding a hexadecimal string.
// UnmarshalJSONFrom will also set id to nil from a null.
func (id *CommitID) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	switch dec.PeekKind() {
	case jsontext.KindNull:
		*id = nil
		// Consume null token. Should never error.
		if _, err := dec.ReadToken(); err != nil {
			return fmt.Errorf("unmarshal commit ID: %v", err)
		}
	case jsontext.KindString:
		tok, err := dec.ReadToken()
		if err != nil {
			return err
		}
		text := tok.String()
		n := hex.DecodedLen(len(text))
		newBuffer := slices.Grow((*id)[:0], n)[:n]
		if _, err := hex.Decode(newBuffer, []byte(text)); err != nil {
			return fmt.Errorf("unmarshal commit ID: %v", err)
		}
		*id = newBuffer
	default:
		return fmt.Errorf("unmarshal commit ID: must be a string or null")
	}
	return nil
}

// MarshalBinary implements [encoding.BinaryMarshaler]
// by returning a copy of id.
func (id CommitID) MarshalBinary() ([]byte, error) {
	return id.AppendBinary(nil)
}

// AppendBinary implements [encoding.BinaryAppender]
// by appending the ID to dst.
func (id CommitID) AppendBinary(dst []byte) ([]byte, error) {
	return append(dst, id...), nil
}

// UnmarshalBinary implements [encoding.TextUnmarshaler]
// by copying the data to id.
func (id *CommitID) UnmarshalBinary(data []byte) error {
	*id = append((*id)[:0], data...)
	return nil
}

const (
	reverseHexTable        = "zyxwvutsrqponmlk"
	reverseReverseHexTable = "" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\x0f\x0e\x0d\x0c\x0b" +
		"\x0a\x09\x08\x07\x06\x05\x04\x03\x02\x01\x00\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\x0f\x0e\x0d\x0c\x0b" +
		"\x0a\x09\x08\x07\x06\x05\x04\x03\x02\x01\x00\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff" +
		"\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff"
)

func appendReverseHex(dst, src []byte) []byte {
	n := hex.EncodedLen(len(src))
	dst = slices.Grow(dst, n)
	for _, v := range src {
		dst = append(dst, reverseHexTable[v>>4], reverseHexTable[v&0x0f])
	}
	return dst
}

func decodeReverseHex[S ~[]byte | ~string](dst []byte, src S) (int, error) {
	i, j := 0, 1
	for ; j < len(src); j += 2 {
		p := src[j-1]
		q := src[j]

		a := reverseReverseHexTable[p]
		b := reverseReverseHexTable[q]
		if a > 0x0f {
			return i, hex.InvalidByteError(p)
		}
		if b > 0x0f {
			return i, hex.InvalidByteError(q)
		}
		dst[i] = (a << 4) | b
		i++
	}
	if len(src)%2 == 1 {
		// Check for invalid char before reporting bad length,
		// since the invalid char (if present) is an earlier problem.
		if reverseReverseHexTable[src[j-1]] > 0x0f {
			return i, hex.InvalidByteError(src[j-1])
		}
		return i, hex.ErrLength
	}
	return i, nil

}
