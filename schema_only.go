package graphql

import (
	"fmt"

	"github.com/graphql-go/graphql"
)

// GenerateSchemaSDL generates the GraphQL schema SDL without requiring
// Elasticsearch connection or backend. This is useful for:
// - CI/CD pipelines
// - Schema validation
// - Apollo Federation composition
// - Documentation generation
//
// Unlike New(), this function only generates the schema structure and doesn't
// create any resolvers. The ES client is only needed to introspect aggregation
// structures from query definitions.
//
// Example:
//
//	config := NewConfig()
//	config.EnableFederation = true
//	config.AddPrecompiledQuery("myQuery", &PrecompiledQueryConfig{
//	    Index: "my-index",
//	    QueryBuilder: buildMyQuery,
//	})
//
//	sdl, err := GenerateSchemaSDL(mapping, config)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Println(sdl)
func GenerateSchemaSDL(config *Config) (string, error) {
	// Create a nil resolver builder (we won't execute queries, just generate schema)
	resolverBuilder := &ResolverBuilder{
		backend:  nil, // Not needed for schema generation
		esClient: nil, // Not needed for schema generation
	}

	// Generate the schema
	generator := NewSchemaGenerator(config, resolverBuilder)
	schema, err := generator.Generate()
	if err != nil {
		return "", fmt.Errorf("failed to generate schema: %w", err)
	}

	// Export as SDL with entity keys (both SDL and resolvable)
	sdl := ExportFederationSDL(schema, config, generator.sdlEntityKeys, generator.entityKeys)
	return sdl, nil
}

// GenerateSchema generates just the GraphQL schema object without requiring
// Elasticsearch connection. This returns the schema object which can be used
// for introspection or further processing.
//
// Note: The returned schema has nil resolvers and cannot execute queries.
// Use New() if you need a fully functional API.
func GenerateSchema(config *Config) (graphql.Schema, error) {
	// Create a nil resolver builder
	resolverBuilder := &ResolverBuilder{
		backend:  nil,
		esClient: nil,
	}

	// Generate the schema
	generator := NewSchemaGenerator(config, resolverBuilder)
	return generator.Generate()
}
