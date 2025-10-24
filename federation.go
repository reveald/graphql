package graphql

import (
	"encoding/json"
	"fmt"

	"github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/language/ast"
)

// Apollo Federation v2.3 Directives
//
// These directives enable this GraphQL schema to work as a subgraph in
// an Apollo Federation supergraph. Common types like Bucket, Pagination, etc.
// are marked as @shareable so they can be defined in multiple subgraphs.

var (
	// ShareableDirective indicates that a type/field can be resolved by multiple subgraphs
	ShareableDirective *graphql.Directive

	// LinkDirective links to external specifications (federation v2)
	LinkDirective *graphql.Directive

	// KeyDirective marks an entity with a primary key
	KeyDirective *graphql.Directive

	// AnyScalar is a scalar that can represent any JSON value (for entity representations)
	AnyScalar *graphql.Scalar

	// ServiceType is the _Service type for federation
	ServiceType *graphql.Object
)

// initFederationDirectives initializes Apollo Federation directives
func initFederationDirectives() {
	if ShareableDirective != nil {
		return // Already initialized
	}

	// @shareable directive
	ShareableDirective = graphql.NewDirective(graphql.DirectiveConfig{
		Name:        "shareable",
		Description: "Indicates that an object type's field is allowed to be resolved by multiple subgraphs (Apollo Federation v2)",
		Locations: []string{
			graphql.DirectiveLocationObject,
			graphql.DirectiveLocationFieldDefinition,
		},
	})

	// @link directive
	LinkDirective = graphql.NewDirective(graphql.DirectiveConfig{
		Name:        "link",
		Description: "Links to an external specification",
		Locations:   []string{graphql.DirectiveLocationSchema},
		Args: graphql.FieldConfigArgument{
			"url": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "The URL of the specification",
			},
			"import": &graphql.ArgumentConfig{
				Type:        graphql.NewList(graphql.String),
				Description: "Specific elements to import from the specification",
			},
		},
	})

	// @key directive (for entities)
	KeyDirective = graphql.NewDirective(graphql.DirectiveConfig{
		Name:        "key",
		Description: "Designates an object type as an entity and specifies its key fields",
		Locations:   []string{graphql.DirectiveLocationObject},
		Args: graphql.FieldConfigArgument{
			"fields": &graphql.ArgumentConfig{
				Type:        graphql.NewNonNull(graphql.String),
				Description: "A selection set of fields (e.g., 'id' or 'id sku')",
			},
			"resolvable": &graphql.ArgumentConfig{
				Type:        graphql.Boolean,
				Description: "Whether this subgraph can resolve queries for the entity (default: true)",
			},
		},
	})
}

// GetFederationDirectives returns all federation directives
func GetFederationDirectives() []*graphql.Directive {
	initFederationDirectives()
	return []*graphql.Directive{
		ShareableDirective,
		LinkDirective,
		KeyDirective,
	}
}

// ShareableTypes lists types that should be marked as @shareable in federation
var ShareableTypes = []string{
	"Bucket",                // Reveald bucket type (used across queries)
	"Pagination",            // Pagination info (common across all queries)
	"StatsValues",           // Stats aggregation values
	"GenericBucket",         // Generic bucket fallback
	"GenericAggregation",    // Generic aggregation fallback
}

// IsShareableType checks if a type name should be marked as shareable
func IsShareableType(typeName string) bool {
	// Check exact matches
	for _, st := range ShareableTypes {
		if typeName == st {
			return true
		}
	}

	// All bucket types are shareable (ends with "Bucket")
	if len(typeName) > 6 && typeName[len(typeName)-6:] == "Bucket" {
		return true
	}

	// All filter bucket types are shareable (contains "FilterBucket")
	if len(typeName) > 12 && typeName[len(typeName)-12:] == "FilterBucket" {
		return true
	}

	return false
}

