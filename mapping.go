package graphql

import (
	"encoding/json"
	"fmt"
)

// FieldType represents an Elasticsearch field type
type FieldType string

const (
	FieldTypeText     FieldType = "text"
	FieldTypeKeyword  FieldType = "keyword"
	FieldTypeLong     FieldType = "long"
	FieldTypeInteger  FieldType = "integer"
	FieldTypeShort    FieldType = "short"
	FieldTypeByte     FieldType = "byte"
	FieldTypeDouble   FieldType = "double"
	FieldTypeFloat    FieldType = "float"
	FieldTypeBoolean  FieldType = "boolean"
	FieldTypeDate     FieldType = "date"
	FieldTypeObject   FieldType = "object"
	FieldTypeNested   FieldType = "nested"
)

// Field represents a field in an Elasticsearch mapping
type Field struct {
	Name       string
	Type       FieldType
	Properties map[string]*Field
	Fields     map[string]*Field // Multi-fields (e.g., text.keyword)
}

// IndexMapping represents the parsed Elasticsearch index mapping
type IndexMapping struct {
	IndexName  string
	Properties map[string]*Field
}

// ParseMapping parses an Elasticsearch mapping JSON into an IndexMapping
func ParseMapping(indexName string, mappingJSON []byte) (*IndexMapping, error) {
	var raw map[string]any
	if err := json.Unmarshal(mappingJSON, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse mapping JSON: %w", err)
	}

	// Extract properties from the mapping structure
	// ES mappings can be in different formats:
	// - {"properties": {...}}
	// - {"mappings": {"properties": {...}}}
	// - {"index_name": {"mappings": {"properties": {...}}}}

	properties, err := extractProperties(raw)
	if err != nil {
		return nil, err
	}

	fields := make(map[string]*Field)
	for name, prop := range properties {
		field, err := parseField(name, prop)
		if err != nil {
			return nil, fmt.Errorf("failed to parse field %s: %w", name, err)
		}
		fields[name] = field
	}

	return &IndexMapping{
		IndexName:  indexName,
		Properties: fields,
	}, nil
}

// extractProperties extracts the properties map from various ES mapping formats
func extractProperties(raw map[string]any) (map[string]any, error) {
	// Try direct properties
	if props, ok := raw["properties"].(map[string]any); ok {
		return props, nil
	}

	// Try mappings.properties
	if mappings, ok := raw["mappings"].(map[string]any); ok {
		if props, ok := mappings["properties"].(map[string]any); ok {
			return props, nil
		}
	}

	// Try index_name.mappings.properties (for GET /index/_mapping response)
	for _, value := range raw {
		if indexData, ok := value.(map[string]any); ok {
			if mappings, ok := indexData["mappings"].(map[string]any); ok {
				if props, ok := mappings["properties"].(map[string]any); ok {
					return props, nil
				}
			}
		}
	}

	return nil, fmt.Errorf("could not find properties in mapping")
}

// parseField parses a single field from the mapping
func parseField(name string, raw any) (*Field, error) {
	fieldMap, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("field is not a map")
	}

	field := &Field{
		Name:       name,
		Properties: make(map[string]*Field),
		Fields:     make(map[string]*Field),
	}

	// Get field type
	if typeStr, ok := fieldMap["type"].(string); ok {
		field.Type = FieldType(typeStr)
	} else {
		// If no type specified, it's an object
		field.Type = FieldTypeObject
	}

	// Parse nested properties (for object and nested types)
	if props, ok := fieldMap["properties"].(map[string]any); ok {
		for propName, propData := range props {
			propField, err := parseField(propName, propData)
			if err != nil {
				return nil, fmt.Errorf("failed to parse property %s: %w", propName, err)
			}
			field.Properties[propName] = propField
		}
	}

	// Parse multi-fields
	if fields, ok := fieldMap["fields"].(map[string]any); ok {
		for fieldName, fieldData := range fields {
			multiField, err := parseField(fieldName, fieldData)
			if err != nil {
				return nil, fmt.Errorf("failed to parse multi-field %s: %w", fieldName, err)
			}
			field.Fields[fieldName] = multiField
		}
	}

	return field, nil
}

// GetField retrieves a field by path (e.g., "user.name" or "tags.keyword")
func (m *IndexMapping) GetField(path string) *Field {
	return getFieldByPath(m.Properties, path)
}

func getFieldByPath(properties map[string]*Field, path string) *Field {
	parts := splitPath(path)
	if len(parts) == 0 {
		return nil
	}

	field, ok := properties[parts[0]]
	if !ok {
		return nil
	}

	if len(parts) == 1 {
		return field
	}

	// Check multi-fields first (e.g., "text.keyword")
	if multiField, ok := field.Fields[parts[1]]; ok && len(parts) == 2 {
		return multiField
	}

	// Otherwise traverse properties
	return getFieldByPath(field.Properties, joinPath(parts[1:]))
}

func splitPath(path string) []string {
	var parts []string
	current := ""
	for _, char := range path {
		if char == '.' {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
		} else {
			current += string(char)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}

func joinPath(parts []string) string {
	result := ""
	for i, part := range parts {
		if i > 0 {
			result += "."
		}
		result += part
	}
	return result
}
