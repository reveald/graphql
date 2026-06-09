package graphql

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"

	elasticsearch "github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/typedapi/core/search"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types"
	"github.com/graphql-go/graphql"
	"github.com/reveald/reveald/v2"
)

// EntityTypeMapping tracks the configuration for each entity type
type EntityTypeMapping struct {
	// For regular queries (using reveald features)
	QueryName       string
	QueryConfig     *QueryConfig
	RevealdEndpoint *reveald.Endpoint
	ArgumentReader  *ArgumentReader
	UseFeatureFlow  bool

	// For precompiled queries (using ES typed API)
	PrecompiledConfig *PrecompiledQueryConfig

	// Common fields
	Mapping    *IndexMapping
	EntityKeys []string // The key fields for this entity (e.g., ["id"] or ["id", "conversationId"])

	// NestedPath is set when the entity lives inside an ES nested field.
	// Format: "<nested_field>.<sub_object>" (e.g. "carRelations.car").
	// When set, entity resolution uses a nested query with inner_hits instead of a top-level query.
	NestedPath string
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
		// Always append — even when entity is not found — to preserve the 1-to-1
		// positional mapping that Apollo Federation's DataLoader requires.
		if entity == nil {
			results = append(results, nil)
		} else {
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

	// Get HTTP request from context (for RootQueryBuilder and RequestInterceptor)
	httpReq, _ := getHTTPRequest(params)

	// Nested entities (e.g. Car inside carRelations) need a different resolution path
	var entity map[string]any
	if typeMapping.NestedPath != "" {
		var nestedErr error
		entity, nestedErr = er.resolveNestedEntity(typeMapping, fields, params.Context)
		if nestedErr != nil {
			return nil, nestedErr
		}
	} else {
		query, queryErr := er.buildEntityQuery(typeMapping, fields)
		if queryErr != nil {
			return nil, fmt.Errorf("failed to build entity query: %w", queryErr)
		}
		var resolveErr error
		if typeMapping.UseFeatureFlow {
			entity, resolveErr = er.resolveWithFeatures(typeMapping, query, httpReq, params.Context)
		} else {
			entity, resolveErr = er.resolveWithPrecompiled(typeMapping, query, httpReq, params.Context)
		}
		if resolveErr != nil {
			return nil, resolveErr
		}
	}

	// Merge representation fields into entity (for @requires directive support)
	// The gateway provides additional data in the representation that needs to be available to resolvers
	if entity != nil {
		for key, value := range fields {
			// Don't overwrite key fields that came from ES
			isKeyField := slices.Contains(typeMapping.EntityKeys, key)
			// Don't overwrite __typename
			if key == "__typename" {
				continue
			}
			// Merge non-key fields from representation (e.g., enriched data from @requires)
			if !isKeyField {
				entity[key] = value
			}
		}
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

// resolveNestedEntity resolves an entity that lives inside an ES nested field.
// NestedPath format: "<nested_field>.<sub_object>" (e.g. "carRelations.car").
// It runs a nested query with inner_hits on the parent index and returns the
// sub-object from the first matching inner hit.
func (er *EntityResolver) resolveNestedEntity(typeMapping *EntityTypeMapping, fields map[string]any, ctx context.Context) (map[string]any, error) {
	if er.esClient == nil {
		return nil, fmt.Errorf("ES client not configured - nested entity resolution requires typed ES client")
	}

	// Split "carRelations.car" into nested ES path "carRelations" and sub-object "car"
	dotIdx := strings.Index(typeMapping.NestedPath, ".")
	if dotIdx < 0 {
		return nil, fmt.Errorf("NestedPath %q must be in <nested_field>.<sub_object> format", typeMapping.NestedPath)
	}
	nestedField := typeMapping.NestedPath[:dotIdx]   // "carRelations"
	subObject := typeMapping.NestedPath[dotIdx+1:]   // "car"

	// Build the key filter using fully-qualified field paths (e.g. "carRelations.car.vin")
	keyQuery, err := er.buildNestedEntityKeyQuery(typeMapping, fields, nestedField, subObject)
	if err != nil {
		return nil, fmt.Errorf("failed to build nested entity key query: %w", err)
	}

	// Wrap in a nested query with inner_hits so we get the matching nested doc back
	innerHitsSize := 1
	nestedQuery := &types.Query{
		Nested: &types.NestedQuery{
			Path:  nestedField,
			Query: *keyQuery,
			InnerHits: &types.InnerHits{
				Size: &innerHitsSize,
			},
		},
	}

	if ctx == nil {
		ctx = context.Background()
	}

	resp, err := er.esClient.Search().
		Index(typeMapping.Mapping.IndexName).
		Request(&search.Request{
			Size:  ptr(1),
			Query: nestedQuery,
		}).
		Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("ES nested entity query failed: %w", err)
	}

	if len(resp.Hits.Hits) == 0 {
		return nil, nil
	}

	// Extract the first matching inner hit for our nested field
	innerHitsResult, ok := resp.Hits.Hits[0].InnerHits[nestedField]
	if !ok || len(innerHitsResult.Hits.Hits) == 0 {
		return nil, nil
	}

	innerHit := innerHitsResult.Hits.Hits[0]
	var nestedSource map[string]any
	if innerHit.Source_ != nil {
		if err := json.Unmarshal(innerHit.Source_, &nestedSource); err != nil {
			return nil, fmt.Errorf("failed to parse inner hit source: %w", err)
		}
	}

	// Navigate to the sub-object within the nested doc (e.g. source["car"])
	subObj, ok := nestedSource[subObject]
	if !ok {
		return nil, nil
	}
	entity, ok := subObj.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("nested sub-object %q is not an object", subObject)
	}

	return entity, nil
}

// buildNestedEntityKeyQuery builds a term/bool query for nested entity key fields,
// prefixing each key with the full nested path (e.g. "vin" → "carRelations.car.vin").
func (er *EntityResolver) buildNestedEntityKeyQuery(typeMapping *EntityTypeMapping, fields map[string]any, nestedField, subObject string) (*types.Query, error) {
	prefix := nestedField + "." + subObject // "carRelations.car"

	buildTermQuery := func(key string) (*types.Query, error) {
		value, ok := fields[key]
		if !ok {
			return nil, fmt.Errorf("missing key field: %s", key)
		}
		qualifiedKey := prefix + "." + key // "carRelations.car.vin"
		if f := typeMapping.Mapping.GetField(qualifiedKey); f != nil && f.Type == FieldTypeKeyword {
			qualifiedKey += ".keyword"
		}
		return &types.Query{
			Term: map[string]types.TermQuery{qualifiedKey: {Value: value}},
		}, nil
	}

	if len(typeMapping.EntityKeys) == 1 {
		return buildTermQuery(typeMapping.EntityKeys[0])
	}

	var must []types.Query
	for _, key := range typeMapping.EntityKeys {
		q, err := buildTermQuery(key)
		if err != nil {
			return nil, err
		}
		must = append(must, *q)
	}
	return &types.Query{Bool: &types.BoolQuery{Must: must}}, nil
}

// ptr helper function to create pointer to value
func ptr[T any](v T) *T {
	return &v
}
