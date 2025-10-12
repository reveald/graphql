package graphql

import (
	"github.com/graphql-go/graphql"
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
