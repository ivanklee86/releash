package variant

import (
	"bytes"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// flatten parses a json payload and returns it as compact JSON, with one
// variable per leaf sorted by name. Leaf names are relative to the payload
// variable: "__IMAGE__TAG" for {"image": {"tag": ...}}. A scalar root has no
// leaves. variant names the variant in errors, which all match ErrUnusable.
func flatten(variant, payload string) (string, []variable, error) {
	var compact bytes.Buffer
	// Compact keeps number text, member order and string escapes as written,
	// and rejects anything that is not exactly one JSON value.
	if err := json.Compact(&compact, []byte(payload)); err != nil {
		return "", nil, unusable("payload of variant %q is not valid JSON: %v", variant, err)
	}
	if err := checkText(variant, payload); err != nil {
		return "", nil, err
	}

	dec := json.NewDecoder(strings.NewReader(payload))
	dec.UseNumber()
	f := &flattener{dec: dec, variant: variant, paths: map[string]string{}}
	if err := f.value("", ""); err != nil {
		return "", nil, err
	}
	slices.SortFunc(f.leaves, func(a, b variable) int { return strings.Compare(a.name, b.name) })
	return compact.String(), f.leaves, nil
}

// checkText rejects text that encoding/json would silently replace with
// U+FFFD when it decodes a string: invalid UTF-8, and a \u escape of a UTF-16
// surrogate that is not part of a pair. Without it a leaf, or a member name,
// would differ from what PAYLOAD shows. payload must be valid JSON, so every
// backslash starts an escape inside a string and every \u has four hex digits.
func checkText(variant, payload string) error {
	if !utf8.ValidString(payload) {
		return unusable("payload of variant %q is not valid UTF-8", variant)
	}
	for i := 0; i < len(payload); i++ {
		if payload[i] != '\\' {
			continue
		}
		i++
		if payload[i] != 'u' {
			continue
		}
		escape := payload[i-1 : i+5]
		r := hex4(payload[i+1 : i+5])
		i += 4
		if !utf16.IsSurrogate(r) {
			continue
		}
		if r < 0xDC00 && strings.HasPrefix(payload[i+1:], `\u`) {
			if low := hex4(payload[i+3 : i+7]); low >= 0xDC00 && low <= 0xDFFF {
				i += 6
				continue
			}
		}
		return unusable("payload of variant %q has an unpaired surrogate escape %s", variant, escape)
	}
	return nil
}

// hex4 returns the code unit written as four hex digits in s.
func hex4(s string) rune {
	n, _ := strconv.ParseUint(s, 16, 16)
	return rune(n)
}

// flattener walks a payload that json.Compact has already accepted, token by
// token, so that it sees duplicate member names and the literal text of
// numbers.
type flattener struct {
	dec     *json.Decoder
	variant string
	// paths maps the name of every node below the root, object or array or
	// leaf, to the path of the first node that had it.
	paths  map[string]string
	leaves []variable
}

// value reads the value at path, whose variable name is name, and everything
// below it. The root has the empty name and path.
func (f *flattener) value(name, path string) error {
	tok, err := f.dec.Token()
	if err != nil {
		return unusable("payload of variant %q is not valid JSON: %v", f.variant, err)
	}
	switch t := tok.(type) {
	case json.Delim:
		if t == '{' {
			return f.object(name, path)
		}
		return f.array(name, path)
	case string:
		f.leaf(name, t)
	case json.Number:
		f.leaf(name, string(t))
	case bool:
		f.leaf(name, strconv.FormatBool(t))
	case nil:
		f.leaf(name, "")
	}
	return nil
}

// object reads the members of an object whose '{' has been read, and its '}'.
func (f *flattener) object(name, path string) error {
	seen := map[string]bool{}
	for f.dec.More() {
		tok, err := f.dec.Token()
		if err != nil {
			return unusable("payload of variant %q is not valid JSON: %v", f.variant, err)
		}
		key, _ := tok.(string)
		childPath := joinPath(path, key)
		if seen[key] {
			return unusable("payload of variant %q has duplicate key %q", f.variant, childPath)
		}
		seen[key] = true
		if err := f.child(name+"__"+normalize(key), childPath); err != nil {
			return err
		}
	}
	return f.end()
}

// array reads the elements of an array whose '[' has been read, and its ']'.
func (f *flattener) array(name, path string) error {
	for i := 0; f.dec.More(); i++ {
		index := strconv.Itoa(i)
		if err := f.child(name+"__"+index, joinPath(path, index)); err != nil {
			return err
		}
	}
	return f.end()
}

// child claims name for the node at path and reads it. Two nodes with the
// same name collide even if neither is a leaf, e.g. {"a-b": {"c": 1}, "a_b":
// {"d": 2}}, because their names no longer say which object a variable came
// from.
func (f *flattener) child(name, path string) error {
	if first, ok := f.paths[name]; ok {
		return unusable("payload of variant %q has colliding keys %q and %q", f.variant, first, path)
	}
	f.paths[name] = path
	return f.value(name, path)
}

// end reads the delimiter that closes an object or array.
func (f *flattener) end() error {
	if _, err := f.dec.Token(); err != nil {
		return unusable("payload of variant %q is not valid JSON: %v", f.variant, err)
	}
	return nil
}

// leaf records a scalar. A scalar root is only visible in PAYLOAD.
func (f *flattener) leaf(name, value string) {
	if name == "" {
		return
	}
	f.leaves = append(f.leaves, variable{name: name, value: value})
}

// joinPath appends a member name or index to a path for error messages. Paths
// are JSON Pointers (RFC 6901) without the leading "/", so a top-level member
// reads as its own name and "/" in a name is written "~1".
func joinPath(path, segment string) string {
	segment = strings.ReplaceAll(segment, "~", "~0")
	segment = strings.ReplaceAll(segment, "/", "~1")
	if path == "" {
		return segment
	}
	return path + "/" + segment
}
