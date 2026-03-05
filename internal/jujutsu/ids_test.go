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
	"testing"
)

var reverseHexTests = []struct {
	data   []byte
	lower  string
	others []string
}{
	{
		data:  nil,
		lower: "",
	},
	{
		data:   []byte{0xfc, 0x69, 0x07, 0x96},
		lower:  "kntqzsqt",
		others: []string{"KNTQZSQT"},
	},
	{
		data:   []byte{0xb8, 0x8f, 0xb7, 0x1b},
		lower:  "orrkosyo",
		others: []string{"ORRKOSYO"},
	},
}

func TestAppendReverseHex(t *testing.T) {
	for _, test := range reverseHexTests {
		got := appendReverseHex(nil, test.data)
		if string(got) != test.lower {
			t.Errorf("appendReverseHex(nil, %#v) = %q; want %q", test.data, got, test.lower)
		}
	}
}

func TestDecodeReverseHex(t *testing.T) {
	for _, test := range reverseHexTests {
		testDecodeReverseHex(t, test.data, []byte(test.lower))
		for _, encoded := range test.others {
			testDecodeReverseHex(t, test.data, []byte(encoded))
		}
	}
}

func testDecodeReverseHex(tb testing.TB, want []byte, encoded []byte) {
	tb.Helper()
	got := make([]byte, len(want))
	n, err := decodeReverseHex(got, encoded)
	if !bytes.Equal(got, want) || n != len(got) || err != nil {
		tb.Errorf("decodeReverseHex(buf, %q) = %d, %v, buf=%#v; want %d, <nil>, buf=%#v",
			encoded, n, err, got, len(got), want)
	}
}

func FuzzReverseHex(f *testing.F) {
	for _, test := range reverseHexTests {
		f.Add(test.data)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		encoded := appendReverseHex(nil, data)
		testDecodeReverseHex(t, data, encoded)
	})
}
