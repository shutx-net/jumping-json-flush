package sml

import (
	"strconv"
	"strings"
)

// sanitizeXMLText removes characters that XML 1.0 cannot represent.
// encoding/xml replaces them with U+FFFD without reporting an error, which is
// silent data loss, so strip them here rather than leaving it to the encoder.
func sanitizeXMLText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == 0x09, r == 0x0A, r == 0x0D:
			b.WriteRune(r)
		case r < 0x20:
			// Not representable in XML 1.0: drop.
		case r == 0xFFFE, r == 0xFFFF:
			// Noncharacters: drop.
		case r >= 0x20 && r <= 0xD7FF,
			r >= 0xE000 && r <= 0xFFFD,
			r >= 0x10000 && r <= 0x10FFFF:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// normalizeNewlines collapses CRLF and lone CR to LF so cell text round-trips
// without relying on Excel's _x000D_ escape convention.
func normalizeNewlines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

// needsSpacePreserve reports whether s begins or ends with whitespace, in which
// case its <t> element needs xml:space="preserve" to survive a round trip.
func needsSpacePreserve(s string) bool {
	if s == "" {
		return false
	}
	return isXMLSpace(s[0]) || isXMLSpace(s[len(s)-1])
}

// isXMLSpace reports whether b is XML whitespace. Comparing bytes is safe here
// because every XML whitespace character is ASCII.
func isXMLSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// colName converts a 1-based column number to its A1 style name. The mapping is
// bijective base 26, not plain base 26: 1 -> A, 26 -> Z, 27 -> AA, 702 -> ZZ,
// 703 -> AAA. It returns "" for col < 1.
func colName(col int) string {
	if col < 1 {
		return ""
	}
	// The widest legal column, XFD, needs three letters; eight is ample.
	var buf [8]byte
	i := len(buf)
	for col > 0 {
		col--
		i--
		buf[i] = byte('A' + col%26)
		col /= 26
	}
	return string(buf[i:])
}

// cellRef returns the A1 style reference of a 1-based column and row.
func cellRef(col, row int) string {
	return colName(col) + strconv.Itoa(row)
}

// rangeRef returns the A1 style reference of a rectangular range. A range that
// covers a single cell collapses to a plain cell reference.
func rangeRef(col1, row1, col2, row2 int) string {
	first := cellRef(col1, row1)
	if col1 == col2 && row1 == row2 {
		return first
	}
	return first + ":" + cellRef(col2, row2)
}
