package graph

import (
	"encoding/json"
	"fmt"
)

// MergeRuleSubObject deep-merges two JSON objects at the top level
// (one nesting level), with the "edit" side winning per-key. The
// result is a json.RawMessage suitable for splicing into a PATCH
// body for /me/mailFolders/inbox/messageRules/{id}.
//
// Spec 32 §5.3: Microsoft Graph PATCH on messageRule sub-objects
// (`conditions`, `actions`, `exceptions`) is documented as
// replace-only at the sub-object level — sending a partial
// `conditions` body can wipe other predicate fields. To preserve
// non-v1 keys the prior version of the resource had, we merge them
// in client-side before issuing the PATCH.
//
// Behaviour:
//   - Both nil or empty → returns `{}`.
//   - Only one side present → returns that side verbatim.
//   - Both present → keys from `edit` overwrite keys from `prior`;
//     keys only in `prior` are preserved; arrays / nested objects
//     are replaced wholesale (matching Graph's own PATCH semantics).
func MergeRuleSubObject(prior, edit json.RawMessage) (json.RawMessage, error) {
	priorMap, err := unmarshalObject(prior)
	if err != nil {
		return nil, fmt.Errorf("merge: prior: %w", err)
	}
	editMap, err := unmarshalObject(edit)
	if err != nil {
		return nil, fmt.Errorf("merge: edit: %w", err)
	}
	if priorMap == nil && editMap == nil {
		return json.RawMessage(`{}`), nil
	}
	if priorMap == nil {
		return marshalObject(editMap)
	}
	if editMap == nil {
		return marshalObject(priorMap)
	}
	merged := make(map[string]json.RawMessage, len(priorMap)+len(editMap))
	for k, v := range priorMap {
		merged[k] = v
	}
	for k, v := range editMap {
		merged[k] = v
	}
	return marshalObject(merged)
}

func unmarshalObject(b json.RawMessage) (map[string]json.RawMessage, error) {
	if len(b) == 0 {
		return nil, nil
	}
	// `null` is treated as absent (Graph occasionally sends it for
	// empty sub-objects). All other non-object values are a server
	// bug or caller error and we fail loudly.
	var probe any
	if err := json.Unmarshal(b, &probe); err != nil {
		return nil, err
	}
	if probe == nil {
		return nil, nil
	}
	out, ok := probe.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected JSON object, got %T", probe)
	}
	// Re-decode into json.RawMessage values to preserve bytes.
	_ = out
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func marshalObject(m map[string]json.RawMessage) (json.RawMessage, error) {
	if len(m) == 0 {
		return json.RawMessage(`{}`), nil
	}
	// encoding/json sorts map keys for deterministic output —
	// relied on by the canonical-JSON helper for content-hash
	// stability.
	return json.Marshal(m)
}
