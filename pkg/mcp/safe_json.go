package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
)

var unsafeJSONKeys = map[string]struct{}{
	"__proto__":   {},
	"constructor": {},
	"prototype":   {},
}

func unmarshalSafeJSON(data []byte, target interface{}) error {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}

	var raw interface{}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&raw); err != nil {
		return err
	}
	if err := rejectUnsafeJSONKeys(raw); err != nil {
		return err
	}

	return json.Unmarshal(data, target)
}

func rejectUnsafeJSONKeys(value interface{}) error {
	switch v := value.(type) {
	case map[string]interface{}:
		for key, nested := range v {
			if _, unsafe := unsafeJSONKeys[key]; unsafe {
				return fmt.Errorf("unsafe JSON object key %q", key)
			}
			if err := rejectUnsafeJSONKeys(nested); err != nil {
				return err
			}
		}
	case []interface{}:
		for _, nested := range v {
			if err := rejectUnsafeJSONKeys(nested); err != nil {
				return err
			}
		}
	}
	return nil
}
