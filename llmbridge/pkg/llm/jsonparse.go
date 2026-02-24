package llm

import (
	"encoding/json"
	"fmt"

	"charm.land/fantasy/schema"
)

// ParseJSONInput parses potentially malformed LLM-generated JSON into dst,
// using schema.ParsePartialJSON to repair the input before unmarshalling.
func ParseJSONInput(input []byte, dst any) error {
	parsed, _, err := schema.ParsePartialJSON(string(input))
	if err != nil {
		return fmt.Errorf("json repair failed: %w", err)
	}
	data, err := json.Marshal(parsed)
	if err != nil {
		return fmt.Errorf("json re-marshal failed: %w", err)
	}
	return json.Unmarshal(data, dst)
}
