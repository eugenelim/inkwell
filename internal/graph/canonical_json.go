package graph

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// CanonicalJSON returns a byte-stable JSON encoding of v: keys in
// every nested object are sorted lexicographically, whitespace is
// minimal, and integer / float values round-trip through
// encoding/json's default precision. The encoding is suitable for
// content-hash comparison across two unmarshal+marshal cycles of the
// same input (spec 32 §5.4 conflict detection).
//
// Implementation: marshal v into RawMessage, then walk the JSON tree
// re-emitting each object with sorted keys. Slices and scalars are
// emitted via encoding/json's defaults.
func CanonicalJSON(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := canonicalEncode(&buf, generic); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ContentHash returns the SHA-256 of CanonicalJSON(v), hex-encoded.
// Used by the rules apply pipeline to detect server-side mutation
// between pull and PATCH (spec 32 §5.4).
func ContentHash(v any) (string, error) {
	b, err := CanonicalJSON(v)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func canonicalEncode(buf *bytes.Buffer, v any) error {
	switch x := v.(type) {
	case nil:
		buf.WriteString("null")
		return nil
	case bool, float64, json.Number, string:
		b, err := json.Marshal(x)
		if err != nil {
			return err
		}
		buf.Write(b)
		return nil
	case []any:
		buf.WriteByte('[')
		for i, item := range x {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := canonicalEncode(buf, item); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
		return nil
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			kb, err := json.Marshal(k)
			if err != nil {
				return err
			}
			buf.Write(kb)
			buf.WriteByte(':')
			if err := canonicalEncode(buf, x[k]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
		return nil
	default:
		return fmt.Errorf("canonical-json: unsupported type %T", v)
	}
}
