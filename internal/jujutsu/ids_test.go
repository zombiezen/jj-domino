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
