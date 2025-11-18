package graphql

import (
	"fmt"
	"sort"
	"strings"

	"github.com/graphql-go/graphql"
)

// ExportFederationSDL exports the GraphQL schema as SDL (Schema Definition Language)
// with Apollo Federation v2 annotations
// - sdlEntityKeys: All entities with @key directives (both resolvable and reference-only)
// - resolvableEntityKeys: Only entities resolvable by this subgraph (included in _Entity union)
// - fieldDirectives: Maps type name -> field name -> directive name -> directive args (empty string for no args, e.g., @external)
func ExportFederationSDL(schema graphql.Schema, config *Config, sdlEntityKeys map[string][]string, resolvableEntityKeys map[string][]string, fieldDirectives map[string]map[string]map[string]string) string {
	enableFederation := config.EnableFederation
	var sdl strings.Builder

	// Add federation schema extension if enabled
	if enableFederation {
		sdl.WriteString(`extend schema
  @link(url: "https://specs.apollo.dev/federation/v2.3",
        import: ["@key", "@shareable", "@external", "@requires"])

`)

		// Add _Any scalar
		sdl.WriteString("scalar _Any\n\n")

		// Add _Service type
		sdl.WriteString(`type _Service {
  sdl: String!
}

`)

		// Add _Entity union if we have resolvable entities
		if len(resolvableEntityKeys) > 0 {
			sdl.WriteString("union _Entity = ")
			first := true
			for typeName := range resolvableEntityKeys {
				if !first {
					sdl.WriteString(" | ")
				}
				sdl.WriteString(typeName)
				first = false
			}
			sdl.WriteString("\n\n")
		}
	}

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

	// Determine which type should be extended (if any)
	namespaceTypeName := ""
	if config.QueryNamespace != "" {
		namespaceTypeName = capitalize(config.QueryNamespace)
	}

	for _, typeName := range typeNames {
		gqlType := typeMap[typeName]

		switch t := gqlType.(type) {
		case *graphql.Object:
			// Skip Query, Mutation, Subscription types (handled separately)
			if typeName == "Query" || typeName == "Mutation" || typeName == "Subscription" {
				continue
			}

			// Check if this is the namespace type and should be extended
			isExtended := enableFederation && config.ExtendQueryNamespace && typeName == namespaceTypeName

			// Get entity key fields for this type (if any) from SDL keys
			keyFieldsList := sdlEntityKeys[typeName]

			// Check if this entity is resolvable by this subgraph
			_, isResolvable := resolvableEntityKeys[typeName]

			// Get field directives for this type (if any)
			typeFieldDirectives := fieldDirectives[typeName]

			sdl.WriteString(exportObjectType(t, enableFederation, isExtended, keyFieldsList, isResolvable, typeFieldDirectives))
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
		sdl.WriteString(exportObjectType(queryType, false, false, nil, true, nil)) // Query is never shareable, extended, has entity keys, or field directives
		sdl.WriteString("\n")
	}

	return sdl.String()
}

// exportObjectType exports a GraphQL object type as SDL
func exportObjectType(objType *graphql.Object, enableFederation bool, isExtended bool, entityKeyFields []string, isResolvable bool, typeFieldDirectives map[string]map[string]string) string {
	var sdl strings.Builder

	// Add description if present (using # comment syntax for types)
	if objType.Description() != "" {
		sdl.WriteString(fmt.Sprintf("# %s\n", objType.Description()))
	}

	// Type declaration with directives (@key, @shareable) if applicable
	typeName := objType.Name()
	isShareable := enableFederation && IsShareableType(typeName)
	hasEntityKeys := enableFederation && len(entityKeyFields) > 0

	// Build type declaration with directives
	var typeDecl string
	var directives string

	// Add @key directives (one for each key specification)
	if hasEntityKeys {
		for _, keyField := range entityKeyFields {
			if directives != "" {
				directives += " "
			}
			// Add resolvable: false parameter if entity is not resolvable by this subgraph
			if !isResolvable {
				directives += fmt.Sprintf("@key(fields: \"%s\", resolvable: false)", keyField)
			} else {
				directives += fmt.Sprintf("@key(fields: \"%s\")", keyField)
			}
		}
	}

	// Add @shareable directive if applicable
	if isShareable {
		if directives != "" {
			directives += " "
		}
		directives += "@shareable"
	}

	// Add space before opening brace if we have directives
	if directives != "" {
		directives = " " + directives
	}

	if isExtended {
		// Use "extend type" for types defined in other subgraphs
		typeDecl = fmt.Sprintf("extend type %s%s {\n", typeName, directives)
	} else {
		// Use "type" for types owned by this subgraph
		typeDecl = fmt.Sprintf("type %s%s {\n", typeName, directives)
	}

	sdl.WriteString(typeDecl)

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

		// Build field arguments if present
		var argsStr string
		if len(field.Args) > 0 {
			var argParts []string
			for _, arg := range field.Args {
				argType := exportType(arg.Type)
				argParts = append(argParts, fmt.Sprintf("%s: %s", arg.Name(), argType))
			}
			argsStr = fmt.Sprintf("(%s)", strings.Join(argParts, ", "))
		}

		fieldDecl := fmt.Sprintf("  %s%s: %s", fieldName, argsStr, fieldType)

		// Add field directives (e.g., @external, @requires)
		if enableFederation && typeFieldDirectives != nil {
			if directives, hasDirectives := typeFieldDirectives[fieldName]; hasDirectives {
				for directiveName, directiveArgs := range directives {
					if directiveArgs != "" {
						fieldDecl += fmt.Sprintf(" @%s(fields: \"%s\")", directiveName, directiveArgs)
					} else {
						fieldDecl += fmt.Sprintf(" @%s", directiveName)
					}
				}
			}
		}

		sdl.WriteString(fieldDecl + "\n")
	}

	sdl.WriteString("}\n")
	return sdl.String()
}

// exportEnumType exports a GraphQL enum type as SDL
func exportEnumType(enumType *graphql.Enum) string {
	var sdl strings.Builder

	if enumType.Description() != "" {
		sdl.WriteString(fmt.Sprintf("# %s\n", enumType.Description()))
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
		sdl.WriteString(fmt.Sprintf("# %s\n", inputType.Description()))
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
