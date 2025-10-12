package graphql

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	elasticsearch "github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/typedapi/core/search"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types"
	"github.com/graphql-go/graphql"
	"github.com/reveald/reveald"
)

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

		// Call RequestInterceptor if defined to inject dynamic parameters
		if config.RequestInterceptor != nil {
			httpReq, ok := getHTTPRequest(params)
			if !ok {
				return nil, fmt.Errorf("HTTP request not available in context")
			}
			if err := config.RequestInterceptor(httpReq, request); err != nil {
				return nil, fmt.Errorf("request interceptor failed: %w", err)
			}
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

	// Build dynamic root query if RootQueryBuilder is defined
	var dynamicRootQuery *types.Query
	if config.RootQueryBuilder != nil {
		httpReq, ok := getHTTPRequest(params)
		if !ok {
			return nil, fmt.Errorf("HTTP request not available in context")
		}
		var err error
		dynamicRootQuery, err = config.RootQueryBuilder(httpReq)
		if err != nil {
			return nil, fmt.Errorf("failed to build root query: %w", err)
		}
	}

	// Merge static root query, dynamic root query, and user query
	finalQuery := mergeQueries(config.RootQuery, dynamicRootQuery, userQuery)

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

// BuildPrecompiledResolver creates a resolver function for a precompiled query
func (rb *ResolverBuilder) BuildPrecompiledResolver(queryName string, config *PrecompiledQueryConfig) graphql.FieldResolveFn {
	return func(params graphql.ResolveParams) (any, error) {
		// Extract HTTP request from context for RootQueryBuilder
		httpReq, _ := getHTTPRequest(params)

		// Load the query (from file and/or builder) and merge with root queries
		searchReq, err := config.LoadQuery(params.Args, httpReq)
		if err != nil {
			return nil, fmt.Errorf("failed to load query: %w", err)
		}

		// Execute the search request
		ctx := context.Background()
		if params.Context != nil {
			ctx = params.Context
		}

		// Build ES search
		searchBuilder := rb.esClient.Search()
		for _, idx := range config.GetIndices() {
			searchBuilder = searchBuilder.Index(idx)
		}

		resp, err := searchBuilder.Request(searchReq).Do(ctx)
		if err != nil {
			return nil, fmt.Errorf("search failed: %w", err)
		}

		// Convert ES response with typed aggregations
		return rb.convertPrecompiledESResponseTyped(resp), nil
	}
}

// convertPrecompiledESResponseTyped converts raw ES response to GraphQL format with typed aggregations
func (rb *ResolverBuilder) convertPrecompiledESResponseTyped(resp *search.Response) map[string]any {
	response := map[string]any{
		"totalCount": int64(0),
		"hits":       make([]map[string]any, 0),
	}

	// Parse total hits
	if resp.Hits.Total != nil {
		response["totalCount"] = int64(resp.Hits.Total.Value)
	}

	// Parse hits
	hits := make([]map[string]any, 0)
	for _, hit := range resp.Hits.Hits {
		doc := make(map[string]any)
		doc["id"] = hit.Id_
		if hit.Source_ != nil {
			var source map[string]any
			if err := json.Unmarshal(hit.Source_, &source); err == nil {
				for k, v := range source {
					doc[k] = v
				}
				if _, hasID := doc["id"]; !hasID {
					doc["id"] = hit.Id_
				}
			}
		}
		hits = append(hits, doc)
	}
	response["hits"] = hits

	// Convert aggregations to typed object structure
	if len(resp.Aggregations) > 0 {
		aggObject := rb.convertESAggregatesToObject(resp.Aggregations)
		response["aggregations"] = aggObject
	}

	return response
}

// convertESAggregatesToObject converts ES Aggregates map to typed object structure
func (rb *ResolverBuilder) convertESAggregatesToObject(esAggs map[string]types.Aggregate) map[string]any {
	result := make(map[string]any)

	for aggName, agg := range esAggs {
		result[aggName] = rb.convertAggregateValue(agg)
	}

	return result
}

// convertAggregateValue converts a single ES aggregate to its typed value
func (rb *ResolverBuilder) convertAggregateValue(agg types.Aggregate) any {
	switch v := agg.(type) {
	case *types.StringTermsAggregate:
		return map[string]any{
			"buckets": rb.convertStringTermsBucketsToTyped(v.Buckets),
		}
	case *types.LongTermsAggregate:
		return map[string]any{
			"buckets": rb.convertLongTermsBucketsToTyped(v.Buckets),
		}
	case *types.DateHistogramAggregate:
		return map[string]any{
			"buckets": rb.convertDateHistogramBucketsToTyped(v.Buckets),
		}
	case *types.HistogramAggregate:
		return map[string]any{
			"buckets": rb.convertHistogramBucketsToTyped(v.Buckets),
		}
	case *types.FilterAggregate:
		result := map[string]any{
			"doc_count": int64(v.DocCount),
		}
		// Add nested aggregations as direct properties
		if len(v.Aggregations) > 0 {
			for nestedName, nestedAgg := range v.Aggregations {
				result[nestedName] = rb.convertAggregateValue(nestedAgg)
			}
		}
		return result
	case *types.NestedAggregate:
		result := map[string]any{
			"doc_count": int64(v.DocCount),
		}
		// Add nested aggregations as direct properties
		if len(v.Aggregations) > 0 {
			for nestedName, nestedAgg := range v.Aggregations {
				result[nestedName] = rb.convertAggregateValue(nestedAgg)
			}
		}
		return result
	case *types.FiltersAggregate:
		// Filters aggregation contains named buckets - return as object
		return rb.convertFiltersBucketsToObject(v.Buckets)
	case *types.StatsAggregate:
		stats := map[string]any{
			"count": int64(v.Count),
			"sum":   float64(v.Sum),
		}
		if v.Min != nil {
			stats["min"] = float64(*v.Min)
		}
		if v.Max != nil {
			stats["max"] = float64(*v.Max)
		}
		if v.Avg != nil {
			stats["avg"] = float64(*v.Avg)
		}
		return stats
	case *types.AvgAggregate:
		if v.Value != nil {
			return *v.Value
		}
		return nil
	case *types.SumAggregate:
		if v.Value != nil {
			return *v.Value
		}
		return nil
	case *types.MinAggregate:
		if v.Value != nil {
			return *v.Value
		}
		return nil
	case *types.MaxAggregate:
		if v.Value != nil {
			return *v.Value
		}
		return nil
	case *types.CardinalityAggregate:
		return float64(v.Value)
	}

	// Unknown type - return nil
	return nil
}

// convertStringTermsBucketsToTyped converts string terms buckets to typed format
func (rb *ResolverBuilder) convertStringTermsBucketsToTyped(esBuckets types.BucketsStringTermsBucket) []map[string]any {
	buckets := make([]map[string]any, 0)

	switch v := esBuckets.(type) {
	case []types.StringTermsBucket:
		for _, b := range v {
			bucket := map[string]any{
				"key":       b.Key,
				"doc_count": int64(b.DocCount),
			}
			// Add nested aggregations as direct properties
			if len(b.Aggregations) > 0 {
				for nestedName, nestedAgg := range b.Aggregations {
					bucket[nestedName] = rb.convertAggregateValue(nestedAgg)
				}
			}
			buckets = append(buckets, bucket)
		}
	}

	return buckets
}

// convertLongTermsBucketsToTyped converts long terms buckets to typed format
func (rb *ResolverBuilder) convertLongTermsBucketsToTyped(esBuckets types.BucketsLongTermsBucket) []map[string]any {
	buckets := make([]map[string]any, 0)

	switch v := esBuckets.(type) {
	case []types.LongTermsBucket:
		for _, b := range v {
			bucket := map[string]any{
				"key":       fmt.Sprintf("%d", b.Key),
				"doc_count": int64(b.DocCount),
			}
			// Add nested aggregations as direct properties
			if len(b.Aggregations) > 0 {
				for nestedName, nestedAgg := range b.Aggregations {
					bucket[nestedName] = rb.convertAggregateValue(nestedAgg)
				}
			}
			buckets = append(buckets, bucket)
		}
	}

	return buckets
}

// convertDateHistogramBucketsToTyped converts date histogram buckets to typed format
func (rb *ResolverBuilder) convertDateHistogramBucketsToTyped(esBuckets types.BucketsDateHistogramBucket) []map[string]any {
	buckets := make([]map[string]any, 0)

	switch v := esBuckets.(type) {
	case []types.DateHistogramBucket:
		for _, b := range v {
			bucket := map[string]any{
				"key":       b.KeyAsString,
				"doc_count": int64(b.DocCount),
			}
			// Add nested aggregations as direct properties
			if len(b.Aggregations) > 0 {
				for nestedName, nestedAgg := range b.Aggregations {
					bucket[nestedName] = rb.convertAggregateValue(nestedAgg)
				}
			}
			buckets = append(buckets, bucket)
		}
	}

	return buckets
}

// convertHistogramBucketsToTyped converts histogram buckets to typed format
func (rb *ResolverBuilder) convertHistogramBucketsToTyped(esBuckets types.BucketsHistogramBucket) []map[string]any {
	buckets := make([]map[string]any, 0)

	switch v := esBuckets.(type) {
	case []types.HistogramBucket:
		for _, b := range v {
			bucket := map[string]any{
				"key":       fmt.Sprintf("%v", b.Key),
				"doc_count": int64(b.DocCount),
			}
			// Add nested aggregations as direct properties
			if len(b.Aggregations) > 0 {
				for nestedName, nestedAgg := range b.Aggregations {
					bucket[nestedName] = rb.convertAggregateValue(nestedAgg)
				}
			}
			buckets = append(buckets, bucket)
		}
	}

	return buckets
}

// convertFiltersBucketsToObject converts filters buckets (named) to object
func (rb *ResolverBuilder) convertFiltersBucketsToObject(esBuckets types.BucketsFiltersBucket) map[string]any {
	result := make(map[string]any)

	switch v := esBuckets.(type) {
	case map[string]types.FiltersBucket:
		for filterName, bucket := range v {
			bucketObj := map[string]any{
				"doc_count": int64(bucket.DocCount),
			}
			// Add nested aggregations as direct properties
			if len(bucket.Aggregations) > 0 {
				for nestedName, nestedAgg := range bucket.Aggregations {
					bucketObj[nestedName] = rb.convertAggregateValue(nestedAgg)
				}
			}
			result[filterName] = bucketObj
		}
	}

	return result
}

// getHTTPRequest extracts the HTTP request from GraphQL resolve params context
func getHTTPRequest(params graphql.ResolveParams) (*http.Request, bool) {
	if params.Context == nil {
		return nil, false
	}
	httpReq, ok := params.Context.Value(httpRequestKey).(*http.Request)
	return httpReq, ok
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
