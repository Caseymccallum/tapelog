package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// canonicalJSON returns the canonical serialization of a JSON value used
// inside the hash-chain canonical form (docs/SCHEMA.md):
//   - object keys sorted lexicographically at all depths
//   - no insignificant whitespace
//   - strings encoded as JSON with HTML escaping disabled
//   - number literals preserved verbatim (json.Number)
//
// The Go implementation is the reference; any language implementing the
// same rules produces identical bytes.
func canonicalJSON(raw json.RawMessage) (string, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return "", fmt.Errorf("decode payload: %w", err)
	}
	b, err := marshalCanonical(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// marshalCanonical recursively serializes a decoded JSON value canonically.
func marshalCanonical(v any) ([]byte, error) {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var buf bytes.Buffer
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			kb, err := marshalCanonicalString(k)
			if err != nil {
				return nil, err
			}
			buf.Write(kb)
			buf.WriteByte(':')
			vb, err := marshalCanonical(t[k])
			if err != nil {
				return nil, err
			}
			buf.Write(vb)
		}
		buf.WriteByte('}')
		return buf.Bytes(), nil
	case []any:
		var buf bytes.Buffer
		buf.WriteByte('[')
		for i, elem := range t {
			if i > 0 {
				buf.WriteByte(',')
			}
			eb, err := marshalCanonical(elem)
			if err != nil {
				return nil, err
			}
			buf.Write(eb)
		}
		buf.WriteByte(']')
		return buf.Bytes(), nil
	case json.Number:
		return []byte(t.String()), nil
	case string:
		return marshalCanonicalString(t)
	case bool:
		if t {
			return []byte("true"), nil
		}
		return []byte("false"), nil
	case nil:
		return []byte("null"), nil
	default:
		return nil, fmt.Errorf("unsupported JSON value type %T", v)
	}
}

// marshalCanonicalString encodes a JSON string with HTML escaping disabled
// so the canonical form is stable and portable.
func marshalCanonicalString(s string) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return nil, err
	}
	return []byte(strings.TrimRight(buf.String(), "\n")), nil
}

// CanonicalJSON is exported for tests and tooling that need the same
// canonical serialization used in the hash chain.
func CanonicalJSON(raw json.RawMessage) (string, error) {
	return canonicalJSON(raw)
}
