package graphql

import (
	"fmt"

	"github.com/elastic/go-elasticsearch/v8/typedapi/types"
)

// convertQueryInput converts GraphQL ESQueryInput to ES Query type
func convertQueryInput(input *ESQueryInput) (*types.Query, error) {
	if input == nil {
		return nil, nil
	}

	query := &types.Query{}
	fieldsSet := 0

	if input.Term != nil {
		query.Term = map[string]types.TermQuery{
			input.Term.Field: {Value: input.Term.Value},
		}
		fieldsSet++
	}

	if input.Terms != nil {
		values := make([]types.FieldValue, len(input.Terms.Values))
		for i, v := range input.Terms.Values {
			values[i] = v
		}
		query.Terms = &types.TermsQuery{
			TermsQuery: map[string]types.TermsQueryField{
				input.Terms.Field: values,
			},
		}
		fieldsSet++
	}

	if input.Match != nil {
		query.Match = map[string]types.MatchQuery{
			input.Match.Field: {Query: input.Match.Query},
		}
		fieldsSet++
	}

	if input.MatchPhrase != nil {
		query.MatchPhrase = map[string]types.MatchPhraseQuery{
			input.MatchPhrase.Field: {Query: input.MatchPhrase.Query},
		}
		fieldsSet++
	}

	if input.MultiMatch != nil {
		mm := types.MultiMatchQuery{
			Query:  input.MultiMatch.Query,
			Fields: input.MultiMatch.Fields,
		}
		// Type is optional, skip for now (would need proper enum conversion)
		query.MultiMatch = &mm
		fieldsSet++
	}

	if input.Range != nil {
		rangeQuery := types.NumberRangeQuery{}
		if input.Range.Gte != nil {
			val := types.Float64(*input.Range.Gte)
			rangeQuery.Gte = &val
		}
		if input.Range.Gt != nil {
			val := types.Float64(*input.Range.Gt)
			rangeQuery.Gt = &val
		}
		if input.Range.Lte != nil {
			val := types.Float64(*input.Range.Lte)
			rangeQuery.Lte = &val
		}
		if input.Range.Lt != nil {
			val := types.Float64(*input.Range.Lt)
			rangeQuery.Lt = &val
		}
		query.Range = map[string]types.RangeQuery{
			input.Range.Field: rangeQuery,
		}
		fieldsSet++
	}

	if input.Bool != nil {
		boolQuery := &types.BoolQuery{}

		if len(input.Bool.Must) > 0 {
			must := make([]types.Query, len(input.Bool.Must))
			for i, q := range input.Bool.Must {
				converted, err := convertQueryInput(q)
				if err != nil {
					return nil, fmt.Errorf("failed to convert must query: %w", err)
				}
				if converted != nil {
					must[i] = *converted
				}
			}
			boolQuery.Must = must
		}

		if len(input.Bool.Should) > 0 {
			should := make([]types.Query, len(input.Bool.Should))
			for i, q := range input.Bool.Should {
				converted, err := convertQueryInput(q)
				if err != nil {
					return nil, fmt.Errorf("failed to convert should query: %w", err)
				}
				if converted != nil {
					should[i] = *converted
				}
			}
			boolQuery.Should = should
		}

		if len(input.Bool.Filter) > 0 {
			filter := make([]types.Query, len(input.Bool.Filter))
			for i, q := range input.Bool.Filter {
				converted, err := convertQueryInput(q)
				if err != nil {
					return nil, fmt.Errorf("failed to convert filter query: %w", err)
				}
				if converted != nil {
					filter[i] = *converted
				}
			}
			boolQuery.Filter = filter
		}

		if len(input.Bool.MustNot) > 0 {
			mustNot := make([]types.Query, len(input.Bool.MustNot))
			for i, q := range input.Bool.MustNot {
				converted, err := convertQueryInput(q)
				if err != nil {
					return nil, fmt.Errorf("failed to convert must_not query: %w", err)
				}
				if converted != nil {
					mustNot[i] = *converted
				}
			}
			boolQuery.MustNot = mustNot
		}

		query.Bool = boolQuery
		fieldsSet++
	}

	if input.Exists != nil {
		query.Exists = &types.ExistsQuery{Field: input.Exists.Field}
		fieldsSet++
	}

	if input.Nested != nil {
		nestedQuery, err := convertQueryInput(input.Nested.Query)
		if err != nil {
			return nil, fmt.Errorf("failed to convert nested query: %w", err)
		}
		query.Nested = &types.NestedQuery{
			Path:  input.Nested.Path,
			Query: *nestedQuery,
		}
		fieldsSet++
	}

	if input.Prefix != nil {
		query.Prefix = map[string]types.PrefixQuery{
			input.Prefix.Field: {Value: input.Prefix.Value},
		}
		fieldsSet++
	}

	if input.Wildcard != nil {
		value := input.Wildcard.Value
		query.Wildcard = map[string]types.WildcardQuery{
			input.Wildcard.Field: {Value: &value},
		}
		fieldsSet++
	}

	if input.QueryString != nil {
		qs := &types.QueryStringQuery{Query: input.QueryString.Query}
		if input.QueryString.DefaultField != nil {
			qs.DefaultField = input.QueryString.DefaultField
		}
		if len(input.QueryString.Fields) > 0 {
			qs.Fields = input.QueryString.Fields
		}
		// DefaultOperator is optional, skip for now (would need proper enum conversion)
		query.QueryString = qs
		fieldsSet++
	}

	if fieldsSet == 0 {
		return nil, fmt.Errorf("no query type specified")
	}
	if fieldsSet > 1 {
		return nil, fmt.Errorf("multiple query types specified, only one allowed")
	}

	return query, nil
}

