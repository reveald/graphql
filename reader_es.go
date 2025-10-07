package graphql

import "fmt"

// convertToESQueryInput converts GraphQL argument to ESQueryInput
func (rb *ResolverBuilder) convertToESQueryInput(arg any) (*ESQueryInput, error) {
	argMap, ok := arg.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("query argument must be an object")
	}

	input := &ESQueryInput{}

	if term, ok := argMap["term"].(map[string]any); ok {
		input.Term = &ESTermQueryInput{
			Field: term["field"].(string),
			Value: term["value"].(string),
		}
	}

	if terms, ok := argMap["terms"].(map[string]any); ok {
		values := []string{}
		for _, v := range terms["values"].([]any) {
			values = append(values, v.(string))
		}
		input.Terms = &ESTermsQueryInput{
			Field:  terms["field"].(string),
			Values: values,
		}
	}

	if match, ok := argMap["match"].(map[string]any); ok {
		input.Match = &ESMatchQueryInput{
			Field: match["field"].(string),
			Query: match["query"].(string),
		}
	}

	if rangeQ, ok := argMap["range"].(map[string]any); ok {
		rangeInput := &ESRangeQueryInput{
			Field: rangeQ["field"].(string),
		}
		if gte, ok := rangeQ["gte"].(float64); ok {
			rangeInput.Gte = &gte
		}
		if gt, ok := rangeQ["gt"].(float64); ok {
			rangeInput.Gt = &gt
		}
		if lte, ok := rangeQ["lte"].(float64); ok {
			rangeInput.Lte = &lte
		}
		if lt, ok := rangeQ["lt"].(float64); ok {
			rangeInput.Lt = &lt
		}
		input.Range = rangeInput
	}

	if boolQ, ok := argMap["bool"].(map[string]any); ok {
		boolInput := &ESBoolQueryInput{}

		if must, ok := boolQ["must"].([]any); ok {
			for _, m := range must {
				q, err := rb.convertToESQueryInput(m)
				if err != nil {
					return nil, err
				}
				boolInput.Must = append(boolInput.Must, q)
			}
		}

		if should, ok := boolQ["should"].([]any); ok {
			for _, s := range should {
				q, err := rb.convertToESQueryInput(s)
				if err != nil {
					return nil, err
				}
				boolInput.Should = append(boolInput.Should, q)
			}
		}

		if filter, ok := boolQ["filter"].([]any); ok {
			for _, f := range filter {
				q, err := rb.convertToESQueryInput(f)
				if err != nil {
					return nil, err
				}
				boolInput.Filter = append(boolInput.Filter, q)
			}
		}

		if mustNot, ok := boolQ["mustNot"].([]any); ok {
			for _, mn := range mustNot {
				q, err := rb.convertToESQueryInput(mn)
				if err != nil {
					return nil, err
				}
				boolInput.MustNot = append(boolInput.MustNot, q)
			}
		}

		input.Bool = boolInput
	}

	if exists, ok := argMap["exists"].(map[string]any); ok {
		input.Exists = &ESExistsQueryInput{
			Field: exists["field"].(string),
		}
	}

	if nested, ok := argMap["nested"].(map[string]any); ok {
		nestedQuery, err := rb.convertToESQueryInput(nested["query"])
		if err != nil {
			return nil, err
		}
		input.Nested = &ESNestedQueryInput{
			Path:  nested["path"].(string),
			Query: nestedQuery,
		}
	}

	if prefix, ok := argMap["prefix"].(map[string]any); ok {
		input.Prefix = &ESPrefixQueryInput{
			Field: prefix["field"].(string),
			Value: prefix["value"].(string),
		}
	}

	if wildcard, ok := argMap["wildcard"].(map[string]any); ok {
		input.Wildcard = &ESWildcardQueryInput{
			Field: wildcard["field"].(string),
			Value: wildcard["value"].(string),
		}
	}

	return input, nil
}

// convertToESAggInputs converts GraphQL argument to ESAggInput slice
func (rb *ResolverBuilder) convertToESAggInputs(arg any) ([]*ESAggInput, error) {
	argList, ok := arg.([]any)
	if !ok {
		return nil, fmt.Errorf("aggs argument must be an array")
	}

	inputs := make([]*ESAggInput, 0, len(argList))

	for _, item := range argList {
		aggMap, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("each agg must be an object")
		}

		input := &ESAggInput{
			Name: aggMap["name"].(string),
		}

		if terms, ok := aggMap["terms"].(map[string]any); ok {
			termsInput := &ESTermsAggInput{
				Field: terms["field"].(string),
			}
			if size, ok := terms["size"].(int); ok {
				termsInput.Size = &size
			}
			input.Terms = termsInput
		}

		if dateHist, ok := aggMap["dateHistogram"].(map[string]any); ok {
			dhInput := &ESDateHistogramAggInput{
				Field: dateHist["field"].(string),
			}
			if ci, ok := dateHist["calendarInterval"].(string); ok {
				dhInput.CalendarInterval = &ci
			}
			if fi, ok := dateHist["fixedInterval"].(string); ok {
				dhInput.FixedInterval = &fi
			}
			if format, ok := dateHist["format"].(string); ok {
				dhInput.Format = &format
			}
			if mdc, ok := dateHist["minDocCount"].(int); ok {
				dhInput.MinDocCount = &mdc
			}
			input.DateHistogram = dhInput
		}

		if hist, ok := aggMap["histogram"].(map[string]any); ok {
			input.Histogram = &ESHistogramAggInput{
				Field:    hist["field"].(string),
				Interval: hist["interval"].(float64),
			}
		}

		if stats, ok := aggMap["stats"].(map[string]any); ok {
			input.Stats = &ESStatsAggInput{
				Field: stats["field"].(string),
			}
		}

		if avg, ok := aggMap["avg"].(map[string]any); ok {
			input.Avg = &ESAvgAggInput{
				Field: avg["field"].(string),
			}
		}

		if sum, ok := aggMap["sum"].(map[string]any); ok {
			input.Sum = &ESSumAggInput{
				Field: sum["field"].(string),
			}
		}

		if min, ok := aggMap["min"].(map[string]any); ok {
			input.Min = &ESMinAggInput{
				Field: min["field"].(string),
			}
		}

		if max, ok := aggMap["max"].(map[string]any); ok {
			input.Max = &ESMaxAggInput{
				Field: max["field"].(string),
			}
		}

		if card, ok := aggMap["cardinality"].(map[string]any); ok {
			input.Cardinality = &ESCardinalityAggInput{
				Field: card["field"].(string),
			}
		}

		// Sub-aggregations
		if subAggs, ok := aggMap["aggs"].([]any); ok {
			subInputs, err := rb.convertToESAggInputs(subAggs)
			if err != nil {
				return nil, err
			}
			input.Aggs = subInputs
		}

		inputs = append(inputs, input)
	}

	return inputs, nil
}
