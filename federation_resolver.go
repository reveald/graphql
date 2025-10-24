package graphql

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	elasticsearch "github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/typedapi/core/search"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types"
	"github.com/graphql-go/graphql"
	"github.com/reveald/reveald/v2"
)

// EntityTypeMapping tracks the configuration for each entity type
type EntityTypeMapping struct {
	// For regular queries (using reveald features)
	QueryName        string
	QueryConfig      *QueryConfig
	RevealdEndpoint  *reveald.Endpoint
	ArgumentReader   *ArgumentReader
	UseFeatureFlow   bool

	// For precompiled queries (using ES typed API)
	PrecompiledConfig *PrecompiledQueryConfig

	// Common fields
	Mapping    *IndexMapping
	EntityKeys []string // The key fields for this entity (e.g., ["id"] or ["id", "conversationId"])
}

// EntityResolver resolves entities for Apollo Federation
type EntityResolver struct {
	esClient     *elasticsearch.TypedClient
	backend      reveald.Backend
	typeMappings map[string]*EntityTypeMapping // typename -> config mapping
}

// NewEntityResolver creates a new entity resolver
func NewEntityResolver(esClient *elasticsearch.TypedClient, backend reveald.Backend) *EntityResolver {
	return &EntityResolver{
		esClient:     esClient,
		backend:      backend,
		typeMappings: make(map[string]*EntityTypeMapping),
	}
}

// RegisterEntityType registers an entity type with its configuration
func (er *EntityResolver) RegisterEntityType(typename string, mapping *EntityTypeMapping) {
	er.typeMappings[typename] = mapping
}

// ResolveEntities resolves a list of entity representations
// This is the resolver for the _entities query
func (er *EntityResolver) ResolveEntities(params graphql.ResolveParams) (any, error) {
	representations, ok := params.Args["representations"].([]any)
	if !ok {
		return nil, fmt.Errorf("representations argument must be a list")
	}

	// Resolve each entity
	var results []any
	for _, repr := range representations {
		entity, err := er.resolveEntity(repr, params)
		if err != nil {
			// Log error but continue with other entities
			// Apollo Federation expects partial results
			results = append(results, map[string]any{
				"__typename": "Error",
				"message":    err.Error(),
			})
			continue
		}
		if entity != nil {
			results = append(results, entity)
		}
	}

	return results, nil
}

// resolveEntity resolves a single entity representation
func (er *EntityResolver) resolveEntity(repr any, params graphql.ResolveParams) (map[string]any, error) {
	// Parse the representation
	typename, fields, err := ParseEntityRepresentation(repr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse representation: %w", err)
	}

	// Find the type mapping
	typeMapping, ok := er.typeMappings[typename]
	if !ok {
		return nil, fmt.Errorf("unknown entity type: %s", typename)
	}

	// Build ES query from key fields
	query, err := er.buildEntityQuery(typeMapping, fields)
	if err != nil {
		return nil, fmt.Errorf("failed to build entity query: %w", err)
	}

	// Get HTTP request from context (for RootQueryBuilder and RequestInterceptor)
	httpReq, _ := getHTTPRequest(params)

	// Execute query based on whether it's a feature-based or precompiled query
	var entity map[string]any
	if typeMapping.UseFeatureFlow {
		entity, err = er.resolveWithFeatures(typeMapping, query, httpReq, params.Context)
	} else {
		entity, err = er.resolveWithPrecompiled(typeMapping, query, httpReq, params.Context)
	}

	if err != nil {
		return nil, err
	}

	// Add __typename to the result
	if entity != nil {
		entity["__typename"] = typename
	}

	return entity, nil
}

// buildEntityQuery builds an Elasticsearch query from entity key fields
func (er *EntityResolver) buildEntityQuery(typeMapping *EntityTypeMapping, fields map[string]any) (*types.Query, error) {
	if len(typeMapping.EntityKeys) == 0 {
		return nil, fmt.Errorf("entity has no key fields defined")
	}

	// If single key field, use terms query
	if len(typeMapping.EntityKeys) == 1 {
		keyField := typeMapping.EntityKeys[0]
		keyValue, ok := fields[keyField]
		if !ok {
			return nil, fmt.Errorf("missing key field: %s", keyField)
		}

		// Use .keyword suffix for text fields if needed
		queryField := keyField
		if field := typeMapping.Mapping.GetField(keyField); field != nil {
			if field.Type == FieldTypeText {
				if _, hasKeyword := field.Fields["keyword"]; hasKeyword {
					queryField = keyField + ".keyword"
				}
			} else if field.Type == FieldTypeKeyword {
				queryField = keyField + ".keyword"
			}
		}

		return &types.Query{
			Term: map[string]types.TermQuery{
				queryField: {Value: keyValue},
			},
		}, nil
	}

	// Multiple key fields - use bool query with must
	var mustQueries []types.Query
	for _, keyField := range typeMapping.EntityKeys {
		keyValue, ok := fields[keyField]
		if !ok {
			return nil, fmt.Errorf("missing key field: %s", keyField)
		}

		// Use .keyword suffix for text fields if needed
		queryField := keyField
		if field := typeMapping.Mapping.GetField(keyField); field != nil {
			if field.Type == FieldTypeText {
				if _, hasKeyword := field.Fields["keyword"]; hasKeyword {
					queryField = keyField + ".keyword"
				}
			} else if field.Type == FieldTypeKeyword {
				queryField = keyField + ".keyword"
			}
		}

		mustQueries = append(mustQueries, types.Query{
			Term: map[string]types.TermQuery{
				queryField: {Value: keyValue},
			},
		})
	}

	return &types.Query{
		Bool: &types.BoolQuery{
			Must: mustQueries,
		},
	}, nil
}

