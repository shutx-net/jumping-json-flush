package sml

import (
	"strconv"
	"strings"
)

// invalidSheetChars lists the characters Excel rejects in a sheet name.
const invalidSheetChars = `[]:*?/\`

// maxSheetNameLen is the longest sheet name Excel accepts.
const maxSheetNameLen = 31

// sanitizeSheetName rewrites a name into a form Excel accepts: at most 31
// characters, none of them from invalidSheetChars, and no leading or trailing
// apostrophe. None of these limits are expressed in the OOXML schema, so
// nothing but Excel itself will complain about a violation.
func sanitizeSheetName(name string) string {
	out := make([]rune, 0, len(name))
	for _, r := range name {
		if strings.ContainsRune(invalidSheetChars, r) || r < 0x20 {
			r = '_'
		}
		out = append(out, r)
	}
	s := strings.Trim(string(out), "'")
	if s == "" {
		s = "Sheet"
	}
	// Truncate by rune, not by byte, so multi-byte names are not cut in half.
	if r := []rune(s); len(r) > maxSheetNameLen {
		s = string(r[:maxSheetNameLen])
	}
	return s
}

// nameAllocator hands out unique sheet names. Excel compares sheet names
// case-insensitively, so uniqueness is tracked on the lowercased form.
type nameAllocator struct {
	used map[string]bool
}

// Allocate returns a sanitized name that no earlier call returned. Collisions
// get a "(n)" suffix, truncating the base name if the suffix would not fit.
func (a *nameAllocator) Allocate(base string) string {
	if a.used == nil {
		a.used = make(map[string]bool)
	}
	s := sanitizeSheetName(base)
	if key := strings.ToLower(s); !a.used[key] {
		a.used[key] = true
		return s
	}
	for n := 2; ; n++ {
		suffix := "(" + strconv.Itoa(n) + ")"
		r := []rune(s)
		keep := len(r)
		if keep+len([]rune(suffix)) > maxSheetNameLen {
			keep = maxSheetNameLen - len([]rune(suffix))
		}
		cand := string(r[:keep]) + suffix
		if key := strings.ToLower(cand); !a.used[key] {
			a.used[key] = true
			return cand
		}
	}
}
