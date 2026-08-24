package schema

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math/big"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// decodeInstance turns a document's bytes into the generic JSON value the
// evaluation walk reads: nil, bool, json.Number, string, []any or
// map[string]any.
//
// UseNumber is not a detail. Without it every number arrives as a float64, and
// a twenty-digit integer is silently rounded on the way in - "type": "integer"
// would then accept a value the document does not contain, and the report
// would be about a number nobody wrote.
func decodeInstance(body []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()

	var inst any
	if err := dec.Decode(&inst); err != nil {
		// Returned verbatim: locateJSONError adds a line and a column by
		// unwrapping a *json.SyntaxError, and anything that hid the type would
		// take that away. An empty document yields io.EOF here, which is what
		// reaches the user as "EOF".
		return nil, err
	}
	// Decode stops at the end of the first value, so two documents in one file
	// would otherwise pass with the second never read. The error is a plain
	// one rather than a *json.SyntaxError because the failure has no single
	// offset to point at, and locateJSONError passes it through unchanged.
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("invalid character after top-level value")
	}
	return inst, nil
}

// jsonTypeName names the JSON type of a decoded value the way a report does.
//
// It never returns "integer": a number that is not integral is still a number,
// which is why a fractional value where the schema wants an integer reads as
// "got number, want integer".
func jsonTypeName(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case json.Number:
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return "unknown"
}

// isIntegralNumber reports whether num denotes an integer. JSON Schema asks
// about the value and not about the spelling, so 1, 1.0 and 1e2 are all
// integers and 1.5 is not; big.Rat is what keeps that true for magnitudes
// float64 cannot hold.
func isIntegralNumber(num json.Number) bool {
	r, ok := new(big.Rat).SetString(num.String())
	return ok && r.IsInt()
}

// quoteToken quotes a name or a value the way every report jjf has ever
// printed quotes it.
//
// The names in an additional-properties message come from the document and can
// contain anything at all - a quote, a tab, a newline, an apostrophe - so the
// quoting has to be reproduced rather than approximated. strconv.Quote escapes
// the string for Go, the double quotes it escaped are put back as they were,
// and the apostrophes are escaped instead, because the result is delimited
// with apostrophes.
func quoteToken(s string) string {
	q := strconv.Quote(s)
	q = strings.ReplaceAll(q, `\"`, `"`)
	q = strings.ReplaceAll(q, `'`, `\'`)
	return "'" + q[1:len(q)-1] + "'"
}

// joinQuoted renders a list of names for a message. It never sorts: the caller
// decides whether the order comes from the schema or from a sort.
func joinQuoted(names []string) string {
	quoted := make([]string, len(names))
	for i, name := range names {
		quoted[i] = quoteToken(name)
	}
	return strings.Join(quoted, ", ")
}

// pointerEscape applies RFC 6901 escaping to one JSON Pointer segment. No
// property name in this schema contains a "~" or a "/", so it never fires
// today; it is here because a segment that skipped the escaping would silently
// name a different location, and a pointer that can do that is not a pointer.
var pointerEscape = strings.NewReplacer("~", "~0", "/", "~1")

// evaluate checks v against n and appends one Issue per violation. ptr is the
// JSON Pointer of v inside the document and is "" at the root, which
// WriteReport renders as "(document root)".
//
// The keyword order below is deliberate and must not be rearranged: type,
// then enum, then $ref, then whatever applies to the value's own type.
func (n *schemaNode) evaluate(ptr string, v any, out *[]Issue) {
	if n.Type != "" {
		got := jsonTypeName(v)
		matched := n.Type == got
		if !matched && n.Type == "integer" {
			num, ok := v.(json.Number)
			matched = ok && isIntegralNumber(num)
		}
		if !matched {
			*out = append(*out, Issue{Pointer: ptr, Message: fmt.Sprintf("got %s, want %s", got, n.Type)})
			// Returning is not an optimisation. Every keyword below assumes a
			// value of the type the schema asked for, so carrying on would
			// report a minLength against a number or a pattern against an
			// array - a second line about a length nobody wrote.
			return
		}
	}

	if len(n.Enum) != 0 {
		s, ok := v.(string)
		if !ok || !slices.Contains(n.Enum, s) {
			var msg string
			if len(n.Enum) == 1 {
				msg = "value must be " + quoteToken(n.Enum[0])
			} else {
				msg = "value must be one of " + joinQuoted(n.Enum)
			}
			*out = append(*out, Issue{Pointer: ptr, Message: msg})
			return
		}
	}

	// Draft 2020-12 evaluates a $ref and the keywords beside it both. No node
	// in this schema carries anything beside a $ref, so the rest of this
	// method finds nothing to do today; honouring a sibling rather than
	// dropping it is what keeps that from becoming a silent rule if one is
	// ever added.
	if n.target != nil {
		n.target.evaluate(ptr, v, out)
	}

	switch value := v.(type) {
	case map[string]any:
		n.evaluateObject(ptr, value, out)
	case []any:
		n.evaluateArray(ptr, value, out)
	case string:
		n.evaluateString(ptr, value, out)
	case json.Number:
		n.evaluateNumber(ptr, value, out)
	}
}

