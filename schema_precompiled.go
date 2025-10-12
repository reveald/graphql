package graphql

import (
	"fmt"

	"github.com/graphql-go/graphql"
)

// generatePrecompiledQueryField generates a GraphQL field for a precompiled query
func (sg *SchemaGenerator) generatePrecompiledQueryField(queryName string, queryConfig *PrecompiledQueryConfig) (*graphql.Field, error) {
	// Validate the configuration
	if err := queryConfig.Validate(); err != nil {
		return nil, fmt.Errorf("invalid precompiled query config: %w", err)
	}

	// Load a sample query to ensure it works (this validates the file/builder at startup)
	sampleArgs := queryConfig.SampleParameters
	if sampleArgs == nil {
		sampleArgs = make(map[string]any)
	}
	sampleReq, err := queryConfig.LoadQuery(sampleArgs, nil) // nil httpReq for schema generation
	if err != nil {
		return nil, fmt.Errorf("failed to load sample query: %w", err)
	}

	// Generate typed aggregations from query structure
	var aggsType *graphql.Object
	if sampleReq.Aggregations != nil {
		aggsType = sg.generateTypedAggregationsType(queryName, sampleReq.Aggregations)
	}

	// Generate the result type with typed aggregations
	resultType := sg.generateSimplePrecompiledResultType(queryName, queryConfig, aggsType)

	// Use parameters from config, or empty if not specified
	args := queryConfig.Parameters
	if args == nil {
		args = graphql.FieldConfigArgument{}
	}

	return &graphql.Field{
		Type:        resultType,
		Description: queryConfig.Description,
		Args:        args,
		Resolve:     sg.resolverBuilder.BuildPrecompiledResolver(queryName, queryConfig),
	}, nil
}

// generateSimplePrecompiledResultType creates a result type with optional typed aggregations
func (sg *SchemaGenerator) generateSimplePrecompiledResultType(queryName string, queryConfig *PrecompiledQueryConfig, aggsType *graphql.Object) *graphql.Object {
	typeName := fmt.Sprintf("%sResult", capitalize(queryName))

	// Check cache
	if cachedType, ok := sg.typeCache[typeName]; ok {
		return cachedType
	}

	// Use index name for document type (same as regular queries)
	indexName := sg.getPrecompiledIndexNameForType(queryConfig)
	docTypeName := fmt.Sprintf("%sDocument", sanitizeTypeName(indexName))

	// Check if document type already exists in cache (shared across queries)
	var docType *graphql.Object
	if cached, ok := sg.typeCache[docTypeName]; ok {
		docType = cached
	} else {
		// Create new document type with just id field (auto-resolves other fields)
		docType = graphql.NewObject(graphql.ObjectConfig{
			Name: docTypeName,
			Fields: graphql.Fields{
				"id": &graphql.Field{Type: graphql.String},
				// Other fields will auto-resolve from ES response
			},
		})
		sg.typeCache[docTypeName] = docType
	}

	fields := graphql.Fields{
		"totalCount": &graphql.Field{
			Type:        graphql.Int,
			Description: "Total number of hits",
		},
		"hits": &graphql.Field{
			Type:        graphql.NewList(docType),
			Description: "The search results",
		},
	}

	// Add aggregations field - use typed if provided, otherwise generic
	if aggsType != nil {
		fields["aggregations"] = &graphql.Field{
			Type:        aggsType,
			Description: "Strongly-typed aggregation results",
		}
	} else {
		// Fallback to generic aggregation types
		initGenericAggregationTypes()
		fields["aggregations"] = &graphql.Field{
			Type:        graphql.NewList(GenericAggregationType),
			Description: "Aggregation results as array with dynamic aggregation names",
		}
	}

	resultType := graphql.NewObject(graphql.ObjectConfig{
		Name:   typeName,
		Fields: fields,
	})

	sg.typeCache[typeName] = resultType
	return resultType
}