// resolveWithFeatures resolves entity using reveald features (for regular queries)
func (er *EntityResolver) resolveWithFeatures(typeMapping *EntityTypeMapping, entityQuery *types.Query, httpReq *http.Request, ctx context.Context) (map[string]any, error) {
	// Create a minimal reveald request
	request := reveald.NewRequest()

	// Apply RequestInterceptor if defined
	if typeMapping.QueryConfig.RequestInterceptor != nil && httpReq != nil {
		if err := typeMapping.QueryConfig.RequestInterceptor(httpReq, request); err != nil {
			return nil, fmt.Errorf("request interceptor failed: %w", err)
		}
	}

	// For entity resolution, we need to use typed ES query since reveald endpoints
	// don't directly support merging arbitrary ES queries
	// This ensures RootQueryBuilder and RequestInterceptor are still applied
	return er.resolveWithTypedQuery(typeMapping, entityQuery, httpReq, ctx)
}

// resolveWithPrecompiled resolves entity using precompiled query config
func (er *EntityResolver) resolveWithPrecompiled(typeMapping *EntityTypeMapping, entityQuery *types.Query, httpReq *http.Request, ctx context.Context) (map[string]any, error) {
	return er.resolveWithTypedQuery(typeMapping, entityQuery, httpReq, ctx)
}

// resolveWithTypedQuery resolves entity using Elasticsearch typed API
func (er *EntityResolver) resolveWithTypedQuery(typeMapping *EntityTypeMapping, entityQuery *types.Query, httpReq *http.Request, ctx context.Context) (map[string]any, error) {
	if er.esClient == nil {
		return nil, fmt.Errorf("ES client not configured - entity resolution requires typed ES client")
	}

	// Build dynamic root query if RootQueryBuilder is defined
	var dynamicRootQuery *types.Query
	var staticRootQuery *types.Query

	if typeMapping.UseFeatureFlow && typeMapping.QueryConfig != nil {
		staticRootQuery = typeMapping.QueryConfig.RootQuery
		if typeMapping.QueryConfig.RootQueryBuilder != nil && httpReq != nil {
			var err error
			dynamicRootQuery, err = typeMapping.QueryConfig.RootQueryBuilder(httpReq)
			if err != nil {
				return nil, fmt.Errorf("failed to build root query: %w", err)
			}
		}
	} else if typeMapping.PrecompiledConfig != nil {
		if typeMapping.PrecompiledConfig.RootQueryBuilder != nil && httpReq != nil {
			var err error
			dynamicRootQuery, err = typeMapping.PrecompiledConfig.RootQueryBuilder(httpReq)
			if err != nil {
				return nil, fmt.Errorf("failed to build root query: %w", err)
			}
		}
	}

	// Merge all queries: static root + dynamic root + entity query
	finalQuery := mergeQueries(staticRootQuery, dynamicRootQuery, entityQuery)

	// Execute the query
	if ctx == nil {
		ctx = context.Background()
	}

	resp, err := er.esClient.Search().
		Index(typeMapping.Mapping.IndexName).
		Request(&search.Request{
			Size:  ptr(1),
			Query: finalQuery,
		}).
		Do(ctx)

	if err != nil {
		return nil, fmt.Errorf("ES query failed: %w", err)
	}

	// Parse the response
	if resp.Hits.Total == nil || resp.Hits.Total.Value == 0 {
		return nil, nil // Entity not found
	}

	if len(resp.Hits.Hits) == 0 {
		return nil, nil
	}

	// Parse the first hit
	hit := resp.Hits.Hits[0]
	var source map[string]any
	if hit.Source_ != nil {
		if err := json.Unmarshal(hit.Source_, &source); err != nil {
			return nil, fmt.Errorf("failed to parse hit source: %w", err)
		}
	}

	// Add id field if not present
	if _, hasID := source["id"]; !hasID {
		source["id"] = hit.Id_
	}

	// Normalize objects to arrays (same as regular queries)
	normalizeObjectsToArrays(source, typeMapping.Mapping)

	return source, nil
}

// ptr helper function to create pointer to value
func ptr[T any](v T) *T {
	return &v
}
