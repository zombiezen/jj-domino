package jujutsu

import (
	"bytes"
	"encoding"
	"encoding/hex"
	"fmt"
	"slices"
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
} = ChangeID(nil)

var _ interface {
	encoding.TextUnmarshaler
	encoding.BinaryUnmarshaler
} = (*ChangeID)(nil)

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
	newBuffer := slices.Grow(*id, n)[:n]
	if _, err := decodeReverseHex(newBuffer, text); err != nil {
		return fmt.Errorf("unmarshal change ID: %v", err)
	}
	*id = newBuffer
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
} = CommitID(nil)

var _ interface {
	encoding.TextUnmarshaler
	encoding.BinaryUnmarshaler
} = (*CommitID)(nil)

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
	newBuffer := slices.Grow(*id, n)[:n]
	if _, err := hex.Decode(newBuffer, text); err != nil {
		return fmt.Errorf("unmarshal commit ID: %v", err)
	}
	*id = newBuffer
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
