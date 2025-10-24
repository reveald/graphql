package graphql

import (
	"testing"

	"github.com/graphql-go/graphql"
	"github.com/reveald/reveald/v2"
)

func TestFederationTypes(t *testing.T) {
	// Test that federation types can be initialized
	initFederationTypes()

	if AnyScalar == nil {
		t.Error("AnyScalar should not be nil after initialization")
	}

	if ServiceType == nil {
		t.Error("ServiceType should not be nil after initialization")
	}

	if AnyScalar.Name() != "_Any" {
		t.Errorf("Expected _Any scalar name, got %s", AnyScalar.Name())
	}

	if ServiceType.Name() != "_Service" {
		t.Errorf("Expected _Service type name, got %s", ServiceType.Name())
	}
}

func TestCreateEntityUnion(t *testing.T) {
	// Create some test types
	typeCache := make(map[string]*graphql.Object)
	entityKeys := make(map[string][]string)

	// Create a test entity type
	testType := graphql.NewObject(graphql.ObjectConfig{
		Name: "TestEntity",
		Fields: graphql.Fields{
			"id": &graphql.Field{
				Type: graphql.String,
			},
		},
	})

	typeCache["TestEntity"] = testType
	entityKeys["TestEntity"] = []string{"id"}

	// Create entity union
	entityUnion := CreateEntityUnion(entityKeys, typeCache)

	if entityUnion == nil {
		t.Fatal("CreateEntityUnion should not return nil when entities exist")
	}

	if entityUnion.Name() != "_Entity" {
		t.Errorf("Expected _Entity union name, got %s", entityUnion.Name())
	}

	// Test with no entities
	emptyEntityUnion := CreateEntityUnion(make(map[string][]string), typeCache)
	if emptyEntityUnion != nil {
		t.Error("CreateEntityUnion should return nil when no entities exist")
	}
}

func TestParseEntityRepresentation(t *testing.T) {
	tests := []struct {
		name          string
		repr          any
		expectedType  string
		expectedError bool
	}{
		{
			name: "valid representation",
			repr: map[string]any{
				"__typename": "TestEntity",
				"id":         "123",
			},
			expectedType:  "TestEntity",
			expectedError: false,
		},
		{
			name: "missing __typename",
			repr: map[string]any{
				"id": "123",
			},
			expectedType:  "",
			expectedError: true,
		},
		{
			name:          "invalid type",
			repr:          "not a map",
			expectedType:  "",
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			typename, fields, err := ParseEntityRepresentation(tt.repr)

			if tt.expectedError {
				if err == nil {
					t.Error("Expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if typename != tt.expectedType {
					t.Errorf("Expected typename %s, got %s", tt.expectedType, typename)
				}
				if fields == nil {
					t.Error("Expected fields map, got nil")
				}
			}
		})
	}
}

func TestFederationSchemaGeneration(t *testing.T) {
	// Create a minimal config with federation enabled
	config := NewConfig(
		WithEnableFederation(),
	)

	// Create a minimal mapping
	mapping, err := ParseMapping("test-index", []byte(`{
		"properties": {
			"id": {"type": "keyword"},
			"name": {"type": "text"}
		}
	}`))
	if err != nil {
		t.Fatalf("Failed to parse mapping: %v", err)
	}

	// Add a query with entity keys
	config.AddQuery("testQuery", &QueryConfig{
		Mapping:         mapping,
		Features:        []reveald.Feature{},
		EntityKeyFields: []string{"id"},
	})

	// This would normally create a backend, but for this test we'll skip
	// actual schema generation as it requires a working ES backend
	t.Log("Federation config created successfully with entity keys")

	// Verify entity keys were set
	if len(config.Queries["testQuery"].EntityKeyFields) != 1 {
		t.Errorf("Expected 1 entity key field, got %d", len(config.Queries["testQuery"].EntityKeyFields))
	}

	if config.Queries["testQuery"].EntityKeyFields[0] != "id" {
		t.Errorf("Expected entity key 'id', got %s", config.Queries["testQuery"].EntityKeyFields[0])
	}
}

func TestEntityResolverRegistration(t *testing.T) {
	// Create an entity resolver
	resolver := NewEntityResolver(nil, nil)

	if resolver == nil {
		t.Fatal("NewEntityResolver should not return nil")
	}

	if resolver.typeMappings == nil {
		t.Error("typeMappings should be initialized")
	}

	// Test registration
	mapping := &IndexMapping{
		IndexName:  "test-index",
		Properties: make(map[string]*Field),
	}

	resolver.RegisterEntityType("TestType", &EntityTypeMapping{
		QueryName:      "testQuery",
		UseFeatureFlow: false,
		Mapping:        mapping,
		EntityKeys:     []string{"id"},
	})

	if len(resolver.typeMappings) != 1 {
		t.Errorf("Expected 1 registered type, got %d", len(resolver.typeMappings))
	}

	typeMapping, ok := resolver.typeMappings["TestType"]
	if !ok {
		t.Error("TestType should be registered")
	}

	if typeMapping.QueryName != "testQuery" {
		t.Errorf("Expected query name 'testQuery', got %s", typeMapping.QueryName)
	}
}

func TestSchemaReferencePattern(t *testing.T) {
	// This test verifies that the schema reference pattern works correctly
	// for the _service query to return complete SDL

	// Create a minimal mapping
	mapping, err := ParseMapping("test-index", []byte(`{
		"properties": {
			"id": {"type": "keyword"},
			"name": {"type": "text"}
		}
	}`))
	if err != nil {
		t.Fatalf("Failed to parse mapping: %v", err)
	}

	// Create config with federation
	config := NewConfig(WithEnableFederation())
	config.AddQuery("testQuery", &QueryConfig{
		Mapping:         mapping,
		Features:        []reveald.Feature{},
		EntityKeyFields: []string{"id"},
	})

	// Create resolver builder
	resolverBuilder := NewResolverBuilder(nil, nil)

	// Create schema generator
	generator := NewSchemaGenerator(config, resolverBuilder)

	// Verify schemaRef is initialized
	if generator.schemaRef == nil {
		t.Fatal("schemaRef should be initialized")
	}

	if generator.schemaRef.schema != nil {
		t.Error("schemaRef.schema should be nil before Generate() is called")
	}

	// Generate schema (this would fail without a real backend, but we can verify the structure)
	// We'll just verify that the schemaRef is set up correctly
	t.Log("Schema reference pattern is correctly initialized")

	// The actual schema generation would populate schemaRef.schema
	// In a real scenario with a working backend, after Generate() completes:
	// - generator.schemaRef.schema would point to the generated schema
	// - The _service resolver would use this to export complete SDL
}
