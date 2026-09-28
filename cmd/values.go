package cmd

import (
	"encoding/json"
	"fmt"
	"strings"
)

// BuildBody converts key=value pairs into a JSON object. Each value is parsed
// as JSON when valid (numbers, booleans, null, arrays, objects, quoted
// strings); otherwise it is kept as a string. Dotted keys build nested
// objects. Later pairs win for duplicate keys.
func BuildBody(pairs []string) ([]byte, error) {
	body := make(map[string]any)
	for _, pair := range pairs {
		idx := strings.IndexByte(pair, '=')
		if idx < 0 {
			return nil, fmt.Errorf("invalid pair %q: expected key=value", pair)
		}
		key := pair[:idx]
		raw := pair[idx+1:]
		setNested(body, strings.Split(key, "."), parseValue(raw))
	}
	return json.Marshal(body)
}

func parseValue(raw string) any {
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err == nil {
		return v
	}
	return raw
}

func setNested(m map[string]any, keys []string, val any) {
	if len(keys) == 1 {
		m[keys[0]] = val
		return
	}
	sub, ok := m[keys[0]].(map[string]any)
	if !ok {
		sub = make(map[string]any)
		m[keys[0]] = sub
	}
	setNested(sub, keys[1:], val)
}
