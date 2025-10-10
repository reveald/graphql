package graphql

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/typedapi/core/search"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types"
	"github.com/reveald/reveald"
)

// executeTypedQuery executes an ES query using the typed API and returns a reveald Result
func executeTypedQuery(
	ctx context.Context,
	client *elasticsearch.TypedClient,
	indices []string,
	query *types.Query,
	aggs map[string]types.Aggregations,
	limit *int,
	offset *int,
) (*reveald.Result, error) {
	// Build search request
	req := &search.Request{}

	if query != nil {
		req.Query = query
	}

	if len(aggs) > 0 {
		req.Aggregations = aggs
	}

	// Apply pagination
	if limit != nil {
		req.Size = limit
	}
	if offset != nil {
		req.From = offset
	}

	// Execute search
	searchReq := client.Search()
	for _, idx := range indices {
		searchReq = searchReq.Index(idx)
	}
	resp, err := searchReq.Request(req).Do(ctx)

	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	// Parse response to reveald Result
	result, err := parseESResponse(resp)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ES response: %w", err)
	}

	// Set pagination info
	if result.Pagination != nil {
		if offset != nil {
			result.Pagination.Offset = *offset
		}
		if limit != nil {
			result.Pagination.PageSize = *limit
		}
	}

	return result, nil
}

// parseESResponse parses ES response to reveald Result format
func parseESResponse(resp *search.Response) (*reveald.Result, error) {
	result := &reveald.Result{
		Hits:          make([]map[string]any, 0),
		Aggregations:  make(map[string][]*reveald.ResultBucket),
		TotalHitCount: 0,
		Pagination:    &reveald.ResultPagination{},
	}

	// Parse total hits
	if resp.Hits.Total != nil {
		result.TotalHitCount = int64(resp.Hits.Total.Value)
	}

	// Parse hits
	for _, hit := range resp.Hits.Hits {
		doc := make(map[string]any)

		// Add _id
		doc["id"] = hit.Id_

		// Parse _source
		if hit.Source_ != nil {
			if err := json.Unmarshal(hit.Source_, &doc); err != nil {
				return nil, fmt.Errorf("failed to unmarshal hit source: %w", err)
			}
			// Ensure id is set even if not in source
			if _, hasID := doc["id"]; !hasID {
				doc["id"] = hit.Id_
			}
		}

		result.Hits = append(result.Hits, doc)
	}

	// Parse aggregations
	if len(resp.Aggregations) > 0 {
		aggs, err := parseAggregations(resp.Aggregations)
		if err != nil {
			return nil, fmt.Errorf("failed to parse aggregations: %w", err)
		}
		result.Aggregations = aggs
	}

	return result, nil
}

// parseAggregations parses ES aggregations to reveald format
func parseAggregations(esAggs map[string]types.Aggregate) (map[string][]*reveald.ResultBucket, error) {
	result := make(map[string][]*reveald.ResultBucket)

	for aggName, agg := range esAggs {
		// Try to parse as different aggregation types
		var buckets []*reveald.ResultBucket
		var err error

		// Use type assertions since Aggregate is an any type
		switch v := agg.(type) {
		case *types.StringTermsAggregate:
			buckets, err = parseStringTermsBuckets(v.Buckets)
			if err != nil {
				return nil, fmt.Errorf("failed to parse string terms buckets for %s: %w", aggName, err)
			}
		case *types.LongTermsAggregate:
			buckets, err = parseLongTermsBuckets(v.Buckets)
			if err != nil {
				return nil, fmt.Errorf("failed to parse long terms buckets for %s: %w", aggName, err)
			}
		case *types.DateHistogramAggregate:
			buckets, err = parseDateHistogramBuckets(v.Buckets)
			if err != nil {
				return nil, fmt.Errorf("failed to parse date histogram buckets for %s: %w", aggName, err)
			}
		case *types.HistogramAggregate:
			buckets, err = parseHistogramBuckets(v.Buckets)
			if err != nil {
				return nil, fmt.Errorf("failed to parse histogram buckets for %s: %w", aggName, err)
			}
		case *types.FilterAggregate:
			bucket := &reveald.ResultBucket{
				Value:    aggName,
				HitCount: int64(v.DocCount),
			}
			if len(v.Aggregations) > 0 {
				subAggs, err := parseAggregations(v.Aggregations)
				if err != nil {
					return nil, fmt.Errorf("failed to parse filter sub-aggregations: %w", err)
				}
				bucket.SubResultBuckets = subAggs
			}
			buckets = []*reveald.ResultBucket{bucket}
		case *types.NestedAggregate:
			bucket := &reveald.ResultBucket{
				Value:    aggName,
				HitCount: int64(v.DocCount),
			}
			if len(v.Aggregations) > 0 {
				subAggs, err := parseAggregations(v.Aggregations)
				if err != nil {
					return nil, fmt.Errorf("failed to parse nested sub-aggregations: %w", err)
				}
				bucket.SubResultBuckets = subAggs
			}
			buckets = []*reveald.ResultBucket{bucket}
		case *types.StatsAggregate:
			// Store stats as a single bucket with stats data in Value
			bucket := &reveald.ResultBucket{
				Value:    aggName,
				HitCount: int64(v.Count),
				// Store stats values - we'll handle this specially in the resolver
			}
			buckets = []*reveald.ResultBucket{bucket}
		case *types.AvgAggregate:
			if v.Value != nil {
				bucket := &reveald.ResultBucket{
					Value:    aggName,
					HitCount: int64(*v.Value),
				}
				buckets = []*reveald.ResultBucket{bucket}
			}
		case *types.SumAggregate:
			if v.Value != nil {
				bucket := &reveald.ResultBucket{
					Value:    aggName,
					HitCount: int64(*v.Value),
				}
				buckets = []*reveald.ResultBucket{bucket}
			}
		case *types.MinAggregate:
			if v.Value != nil {
				bucket := &reveald.ResultBucket{
					Value:    aggName,
					HitCount: int64(*v.Value),
				}
				buckets = []*reveald.ResultBucket{bucket}
			}
		case *types.MaxAggregate:
			if v.Value != nil {
				bucket := &reveald.ResultBucket{
					Value:    aggName,
					HitCount: int64(*v.Value),
				}
				buckets = []*reveald.ResultBucket{bucket}
			}
		}

		if len(buckets) > 0 {
			result[aggName] = buckets
		}
	}

	return result, nil
}

