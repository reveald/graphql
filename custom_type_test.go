package graphql

import (
	"strings"
	"testing"

	"github.com/graphql-go/graphql"
)

func TestCustomTypeResolvableParameter(t *testing.T) {
	// Create a simple mapping
	mapping := &IndexMapping{
		IndexName: "products",
		Properties: map[string]*Field{
			"id": {Name: "id", Type: FieldTypeKeyword},
		},
	}

	// Create custom Review type
	reviewType := graphql.NewObject(graphql.ObjectConfig{
		Name: "Review",
		Fields: graphql.Fields{
			"id": &graphql.Field{Type: graphql.String},
		},
	})

	// Test Case 1: Resolvable = false (reference only)
	t.Run("Resolvable=false adds resolvable: false to SDL", func(t *testing.T) {
		config := NewConfig(
			WithEnableFederation(),
			WithCustomTypesWithKeys(
				CustomTypeWithKeys{
					Type:       reviewType,
					EntityKeys: []string{"id"},
					Resolvable: false,
				},
			),
			WithQuery("searchProducts", &QueryConfig{
				Mapping: *mapping,
			}),
		)

		sdl, err := GenerateSchemaSDL(config)
		if err != nil {
			t.Fatalf("Failed to generate SDL: %v", err)
		}

		// Check that Review has @key with resolvable: false
		if !strings.Contains(sdl, `@key(fields: "id", resolvable: false)`) {
			t.Errorf("SDL should contain @key with resolvable: false, got:\n%s", sdl)
		}

		// Check that Review is NOT in _Entity union
		if strings.Contains(sdl, "union _Entity") && strings.Contains(sdl, "union _Entity = Review") {
			t.Errorf("Review should NOT be in _Entity union when Resolvable=false")
		}
	})

	// Test Case 2: Resolvable = true (owned entity)
	t.Run("Resolvable=true does not add resolvable parameter", func(t *testing.T) {
		config := NewConfig(
			WithEnableFederation(),
			WithCustomTypesWithKeys(
				CustomTypeWithKeys{
					Type:       reviewType,
					EntityKeys: []string{"id"},
					Resolvable: true,
				},
			),
			WithQuery("searchProducts", &QueryConfig{
				Mapping: *mapping,
			}),
		)

		sdl, err := GenerateSchemaSDL(config)
		if err != nil {
			t.Fatalf("Failed to generate SDL: %v", err)
		}

		// Check that Review has @key WITHOUT resolvable: false
		if !strings.Contains(sdl, `@key(fields: "id")`) {
			t.Errorf("SDL should contain @key(fields: \"id\"), got:\n%s", sdl)
		}

		if strings.Contains(sdl, "resolvable: false") {
			t.Errorf("SDL should NOT contain resolvable: false when Resolvable=true, got:\n%s", sdl)
		}

		// Check that Review IS in _Entity union
		if !strings.Contains(sdl, "union _Entity") || !strings.Contains(sdl, "Review") {
			t.Errorf("Review should be in _Entity union when Resolvable=true")
		}
	})
}
