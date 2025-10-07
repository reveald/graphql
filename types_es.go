package graphql

// ESQueryInput represents an Elasticsearch query input for GraphQL
// Only one query type field should be set at a time
type ESQueryInput struct {
	Term       *ESTermQueryInput
	Terms      *ESTermsQueryInput
	Match      *ESMatchQueryInput
	MatchPhrase *ESMatchPhraseQueryInput
	MultiMatch *ESMultiMatchQueryInput
	Range      *ESRangeQueryInput
	Bool       *ESBoolQueryInput
	Exists     *ESExistsQueryInput
	Nested     *ESNestedQueryInput
	Prefix     *ESPrefixQueryInput
	Wildcard   *ESWildcardQueryInput
	QueryString *ESQueryStringInput
}

// ESTermQueryInput represents a term query
type ESTermQueryInput struct {
	Field string
	Value string
}

// ESTermsQueryInput represents a terms query
type ESTermsQueryInput struct {
	Field  string
	Values []string
}

// ESMatchQueryInput represents a match query
type ESMatchQueryInput struct {
	Field string
	Query string
}

// ESMatchPhraseQueryInput represents a match_phrase query
type ESMatchPhraseQueryInput struct {
	Field string
	Query string
}

// ESMultiMatchQueryInput represents a multi_match query
type ESMultiMatchQueryInput struct {
	Query  string
	Fields []string
	Type   *string // best_fields, most_fields, cross_fields, phrase, etc.
}

// ESRangeQueryInput represents a range query
type ESRangeQueryInput struct {
	Field string
	Gte   *float64 // greater than or equal
	Gt    *float64 // greater than
	Lte   *float64 // less than or equal
	Lt    *float64 // less than
}

// ESBoolQueryInput represents a bool query
type ESBoolQueryInput struct {
	Must    []*ESQueryInput
	Should  []*ESQueryInput
	Filter  []*ESQueryInput
	MustNot []*ESQueryInput
}

// ESExistsQueryInput represents an exists query
type ESExistsQueryInput struct {
	Field string
}

// ESNestedQueryInput represents a nested query
type ESNestedQueryInput struct {
	Path  string
	Query *ESQueryInput
}

// ESPrefixQueryInput represents a prefix query
type ESPrefixQueryInput struct {
	Field string
	Value string
}

// ESWildcardQueryInput represents a wildcard query
type ESWildcardQueryInput struct {
	Field string
	Value string
}

// ESQueryStringInput represents a query_string query
type ESQueryStringInput struct {
	Query          string
	DefaultField   *string
	Fields         []string
	DefaultOperator *string // AND or OR
}

// ESAggInput represents an Elasticsearch aggregation input for GraphQL
type ESAggInput struct {
	Name           string
	Terms          *ESTermsAggInput
	DateHistogram  *ESDateHistogramAggInput
	Histogram      *ESHistogramAggInput
	Range          *ESRangeAggInput
	Stats          *ESStatsAggInput
	Avg            *ESAvgAggInput
	Sum            *ESSumAggInput
	Min            *ESMinAggInput
	Max            *ESMaxAggInput
	Cardinality    *ESCardinalityAggInput
	Nested         *ESNestedAggInput
	Filter         *ESFilterAggInput
	Aggs           []*ESAggInput // sub-aggregations
}

// ESTermsAggInput represents a terms aggregation
type ESTermsAggInput struct {
	Field string
	Size  *int
	Order *ESAggOrderInput
}

// ESAggOrderInput represents aggregation ordering
type ESAggOrderInput struct {
	Key   string // _count, _key, or a metric name
	Order string // asc or desc
}

// ESDateHistogramAggInput represents a date_histogram aggregation
type ESDateHistogramAggInput struct {
	Field            string
	CalendarInterval *string // 1d, 1w, 1M, etc.
	FixedInterval    *string // 30s, 1h, etc.
	Format           *string
	MinDocCount      *int
}

// ESHistogramAggInput represents a histogram aggregation
type ESHistogramAggInput struct {
	Field    string
	Interval float64
}

// ESRangeAggInput represents a range aggregation
type ESRangeAggInput struct {
	Field  string
	Ranges []*ESRangeInput
}

// ESRangeInput represents a single range
type ESRangeInput struct {
	From *float64
	To   *float64
	Key  *string
}

// ESStatsAggInput represents a stats aggregation
type ESStatsAggInput struct {
	Field string
}

// ESAvgAggInput represents an avg aggregation
type ESAvgAggInput struct {
	Field string
}

// ESSumAggInput represents a sum aggregation
type ESSumAggInput struct {
	Field string
}

// ESMinAggInput represents a min aggregation
type ESMinAggInput struct {
	Field string
}

// ESMaxAggInput represents a max aggregation
type ESMaxAggInput struct {
	Field string
}

// ESCardinalityAggInput represents a cardinality aggregation
type ESCardinalityAggInput struct {
	Field string
}

// ESNestedAggInput represents a nested aggregation
type ESNestedAggInput struct {
	Path string
}

// ESFilterAggInput represents a filter aggregation
type ESFilterAggInput struct {
	Query *ESQueryInput
}