// evaluateObject checks the keywords that apply to an object.
func (n *schemaNode) evaluateObject(ptr string, obj map[string]any, out *[]Issue) {
	var missing []string
	for _, name := range n.Required {
		if _, ok := obj[name]; !ok {
			missing = append(missing, name)
		}
	}
	switch len(missing) {
	case 0:
	case 1:
		*out = append(*out, Issue{Pointer: ptr, Message: "missing property " + quoteToken(missing[0])})
	default:
		*out = append(*out, Issue{Pointer: ptr, Message: "missing properties " + joinQuoted(missing)})
	}

	// Go randomises map iteration, so a walk in iteration order would list
	// unknown properties differently on every run and the report would stop
	// being reproducible. Sorting the instance's keys here is what fixes both
	// the recursion order and the order the names come out in below.
	var unknown []string
	for _, name := range slices.Sorted(maps.Keys(obj)) {
		sub, ok := n.Properties[name]
		if ok {
			sub.evaluate(ptr+"/"+pointerEscape.Replace(name), obj[name], out)
			continue
		}
		if n.AdditionalProperties != nil && !*n.AdditionalProperties {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) != 0 {
		*out = append(*out, Issue{Pointer: ptr, Message: "additional properties " + joinQuoted(unknown) + " not allowed"})
	}

	for _, key := range slices.Sorted(maps.Keys(n.DependentRequired)) {
		if _, ok := obj[key]; !ok {
			continue
		}
		var required []string
		for _, name := range n.DependentRequired[key] {
			if _, ok := obj[name]; !ok {
				required = append(required, name)
			}
		}
		if len(required) != 0 {
			msg := fmt.Sprintf("properties %s required, if %s exists", joinQuoted(required), quoteToken(key))
			*out = append(*out, Issue{Pointer: ptr, Message: msg})
		}
	}
}

// evaluateArray checks the keywords that apply to an array.
func (n *schemaNode) evaluateArray(ptr string, arr []any, out *[]Issue) {
	if n.MinItems != nil && len(arr) < *n.MinItems {
		msg := fmt.Sprintf("minItems: got %d, want %d", len(arr), *n.MinItems)
		*out = append(*out, Issue{Pointer: ptr, Message: msg})
	}

	if n.UniqueItems {
		if i, j, ok := firstDuplicate(arr); ok {
			*out = append(*out, Issue{Pointer: ptr, Message: fmt.Sprintf("items at %d and %d are equal", i, j)})
		}
	}

	if n.Items != nil {
		for i, item := range arr {
			n.Items.evaluate(ptr+"/"+strconv.Itoa(i), item, out)
		}
	}
}

// evaluateString checks the keywords that apply to a string.
func (n *schemaNode) evaluateString(ptr string, s string, out *[]Issue) {
	if n.MinLength != nil || n.MaxLength != nil {
		// Code points, not bytes. The design documents this validates are full
		// of Japanese, where "顧客ID" is four characters and eight bytes, and a
		// bound counted in bytes would refuse names nobody wrote too long.
		length := utf8.RuneCountInString(s)
		if n.MinLength != nil && length < *n.MinLength {
			msg := fmt.Sprintf("minLength: got %d, want %d", length, *n.MinLength)
			*out = append(*out, Issue{Pointer: ptr, Message: msg})
		}
		if n.MaxLength != nil && length > *n.MaxLength {
			msg := fmt.Sprintf("maxLength: got %d, want %d", length, *n.MaxLength)
			*out = append(*out, Issue{Pointer: ptr, Message: msg})
		}
	}

	// Neither operand may go through %q or through quoteToken. %q would print
	// ^[0-9]+\.[0-9]+$ with a doubled backslash, which is a different regular
	// expression, and a reader comparing the message against the schema would
	// be comparing it against something the schema does not say.
	if n.re != nil && !n.re.MatchString(s) {
		msg := fmt.Sprintf("'%s' does not match pattern '%s'", s, n.Pattern)
		*out = append(*out, Issue{Pointer: ptr, Message: msg})
	}
}

// evaluateNumber checks the keywords that apply to a number.
func (n *schemaNode) evaluateNumber(ptr string, num json.Number, out *[]Issue) {
	if n.minimum == nil {
		return
	}
	got, ok := new(big.Rat).SetString(num.String())
	if !ok || got.Cmp(n.minimum) >= 0 {
		return
	}
	// The comparison is exact and the message is not: both operands are
	// rendered through Float64 so that the bounds this schema uses print as 0,
	// 1 and -1 rather than as the rationals they are compared as.
	gotF, _ := got.Float64()
	wantF, _ := n.minimum.Float64()
	*out = append(*out, Issue{Pointer: ptr, Message: fmt.Sprintf("minimum: got %v, want %v", gotF, wantF)})
}

// firstDuplicate returns the first pair of equal elements, smaller index
// first, scanning i upwards and j from the start so that the pair reported is
// the earliest one an eye reading the array would find. Column lists are a
// handful of names long, so the quadratic scan is cheaper than hashing them.
func firstDuplicate(arr []any) (int, int, bool) {
	for i := 1; i < len(arr); i++ {
		for j := 0; j < i; j++ {
			if jsonEqual(arr[j], arr[i]) {
				return j, i, true
			}
		}
	}
	return 0, 0, false
}

// jsonEqual compares two decoded JSON values the way JSON Schema means equal.
//
// reflect.DeepEqual is not used, and not only because AGENTS.md wants a reason
// before reflection: it would compare json.Number as text and so call 1 and
// 1.0 different, where JSON Schema calls them the same number.
func jsonEqual(a, b any) bool {
	switch av := a.(type) {
	case nil:
		return b == nil
	case bool:
		bv, ok := b.(bool)
		return ok && av == bv
	case string:
		bv, ok := b.(string)
		return ok && av == bv
	case json.Number:
		bv, ok := b.(json.Number)
		if !ok {
			return false
		}
		ar, aok := new(big.Rat).SetString(av.String())
		br, bok := new(big.Rat).SetString(bv.String())
		if !aok || !bok {
			return av == bv
		}
		return ar.Cmp(br) == 0
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !jsonEqual(av[i], bv[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for name, value := range av {
			other, ok := bv[name]
			if !ok || !jsonEqual(value, other) {
				return false
			}
		}
		return true
	}
	return false
}
