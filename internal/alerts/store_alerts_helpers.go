package alerts

import (
	"encoding/json"
)

// jsonOrNull marshals v to JSON, or returns nil if v is empty.
func jsonOrNull(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	return json.Marshal(v)
}

// joinAnd joins SQL fragments with " AND ".
func joinAnd(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += " AND "
		}
		out += p
	}
	return out
}
