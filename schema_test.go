package graphql

import (
	"testing"

	"github.com/reveald/reveald/v2"
	"github.com/reveald/reveald/v2/featureset"
)

// mockWrapperFeature is a test mock that simulates NestedDocumentWrapper
// with a `features` slice containing child features
type mockWrapperFeature struct {
	path     string
	features []reveald.Feature
}

func (m *mockWrapperFeature) Process(builder *reveald.QueryBuilder, next reveald.FeatureFunc) (*reveald.Result, error) {
	return next(builder)
}

// mockSimpleFeature is a test mock with just a property field
type mockSimpleFeature struct {
	property string
}

func (m *mockSimpleFeature) Process(builder *reveald.QueryBuilder, next reveald.FeatureFunc) (*reveald.Result, error) {
	return next(builder)
}

func TestExtractAggregationFields(t *testing.T) {
	t.Run("extracts fields from DynamicFilterFeature", func(t *testing.T) {
		features := []reveald.Feature{
			featureset.NewDynamicFilterFeature("category"),
			featureset.NewDynamicFilterFeature("brand"),
		}

		fields := extractAggregationFields(features)

		expected := []string{"category", "brand"}
		if len(fields) != len(expected) {
			t.Fatalf("expected %d fields, got %d: %v", len(expected), len(fields), fields)
		}

		for i, field := range fields {
			if field != expected[i] {
				t.Errorf("expected field %d to be %q, got %q", i, expected[i], field)
			}
		}
	})

	t.Run("extracts fields from wrapper feature with nested features", func(t *testing.T) {
		// This simulates NestedDocumentWrapper structure
		features := []reveald.Feature{
			&mockWrapperFeature{
				path: "carRelations",
				features: []reveald.Feature{
					&mockSimpleFeature{property: "carRelations.car.model"},
					&mockSimpleFeature{property: "carRelations.car.color"},
				},
			},
		}

		fields := extractAggregationFields(features)

		expected := []string{"carRelations.car.model", "carRelations.car.color"}
		if len(fields) != len(expected) {
			t.Fatalf("expected %d fields, got %d: %v", len(expected), len(fields), fields)
		}

		for i, field := range fields {
			if field != expected[i] {
				t.Errorf("expected field %d to be %q, got %q", i, expected[i], field)
			}
		}
	})

	t.Run("extracts fields from mixed features and nested wrappers", func(t *testing.T) {
		features := []reveald.Feature{
			featureset.NewDynamicFilterFeature("topLevelField"),
			&mockWrapperFeature{
				path: "items",
				features: []reveald.Feature{
					&mockSimpleFeature{property: "items.category"},
					&mockSimpleFeature{property: "items.price"},
				},
			},
			featureset.NewDynamicFilterFeature("anotherTopLevel"),
		}

		fields := extractAggregationFields(features)

		expected := []string{"topLevelField", "items.category", "items.price", "anotherTopLevel"}
		if len(fields) != len(expected) {
			t.Fatalf("expected %d fields, got %d: %v", len(expected), len(fields), fields)
		}

		for i, field := range fields {
			if field != expected[i] {
				t.Errorf("expected field %d to be %q, got %q", i, expected[i], field)
			}
		}
	})

	t.Run("handles deeply nested wrappers", func(t *testing.T) {
		// Test nested wrappers containing other wrappers
		features := []reveald.Feature{
			&mockWrapperFeature{
				path: "level1",
				features: []reveald.Feature{
					&mockWrapperFeature{
						path: "level1.level2",
						features: []reveald.Feature{
							&mockSimpleFeature{property: "level1.level2.deepField"},
						},
					},
				},
			},
		}

		fields := extractAggregationFields(features)

		if len(fields) != 1 {
			t.Fatalf("expected 1 field, got %d: %v", len(fields), fields)
		}

		if fields[0] != "level1.level2.deepField" {
			t.Errorf("expected field to be %q, got %q", "level1.level2.deepField", fields[0])
		}
	})

	t.Run("deduplicates fields", func(t *testing.T) {
		features := []reveald.Feature{
			featureset.NewDynamicFilterFeature("category"),
			featureset.NewDynamicFilterFeature("category"),
		}

		fields := extractAggregationFields(features)

		if len(fields) != 1 {
			t.Fatalf("expected 1 field (deduplicated), got %d: %v", len(fields), fields)
		}

		if fields[0] != "category" {
			t.Errorf("expected field to be %q, got %q", "category", fields[0])
		}
	})

	t.Run("deduplicates fields across wrapper and direct features", func(t *testing.T) {
		features := []reveald.Feature{
			featureset.NewDynamicFilterFeature("sharedField"),
			&mockWrapperFeature{
				path: "wrapper",
				features: []reveald.Feature{
					&mockSimpleFeature{property: "sharedField"},
				},
			},
		}

		fields := extractAggregationFields(features)

		if len(fields) != 1 {
			t.Fatalf("expected 1 field (deduplicated), got %d: %v", len(fields), fields)
		}

		if fields[0] != "sharedField" {
			t.Errorf("expected field to be %q, got %q", "sharedField", fields[0])
		}
	})
}