// parseStringTermsBuckets parses string terms aggregation buckets
func parseStringTermsBuckets(esBuckets types.BucketsStringTermsBucket) ([]*reveald.ResultBucket, error) {
	buckets := make([]*reveald.ResultBucket, 0)

	// BucketsStringTermsBucket can be []StringTermsBucket or map[string]StringTermsBucket
	switch v := esBuckets.(type) {
	case []types.StringTermsBucket:
		for _, b := range v {
			bucket := &reveald.ResultBucket{
				Value:    b.Key,
				HitCount: int64(b.DocCount),
			}

			if len(b.Aggregations) > 0 {
				subAggs, err := parseAggregations(b.Aggregations)
				if err != nil {
					return nil, fmt.Errorf("failed to parse sub-aggregations: %w", err)
				}
				bucket.SubResultBuckets = subAggs
			}

			buckets = append(buckets, bucket)
		}
	}

	return buckets, nil
}

// parseLongTermsBuckets parses long terms aggregation buckets
func parseLongTermsBuckets(esBuckets types.BucketsLongTermsBucket) ([]*reveald.ResultBucket, error) {
	buckets := make([]*reveald.ResultBucket, 0)

	// BucketsLongTermsBucket can be []LongTermsBucket or map[string]LongTermsBucket
	switch v := esBuckets.(type) {
	case []types.LongTermsBucket:
		for _, b := range v {
			bucket := &reveald.ResultBucket{
				Value:    fmt.Sprintf("%d", b.Key),
				HitCount: int64(b.DocCount),
			}

			if len(b.Aggregations) > 0 {
				subAggs, err := parseAggregations(b.Aggregations)
				if err != nil {
					return nil, fmt.Errorf("failed to parse sub-aggregations: %w", err)
				}
				bucket.SubResultBuckets = subAggs
			}

			buckets = append(buckets, bucket)
		}
	}

	return buckets, nil
}

// parseDateHistogramBuckets parses date histogram aggregation buckets
func parseDateHistogramBuckets(esBuckets types.BucketsDateHistogramBucket) ([]*reveald.ResultBucket, error) {
	buckets := make([]*reveald.ResultBucket, 0)

	// BucketsDateHistogramBucket can be []DateHistogramBucket or map[string]DateHistogramBucket
	switch v := esBuckets.(type) {
	case []types.DateHistogramBucket:
		for _, b := range v {
			bucket := &reveald.ResultBucket{
				Value:    b.KeyAsString,
				HitCount: int64(b.DocCount),
			}

			if len(b.Aggregations) > 0 {
				subAggs, err := parseAggregations(b.Aggregations)
				if err != nil {
					return nil, fmt.Errorf("failed to parse sub-aggregations: %w", err)
				}
				bucket.SubResultBuckets = subAggs
			}

			buckets = append(buckets, bucket)
		}
	}

	return buckets, nil
}

// parseHistogramBuckets parses histogram aggregation buckets
func parseHistogramBuckets(esBuckets types.BucketsHistogramBucket) ([]*reveald.ResultBucket, error) {
	buckets := make([]*reveald.ResultBucket, 0)

	// BucketsHistogramBucket can be []HistogramBucket or map[string]HistogramBucket
	switch v := esBuckets.(type) {
	case []types.HistogramBucket:
		for _, b := range v {
			bucket := &reveald.ResultBucket{
				Value:    fmt.Sprintf("%v", b.Key),
				HitCount: int64(b.DocCount),
			}

			if len(b.Aggregations) > 0 {
				subAggs, err := parseAggregations(b.Aggregations)
				if err != nil {
					return nil, fmt.Errorf("failed to parse sub-aggregations: %w", err)
				}
				bucket.SubResultBuckets = subAggs
			}

			buckets = append(buckets, bucket)
		}
	}

	return buckets, nil
}

// parseRangeBuckets parses range aggregation buckets
func parseRangeBuckets(esBuckets types.BucketsRangeBucket) ([]*reveald.ResultBucket, error) {
	buckets := make([]*reveald.ResultBucket, 0)

	// BucketsRangeBucket can be []RangeBucket or map[string]RangeBucket
	switch v := esBuckets.(type) {
	case []types.RangeBucket:
		for _, b := range v {
			var value string
			if b.Key != nil {
				value = *b.Key
			} else if b.From != nil && b.To != nil {
				value = fmt.Sprintf("%v-%v", *b.From, *b.To)
			} else if b.From != nil {
				value = fmt.Sprintf("%v-*", *b.From)
			} else if b.To != nil {
				value = fmt.Sprintf("*-%v", *b.To)
			}

			bucket := &reveald.ResultBucket{
				Value:    value,
				HitCount: int64(b.DocCount),
			}

			if len(b.Aggregations) > 0 {
				subAggs, err := parseAggregations(b.Aggregations)
				if err != nil {
					return nil, fmt.Errorf("failed to parse sub-aggregations: %w", err)
				}
				bucket.SubResultBuckets = subAggs
			}

			buckets = append(buckets, bucket)
		}
	}

	return buckets, nil
}
