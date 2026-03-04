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
