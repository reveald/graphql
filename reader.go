package graphql

import (
	"fmt"
	"strings"

	"github.com/graphql-go/graphql"
	"github.com/reveald/reveald/v2"
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

	// Handle range filter suffixes (_min, _max) for histogram/range fields.
	// These are exposed by the schema for HistogramFeature and DateHistogramFeature fields.
	// reveald.NewParameter strips the ".min"/".max" suffix and sets the range bounds.
	for _, suffix := range []string{"_min", "_max"} {
		if strings.HasSuffix(name, suffix) {
			baseName := name[:len(name)-len(suffix)]
			esBaseName := ar.resolveESFieldName(baseName)
			baseField := ar.mapping.GetField(esBaseName)
			if baseField != nil && isRangeableFieldType(baseField.Type) {
				rangeSuffix := "." + suffix[1:] // "_min" → ".min", "_max" → ".max"
				return reveald.NewParameter(esBaseName+rangeSuffix, fmt.Sprintf("%v", value)), true, nil
			}
			break
		}
	}

	// Handle field filters: convert GraphQL underscore names back to ES dot-separated paths.
	esFieldName := ar.resolveESFieldName(name)
	field := ar.mapping.GetField(esFieldName)

	param, err := ar.convertFieldArgument(esFieldName, value, field)
	if err != nil {
		return reveald.Parameter{}, false, err
	}
	return param, true, nil
}

// resolveESFieldName converts a GraphQL argument name (underscores) to an ES field path (dots).
// It tries the prefix pattern first (carRelations_car.modelYear), then full conversion
// (carRelations.car.modelYear), falling back to the prefix pattern for virtual fields.
func (ar *ArgumentReader) resolveESFieldName(name string) string {
	if !strings.Contains(name, "_") {
		return name
	}

	parts := strings.SplitN(name, "_", 2)
	prefixedName := parts[0] + "_" + strings.ReplaceAll(parts[1], "_", ".")
	if ar.mapping.GetField(prefixedName) != nil {
		return prefixedName
	}

	fullConversion := strings.ReplaceAll(name, "_", ".")
	if ar.mapping.GetField(fullConversion) != nil {
		return fullConversion
	}

	// Virtual field fallback (features may create aggregation names not in the mapping)
	return prefixedName
}

// isRangeableFieldType returns true for numeric and date field types that support min/max range filtering.
func isRangeableFieldType(ft FieldType) bool {
	switch ft {
	case FieldTypeInteger, FieldTypeLong, FieldTypeShort, FieldTypeByte,
		FieldTypeDouble, FieldTypeFloat, FieldTypeDate:
		return true
	}
	return false
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
