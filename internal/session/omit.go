package session

import (
	"encoding/json"
	"strings"
)

// structuralKeys are the only keys whose string values --omit-result-text keeps.
// The rule is fail-closed: every other string is payload, including strings under
// keys nobody anticipated, because a recorder that guesses which fields are safe is
// exactly the failure this mode exists to prevent. Numbers, booleans and null are
// kept as they are: they are counts, flags and codes, and blanking them would
// destroy the shape the mode promises to keep.
var structuralKeys = map[string]bool{
	"type": true, "mimetype": true, "uri": true, "name": true, "role": true,
	"code": true, "id": true, "ids": true, "iserror": true, "nextcursor": true,
	"parent_seq": true, "seq": true,
}

// numericStructuralKeys are counts and measurements that describe the shape of a
// result without being its content. A number under any other key is treated as
// payload: an account id or a PIN is a number too.
var numericStructuralKeys = map[string]bool{
	"tokens": true, "size": true, "bytes": true, "count": true, "length": true,
	"duration": true, "durationms": true, "ms": true,
}

// OmitResultText replaces the payload of a tool result with Omitted, leaving the
// envelope (content types, ids, is_error, error codes) intact.
//
// This is the escape hatch for content that cannot be safely redacted: free-text
// prose, malformed JSON, or anything larger than the redactor's bounds. It runs
// before hashing, like every other transformation here — the recording holds the
// omitted body, so replay reproduces what was recorded rather than pretending to
// know the original.
//
// A payload that does not parse is replaced wholesale by an object carrying the
// marker: recording unparseable bytes verbatim is exactly what this mode exists to
// avoid.
func OmitResultText(raw json.RawMessage) json.RawMessage {
	fallback := func() json.RawMessage {
		b, _ := json.Marshal(map[string]any{"omitted": Omitted, "unparseable": true})
		return b
	}
	if len(raw) == 0 {
		return fallback()
	}
	var v any
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return fallback()
	}
	b, err := marshalCanonical(omitValue(v))
	if err != nil {
		return fallback()
	}
	return b
}

// omitValue walks a decoded result, blanking payload and keeping structure. A bare
// string result has nowhere to keep structure, so it is payload.
func omitValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			key := strings.ToLower(k)
			if structuralKeys[key] {
				switch val.(type) {
				case map[string]any, []any:
					out[k] = omitValue(val)
				default:
					out[k] = val // type, code, id, isError, and their scalar cousins
				}
				continue
			}
			// Fail-closed: under a key the mode does not recognise as structure, a
			// scalar is payload whatever its type (a number can be an account id or a
			// PIN). Containers are walked so nested structure still survives.
			switch val := val.(type) {
			case map[string]any, []any:
				out[k] = omitValue(val)
			case json.Number:
				if numericStructuralKeys[key] {
					out[k] = val
					continue
				}
				out[k] = Omitted
			default:
				out[k] = Omitted
			}
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, elem := range t {
			out[i] = omitValue(elem)
		}
		return out
	case string:
		return Omitted
	case json.Number, bool, nil:
		return v
	default:
		return Omitted
	}
}
