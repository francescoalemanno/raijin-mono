package llm

import "charm.land/fantasy/schema"

// Schema is a JSON schema definition used in tool metadata.
type Schema = schema.Schema

// SchemaToMap converts a schema into a map representation.
func SchemaToMap(s Schema) map[string]any {
	return schema.ToMap(s)
}
