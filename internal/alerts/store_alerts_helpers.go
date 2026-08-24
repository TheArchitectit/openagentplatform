package alerts

import (
	"encoding/json"
	"reflect"
	"time"
)

// jsonOrNull marshals v to JSON, or returns nil if v is nil or a nil-valued
// slice/map/ptr (so the caller binds a SQL NULL instead of the literal JSON
// string "null").
func jsonOrNull(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map, reflect.Ptr, reflect.Interface:
		if rv.IsNil() {
			return nil, nil
		}
	}
	return json.Marshal(v)
}

// nullIfEmpty returns nil for an empty string so callers can bind a NULL
// column rather than an empty-string match.
func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// tzOrUTC returns s unchanged when it is a non-empty, loadable IANA
// timezone; otherwise it returns "UTC". Used to fail-closed at write time
// for alert-suppression-window timezone validation.
func tzOrUTC(s string) string {
	if s == "" {
		return "UTC"
	}
	if _, err := time.LoadLocation(s); err != nil {
		return "UTC"
	}
	return s
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
