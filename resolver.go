package graphql

import (
	"context"
	"fmt"
	"strings"

	elasticsearch "github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types"
	"github.com/graphql-go/graphql"
	"github.com/reveald/reveald"
)

// ResolverFunc is a function that creates a GraphQL field resolver
type ResolverFunc func(queryName string, config *QueryConfig) graphql.FieldResolveFn

// ResolverBuilder creates resolvers that execute reveald queries
type ResolverBuilder struct {
	backend  reveald.Backend
	esClient *elasticsearch.TypedClient
	reader   *ArgumentReader
}

// NewResolverBuilder creates a new resolver builder
func NewResolverBuilder(backend reveald.Backend, mapping *IndexMapping, esClient *elasticsearch.TypedClient) *ResolverBuilder {
	return &ResolverBuilder{
		backend:  backend,
		esClient: esClient,
		reader:   NewArgumentReader(mapping),
	}
}

// BuildResolver creates a resolver function for a query
func (rb *ResolverBuilder) BuildResolver(queryName string, config *QueryConfig) graphql.FieldResolveFn {
	// Create the endpoint with configured indices and features
	endpoint := reveald.NewEndpoint(rb.backend, reveald.WithIndices(config.GetIndices()...))

	if err := endpoint.Register(config.Features...); err != nil {
		panic(fmt.Sprintf("failed to register features for query %s: %v", queryName, err))
	}

	return func(params graphql.ResolveParams) (any, error) {
		// Check if this is an ES typed query
		if config.EnableElasticQuerying && rb.esClient != nil {
			if queryArg, hasQuery := params.Args["query"]; hasQuery && queryArg != nil {
				return rb.executeTypedESQuery(params, config)
			}
		}

		// Default: use Feature-based flow
		// Convert GraphQL arguments to reveald Request
		request, err := rb.reader.Read(params)
		if err != nil {
			return nil, fmt.Errorf("failed to read arguments: %w", err)
		}

		// Execute the query
		result, err := endpoint.Execute(context.Background(), request)
		if err != nil {
			return nil, fmt.Errorf("failed to execute query: %w", err)
		}

		// Convert reveald Result to GraphQL response
		return rb.convertResult(result, config), nil
	}
}

// executeTypedESQuery handles typed Elasticsearch queries
func (rb *ResolverBuilder) executeTypedESQuery(params graphql.ResolveParams, config *QueryConfig) (any, error) {
	// Convert GraphQL query argument to ES Query
	var userQuery *types.Query
	if queryArg, ok := params.Args["query"]; ok && queryArg != nil {
		queryInput, err := rb.convertToESQueryInput(queryArg)
		if err != nil {
			return nil, fmt.Errorf("failed to convert query input: %w", err)
		}
		userQuery, err = convertQueryInput(queryInput)
		if err != nil {
			return nil, fmt.Errorf("failed to convert query: %w", err)
		}
	}

	// Merge with root query if provided
	finalQuery := mergeQueries(config.RootQuery, userQuery)

	// Convert GraphQL aggs argument to ES Aggregations
	var aggs map[string]types.Aggregations
	if aggsArg, ok := params.Args["aggs"]; ok && aggsArg != nil {
		aggsInputs, err := rb.convertToESAggInputs(aggsArg)
		if err != nil {
			return nil, fmt.Errorf("failed to convert aggs input: %w", err)
		}
		aggs, err = convertAggsInput(aggsInputs)
		if err != nil {
			return nil, fmt.Errorf("failed to convert aggregations: %w", err)
		}
	}

	// Extract pagination params
	var limit, offset *int
	if limitArg, ok := params.Args["limit"].(int); ok {
		limit = &limitArg
	}
	if offsetArg, ok := params.Args["offset"].(int); ok {
		offset = &offsetArg
	}

	// Execute typed query
	result, err := executeTypedQuery(
		context.Background(),
		rb.esClient,
		config.GetIndices(),
		finalQuery,
		aggs,
		limit,
		offset,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to execute typed query: %w", err)
	}

	// Convert to GraphQL response
	return rb.convertResult(result, config), nil
}

// convertResult converts a reveald Result to a GraphQL response
func (rb *ResolverBuilder) convertResult(result *reveald.Result, config *QueryConfig) map[string]any {
	response := map[string]any{
		"hits":       result.Hits,
		"totalCount": result.TotalHitCount,
	}

	// Add aggregations if enabled
	if config.EnableAggregations && len(result.Aggregations) > 0 {
		aggResponse := make(map[string]any)
		for aggName, buckets := range result.Aggregations {
			// Convert dots to underscores for GraphQL field names
			gqlAggName := replaceDotsWithUnderscores(aggName)

			bucketsResponse := convertBucketsWithPath(buckets, "")
			aggResponse[gqlAggName] = bucketsResponse
		}
		response["aggregations"] = aggResponse
	}

	// Add pagination if enabled
	if config.EnablePagination && result.Pagination != nil {
		response["pagination"] = map[string]any{
			"offset":     result.Pagination.Offset,
			"limit":      result.Pagination.PageSize,
			"totalCount": result.TotalHitCount,
		}
	}

	return response
}

// GetResolverFunc returns a ResolverFunc for this builder
func (rb *ResolverBuilder) GetResolverFunc() ResolverFunc {
	return rb.BuildResolver
}

// replaceDotsWithUnderscores converts ES field names to GraphQL field names
func replaceDotsWithUnderscores(s string) string {
	return strings.ReplaceAll(s, ".", "_")
}

// convertBucketsWithPath recursively converts reveald buckets to GraphQL response format
// parentPath tracks the hierarchical path for nested buckets
func convertBucketsWithPath(buckets []*reveald.ResultBucket, parentPath string) []map[string]any {
	bucketsResponse := make([]map[string]any, 0, len(buckets))

	for _, bucket := range buckets {
		valueStr := fmt.Sprintf("%v", bucket.Value)

		// Build the full hierarchical filter value
		filterValue := valueStr
		if parentPath != "" {
			filterValue = parentPath + ">" + valueStr
		}

		bucketMap := map[string]any{
			"value":       valueStr,
			"count":       bucket.HitCount,
			"filterValue": filterValue,
		}

		// Convert sub-buckets if present
		if len(bucket.SubResultBuckets) > 0 {
			subBuckets := make([]map[string]any, 0)

			// Flatten all sub-aggregations into a single array with updated paths
			for _, subBucketList := range bucket.SubResultBuckets {
				converted := convertBucketsWithPath(subBucketList, filterValue)
				subBuckets = append(subBuckets, converted...)
			}

			if len(subBuckets) > 0 {
				bucketMap["buckets"] = subBuckets
			}
		}

		bucketsResponse = append(bucketsResponse, bucketMap)
	}

	return bucketsResponse
}
