package graphql

import (
	"fmt"
	"sort"
	"strings"

	"github.com/graphql-go/graphql"
)

// ExportFederationSDL exports the GraphQL schema as SDL (Schema Definition Language)
// with Apollo Federation v2 annotations
func ExportFederationSDL(schema graphql.Schema, enableFederation bool) string {
	var sdl strings.Builder

	// Add federation schema extension if enabled
	if enableFederation {
		sdl.WriteString(`extend schema
  @link(url: "https://specs.apollo.dev/federation/v2.3",
        import: ["@key", "@shareable"])

`)
	}

	// Export scalar types (if any custom ones)
	// graphql-go doesn't expose custom scalars easily, so we skip for now

	// Export types from schema
	typeMap := schema.TypeMap()

	// Get all type names and sort for consistent output
	var typeNames []string
	for typeName := range typeMap {
		// Skip introspection types
		if strings.HasPrefix(typeName, "__") {
			continue
		}
		typeNames = append(typeNames, typeName)
	}
	sort.Strings(typeNames)

	for _, typeName := range typeNames {
		gqlType := typeMap[typeName]

		switch t := gqlType.(type) {
		case *graphql.Object:
			// Skip Query, Mutation, Subscription types (handled separately)
			if typeName == "Query" || typeName == "Mutation" || typeName == "Subscription" {
				continue
			}

			sdl.WriteString(exportObjectType(t, enableFederation))
			sdl.WriteString("\n")

		case *graphql.Enum:
			sdl.WriteString(exportEnumType(t))
			sdl.WriteString("\n")

		case *graphql.InputObject:
			sdl.WriteString(exportInputObjectType(t))
			sdl.WriteString("\n")
		}
	}

	// Export Query type last
	if queryType := schema.QueryType(); queryType != nil {
		sdl.WriteString(exportObjectType(queryType, false)) // Query is never shareable
		sdl.WriteString("\n")
	}

	return sdl.String()
}

// exportObjectType exports a GraphQL object type as SDL
func exportObjectType(objType *graphql.Object, enableFederation bool) string {
	var sdl strings.Builder

	// Add description if present
	if objType.Description() != "" {
		sdl.WriteString(fmt.Sprintf(`"""%s"""
`, objType.Description()))
	}

	// Type declaration with @shareable if applicable
	typeName := objType.Name()
	isShareable := enableFederation && IsShareableType(typeName)

	if isShareable {
		sdl.WriteString(fmt.Sprintf("type %s @shareable {\n", typeName))
	} else {
		sdl.WriteString(fmt.Sprintf("type %s {\n", typeName))
	}

	// Export fields
	fields := objType.Fields()

	// Get field names and sort for consistent output
	var fieldNames []string
	for fieldName := range fields {
		fieldNames = append(fieldNames, fieldName)
	}
	sort.Strings(fieldNames)

	for _, fieldName := range fieldNames {
		field := fields[fieldName]

		// Add field description if present
		if field.Description != "" {
			sdl.WriteString(fmt.Sprintf("  \"\"\"%s\"\"\"\n", field.Description))
		}

		// Field declaration
		fieldType := exportType(field.Type)
		sdl.WriteString(fmt.Sprintf("  %s: %s\n", fieldName, fieldType))
	}

	sdl.WriteString("}\n")
	return sdl.String()
}

// exportEnumType exports a GraphQL enum type as SDL
func exportEnumType(enumType *graphql.Enum) string {
	var sdl strings.Builder

	if enumType.Description() != "" {
		sdl.WriteString(fmt.Sprintf(`"""%s"""
`, enumType.Description()))
	}

	sdl.WriteString(fmt.Sprintf("enum %s {\n", enumType.Name()))

	for _, value := range enumType.Values() {
		if value.Description != "" {
			sdl.WriteString(fmt.Sprintf("  \"\"\"%s\"\"\"\n", value.Description))
		}
		sdl.WriteString(fmt.Sprintf("  %s\n", value.Name))
	}

	sdl.WriteString("}\n")
	return sdl.String()
}

// exportInputObjectType exports a GraphQL input object type as SDL
func exportInputObjectType(inputType *graphql.InputObject) string {
	var sdl strings.Builder

	if inputType.Description() != "" {
		sdl.WriteString(fmt.Sprintf(`"""%s"""
`, inputType.Description()))
	}

	sdl.WriteString(fmt.Sprintf("input %s {\n", inputType.Name()))

	fields := inputType.Fields()
	var fieldNames []string
	for fieldName := range fields {
		fieldNames = append(fieldNames, fieldName)
	}
	sort.Strings(fieldNames)

	for _, fieldName := range fieldNames {
		field := fields[fieldName]
		fieldType := exportType(field.Type)
		sdl.WriteString(fmt.Sprintf("  %s: %s\n", fieldName, fieldType))
	}

	sdl.WriteString("}\n")
	return sdl.String()
}

// exportType exports a GraphQL type reference as SDL string
func exportType(t graphql.Type) string {
	switch typ := t.(type) {
	case *graphql.NonNull:
		return fmt.Sprintf("%s!", exportType(typ.OfType))
	case *graphql.List:
		return fmt.Sprintf("[%s]", exportType(typ.OfType))
	case *graphql.Object:
		return typ.Name()
	case *graphql.Scalar:
		return typ.Name()
	case *graphql.Enum:
		return typ.Name()
	case *graphql.InputObject:
		return typ.Name()
	default:
		return "String" // Fallback
	}
}
