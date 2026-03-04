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
