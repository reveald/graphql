package graphql

import (
	"fmt"
	"strings"

	"github.com/graphql-go/graphql"
	"github.com/reveald/reveald"
)

// ArgumentReader converts GraphQL arguments to reveald Parameters
type ArgumentReader struct {
	mapping *IndexMapping
}

// NewArgumentReader creates a new argument reader
func NewArgumentReader(mapping *IndexMapping) *ArgumentReader {
	return &ArgumentReader{
		mapping: mapping,
	}
}

// Read converts GraphQL resolver params to reveald Request
func (ar *ArgumentReader) Read(params graphql.ResolveParams) (*reveald.Request, error) {
	request := reveald.NewRequest()

	for argName, argValue := range params.Args {
		if argValue == nil {
			continue
		}

		param, ok, err := ar.convertArgument(argName, argValue)
		if err != nil {
			return nil, fmt.Errorf("failed to convert argument %s: %w", argName, err)
		}

		if ok {
			request.Append(param)
		}
	}

	return request, nil
}

// convertArgument converts a single GraphQL argument to a reveald Parameter
func (ar *ArgumentReader) convertArgument(name string, value any) (reveald.Parameter, bool, error) {
	// Handle special pagination/sorting arguments
	switch name {
	case "limit":
		if v, ok := value.(int); ok {
			return reveald.NewParameter("size", fmt.Sprintf("%d", v)), true, nil
		}
	case "offset":
		if v, ok := value.(int); ok {
			return reveald.NewParameter("offset", fmt.Sprintf("%d", v)), true, nil
		}
	case "sort":
		if v, ok := value.(string); ok {
			return reveald.NewParameter("sort", v), true, nil
		}
	}

	// Handle field filters
	// Convert underscores back to dots for nested fields (GraphQL doesn't allow dots)
	// But keep prefixes intact (e.g., processes_tasks_process → processes_tasks.process)
	esFieldName := name

	// Check if this looks like a prefixed field (prefix_field.subfield pattern)
	// Common prefixes: processes_, tasks_, insights_
	if strings.Contains(name, "_") {
		parts := strings.SplitN(name, "_", 2)
		if len(parts) == 2 {
			// Try converting just the second part's underscores to dots
			possibleESName := parts[0] + "_" + strings.ReplaceAll(parts[1], "_", ".")

			// This handles: processes_tasks_process → processes_tasks.process
			// But for regular fields, fall through to full conversion
			esFieldName = possibleESName
		}
	}

	// Try to find in mapping with prefixed name first
	field := ar.mapping.GetField(esFieldName)

	// If not found and contains underscore, try full conversion
	if field == nil && strings.Contains(name, "_") {
		esFieldName = strings.ReplaceAll(name, "_", ".")
		field = ar.mapping.GetField(esFieldName)
	}

	// If still not found, it might be a virtual field - allow it through
	// (Features create virtual aggregation names)
	if field == nil {
		// For virtual fields, use the parameter name as-is for prefixed fields
		// or convert underscores to dots for regular nested fields
		if strings.Contains(name, "_") {
			parts := strings.SplitN(name, "_", 2)
			if len(parts) == 2 {
				esFieldName = parts[0] + "_" + strings.ReplaceAll(parts[1], "_", ".")
			}
		}
	}

	param, err := ar.convertFieldArgument(esFieldName, value, field)
	if err != nil {
		return reveald.Parameter{}, false, err
	}
	return param, true, nil
}

// convertFieldArgument converts a field-specific argument
func (ar *ArgumentReader) convertFieldArgument(name string, value any, field *Field) (reveald.Parameter, error) {
	// Handle arrays of values
	if values, ok := value.([]any); ok {
		var stringValues []string
		for _, v := range values {
			stringValues = append(stringValues, fmt.Sprintf("%v", v))
		}
		return reveald.NewParameter(name, stringValues...), nil
	}

	// Handle single values
	return reveald.NewParameter(name, fmt.Sprintf("%v", value)), nil
}