// initFederationTypes initializes Apollo Federation special types (_Any scalar, _Service type)
func initFederationTypes() {
	if AnyScalar != nil {
		return // Already initialized
	}

	// _Any scalar - can represent any JSON value
	AnyScalar = graphql.NewScalar(graphql.ScalarConfig{
		Name:        "_Any",
		Description: "Scalar representing any value (for entity representations)",
		Serialize: func(value any) any {
			return value
		},
		ParseValue: func(value any) any {
			return value
		},
		ParseLiteral: func(valueAST ast.Value) any {
			switch valueAST := valueAST.(type) {
			case *ast.ObjectValue:
				obj := make(map[string]any)
				for _, field := range valueAST.Fields {
					obj[field.Name.Value] = parseLiteralValue(field.Value)
				}
				return obj
			default:
				return parseLiteralValue(valueAST)
			}
		},
	})

	// _Service type
	ServiceType = graphql.NewObject(graphql.ObjectConfig{
		Name:        "_Service",
		Description: "Service type for Apollo Federation",
		Fields: graphql.Fields{
			"sdl": &graphql.Field{
				Type:        graphql.String,
				Description: "The SDL (Schema Definition Language) for this service",
			},
		},
	})
}

// parseLiteralValue converts AST literal values to Go values
func parseLiteralValue(valueAST ast.Value) any {
	switch valueAST := valueAST.(type) {
	case *ast.StringValue:
		return valueAST.Value
	case *ast.IntValue:
		// Try to parse as int, fallback to string
		var i int
		if _, err := fmt.Sscanf(valueAST.Value, "%d", &i); err == nil {
			return i
		}
		return valueAST.Value
	case *ast.FloatValue:
		var f float64
		if _, err := fmt.Sscanf(valueAST.Value, "%f", &f); err == nil {
			return f
		}
		return valueAST.Value
	case *ast.BooleanValue:
		return valueAST.Value
	case *ast.ListValue:
		list := make([]any, len(valueAST.Values))
		for i, v := range valueAST.Values {
			list[i] = parseLiteralValue(v)
		}
		return list
	case *ast.ObjectValue:
		obj := make(map[string]any)
		for _, field := range valueAST.Fields {
			obj[field.Name.Value] = parseLiteralValue(field.Value)
		}
		return obj
	default:
		return nil
	}
}

// CreateEntityUnion creates the _Entity union from all entity types (types with @key directives)
func CreateEntityUnion(entityKeys map[string][]string, typeCache map[string]*graphql.Object) *graphql.Union {
	if len(entityKeys) == 0 {
		return nil
	}

	// Collect all entity types
	var entityTypes []*graphql.Object
	for typeName := range entityKeys {
		if objType, ok := typeCache[typeName]; ok {
			entityTypes = append(entityTypes, objType)
		}
	}

	if len(entityTypes) == 0 {
		return nil
	}

	return graphql.NewUnion(graphql.UnionConfig{
		Name:        "_Entity",
		Description: "Union of all entity types for Apollo Federation",
		Types:       entityTypes,
		ResolveType: func(p graphql.ResolveTypeParams) *graphql.Object {
			// Resolve type based on the object data
			if obj, ok := p.Value.(map[string]any); ok {
				if typename, ok := obj["__typename"].(string); ok {
					if objType, exists := typeCache[typename]; exists {
						return objType
					}
				}
			}
			return nil
		},
	})
}

// ParseEntityRepresentation parses an entity representation (map with __typename and key fields)
func ParseEntityRepresentation(repr any) (typename string, fields map[string]any, err error) {
	reprMap, ok := repr.(map[string]any)
	if !ok {
		// Try to unmarshal if it's a JSON string or bytes
		if str, ok := repr.(string); ok {
			if err := json.Unmarshal([]byte(str), &reprMap); err != nil {
				return "", nil, fmt.Errorf("representation is not a valid object: %w", err)
			}
		} else {
			return "", nil, fmt.Errorf("representation must be an object")
		}
	}

	typename, ok = reprMap["__typename"].(string)
	if !ok || typename == "" {
		return "", nil, fmt.Errorf("representation missing __typename field")
	}

	return typename, reprMap, nil
}
