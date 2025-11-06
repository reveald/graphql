package graphql

import (
	"strings"
	"testing"

	"github.com/graphql-go/graphql"
)

func TestFieldTypeOverrides(t *testing.T) {
	// Create a simple mapping
	mapping := &IndexMapping{
		IndexName: "products",
		Properties: map[string]*Field{
			"id":    {Name: "id", Type: FieldTypeKeyword},
			"name":  {Name: "name", Type: FieldTypeText},
			"price": {Name: "price", Type: FieldTypeFloat},
		},
	}

	t.Run("Regular query with field type overrides", func(t *testing.T) {
		config := NewConfig(
			WithQuery("searchProducts", &QueryConfig{
				Mapping: *mapping,
				FieldTypeOverrides: map[string]graphql.Output{
					"id":   graphql.NewNonNull(graphql.ID),
					"name": graphql.NewNonNull(graphql.String),
				},
			}),
		)

		sdl, err := GenerateSchemaSDL(config)
		if err != nil {
			t.Fatalf("Failed to generate SDL: %v", err)
		}

		// Check that id is ID! (non-null ID)
		if !strings.Contains(sdl, "id: ID!") {
			t.Errorf("SDL should contain 'id: ID!', got:\n%s", sdl)
		}

		// Check that name is String! (non-null String)
		if !strings.Contains(sdl, "name: String!") {
			t.Errorf("SDL should contain 'name: String!', got:\n%s", sdl)
		}

		// Check that price is still Float (default, no override)
		if !strings.Contains(sdl, "price: Float") {
			t.Errorf("SDL should contain 'price: Float', got:\n%s", sdl)
		}
	})

	t.Run("Precompiled query with field type overrides", func(t *testing.T) {
		config := NewConfig(
			WithPrecompiledQuery("analytics", &PrecompiledQueryConfig{
				Mapping: *mapping,
				Index:   "products",
				QueryJSON: `{
					"size": 0,
					"aggs": {
						"by_category": {
							"terms": {"field": "category.keyword"}
						}
					}
				}`,
				FieldTypeOverrides: map[string]graphql.Output{
					"id":    graphql.NewNonNull(graphql.ID),
					"price": graphql.NewNonNull(graphql.Float),
				},
			}),
		)

		sdl, err := GenerateSchemaSDL(config)
		if err != nil {
			t.Fatalf("Failed to generate SDL: %v", err)
		}

		// Check that id is ID!
		if !strings.Contains(sdl, "id: ID!") {
			t.Errorf("SDL should contain 'id: ID!', got:\n%s", sdl)
		}

		// Check that price is Float! (non-null)
		if !strings.Contains(sdl, "price: Float!") {
			t.Errorf("SDL should contain 'price: Float!', got:\n%s", sdl)
		}
	})
}