// convertAggsInput converts GraphQL ESAggInput to ES Aggregations map
func convertAggsInput(inputs []*ESAggInput) (map[string]types.Aggregations, error) {
	if len(inputs) == 0 {
		return nil, nil
	}

	aggs := make(map[string]types.Aggregations)

	for _, input := range inputs {
		if input.Name == "" {
			return nil, fmt.Errorf("aggregation name is required")
		}

		agg := types.Aggregations{}
		fieldsSet := 0

		if input.Terms != nil {
			termsAgg := &types.TermsAggregation{Field: &input.Terms.Field}
			if input.Terms.Size != nil {
				termsAgg.Size = input.Terms.Size
			}
			// Order is complex, skip for now
			agg.Terms = termsAgg
			fieldsSet++
		}

		if input.DateHistogram != nil {
			dhAgg := &types.DateHistogramAggregation{Field: &input.DateHistogram.Field}
			// CalendarInterval and FixedInterval need proper enum conversion, skip for now
			if input.DateHistogram.Format != nil {
				dhAgg.Format = input.DateHistogram.Format
			}
			if input.DateHistogram.MinDocCount != nil {
				dhAgg.MinDocCount = input.DateHistogram.MinDocCount
			}
			agg.DateHistogram = dhAgg
			fieldsSet++
		}

		if input.Histogram != nil {
			interval := types.Float64(input.Histogram.Interval)
			agg.Histogram = &types.HistogramAggregation{
				Field:    &input.Histogram.Field,
				Interval: &interval,
			}
			fieldsSet++
		}

		if input.Range != nil {
			ranges := make([]types.AggregationRange, len(input.Range.Ranges))
			for i, r := range input.Range.Ranges {
				ar := types.AggregationRange{}
				if r.From != nil {
					ar.From = (*types.Float64)(r.From)
				}
				if r.To != nil {
					ar.To = (*types.Float64)(r.To)
				}
				if r.Key != nil {
					ar.Key = r.Key
				}
				ranges[i] = ar
			}
			agg.Range = &types.RangeAggregation{
				Field:  &input.Range.Field,
				Ranges: ranges,
			}
			fieldsSet++
		}

		if input.Stats != nil {
			agg.Stats = &types.StatsAggregation{Field: &input.Stats.Field}
			fieldsSet++
		}

		if input.Avg != nil {
			agg.Avg = &types.AverageAggregation{Field: &input.Avg.Field}
			fieldsSet++
		}

		if input.Sum != nil {
			agg.Sum = &types.SumAggregation{Field: &input.Sum.Field}
			fieldsSet++
		}

		if input.Min != nil {
			agg.Min = &types.MinAggregation{Field: &input.Min.Field}
			fieldsSet++
		}

		if input.Max != nil {
			agg.Max = &types.MaxAggregation{Field: &input.Max.Field}
			fieldsSet++
		}

		if input.Cardinality != nil {
			agg.Cardinality = &types.CardinalityAggregation{Field: &input.Cardinality.Field}
			fieldsSet++
		}

		if input.Nested != nil {
			agg.Nested = &types.NestedAggregation{Path: &input.Nested.Path}
			fieldsSet++
		}

		if input.Filter != nil {
			filterQuery, err := convertQueryInput(input.Filter.Query)
			if err != nil {
				return nil, fmt.Errorf("failed to convert filter query: %w", err)
			}
			agg.Filter = filterQuery
			fieldsSet++
		}

		if fieldsSet == 0 {
			return nil, fmt.Errorf("no aggregation type specified for %s", input.Name)
		}
		if fieldsSet > 1 {
			return nil, fmt.Errorf("multiple aggregation types specified for %s, only one allowed", input.Name)
		}

		// Handle sub-aggregations
		if len(input.Aggs) > 0 {
			subAggs, err := convertAggsInput(input.Aggs)
			if err != nil {
				return nil, fmt.Errorf("failed to convert sub-aggregations: %w", err)
			}
			agg.Aggregations = subAggs
		}

		aggs[input.Name] = agg
	}

	return aggs, nil
}

// mergeQueries merges a root query with a user query using bool must
func mergeQueries(rootQuery, userQuery *types.Query) *types.Query {
	if rootQuery == nil {
		return userQuery
	}
	if userQuery == nil {
		return rootQuery
	}

	// Create a bool query with both as must clauses
	return &types.Query{
		Bool: &types.BoolQuery{
			Must: []types.Query{*rootQuery, *userQuery},
		},
	}
}
