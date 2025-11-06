package graphql

import (
	"fmt"
	"testing"

	"github.com/graphql-go/graphql"
)

func TestFieldDirectives(t *testing.T) {
	// Test the field directives storage
	config := NewConfig(
		WithEnableFederation(),
		WithTypeExtension("TestType", []FieldExtension{
			{
				FieldName: "testField",
				Field: &graphql.Field{
					Type: graphql.String,
				},
				Directives: map[string]string{
					"requires": "other { field }",
				},
			},
		}),
	)

	resolverBuilder := &ResolverBuilder{}
	generator := NewSchemaGenerator(config, resolverBuilder)

	// Check if fieldDirectives are captured
	if generator.fieldDirectives["TestType"] == nil {
		t.Error("Expected TestType in fieldDirectives, got nil")
	}

	if directives := generator.fieldDirectives["TestType"]["testField"]; directives == nil {
		t.Error("Expected testField directives, got nil")
	} else {
		fmt.Printf("Directives: %+v\n", directives)
	}
}
