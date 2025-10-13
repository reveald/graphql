package main

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	elasticsearch "github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/typedapi/core/search"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types/enums/calendarinterval"
	"github.com/graphql-go/graphql"
	"github.com/reveald/reveald"
	revealdgraphql "github.com/wayke-se/reveald-graphql"
)

func main() {
	// Create Elasticsearch backend
	backend, err := reveald.NewElasticBackend([]string{"http://localhost:9200"})
	if err != nil {
		log.Fatalf("Failed to create backend: %v", err)
	}

	// Load the Elasticsearch mapping (for leads index)
	// In a real application, you would load this from a file
	mapping := revealdgraphql.IndexMapping{
		IndexName: "leads",
		Properties: map[string]*revealdgraphql.Field{
			"id":               {Name: "id", Type: revealdgraphql.FieldTypeKeyword},
			"leadType":         {Name: "leadType", Type: revealdgraphql.FieldTypeKeyword},
			"createdAt":        {Name: "createdAt", Type: revealdgraphql.FieldTypeDate},
			"branchMarketCode": {Name: "branchMarketCode", Type: revealdgraphql.FieldTypeKeyword},
		},
	}

	// Configure the GraphQL API
	config := revealdgraphql.NewConfig(mapping)

	// Add a precompiled query with complex aggregations
	config.AddPrecompiledQuery("leadsOverview", &revealdgraphql.PrecompiledQueryConfig{
		Description:  "Leads overview with statistics by type and mechanism",
		QueryBuilder: buildLeadsOverviewQuery,
		Parameters: graphql.FieldConfigArgument{
			"market": &graphql.ArgumentConfig{
				Type:        graphql.NewList(graphql.String),
				Description: "Filter by market codes (e.g., ['SE', 'NO'])",
			},
		},
		SampleParameters: map[string]any{
			"market": []string{"SE"},
		},
	})

	// Create Elasticsearch typed client for precompiled querying
	esClient, err := elasticsearch.NewTypedClient(elasticsearch.Config{
		Addresses: []string{"http://localhost:9200"},
	})
	if err != nil {
		log.Fatalf("Failed to create ES client: %v", err)
	}

	// Create the GraphQL API with ES client option
	api, err := revealdgraphql.New(backend, config, revealdgraphql.WithESClient(esClient))
	if err != nil {
		log.Fatalf("Failed to create GraphQL API: %v", err)
	}

	// Start the HTTP server
	fmt.Println("GraphQL server starting on http://localhost:8080/graphql")
	fmt.Println("Open http://localhost:8080/graphql in your browser to use GraphiQL")
	fmt.Println()
	fmt.Println("Example query:")
	fmt.Println(`
query {
  leadsOverview(market: ["SE", "NO"]) {
    totalCount
    aggregations {
      by_leadType {
        value
        count
        by_mechanism {
          value
          count
          periods {
            last_24h {
              count
            }
            prev_24h {
              count
            }
            last_7d {
              count
            }
          }
          last_30d_daily {
            count
            per_day {
              date
              count
            }
          }
        }
      }
    }
  }
}`)

	if err := http.ListenAndServe(":8080", api); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

// buildLeadsOverviewQuery builds the complex leads overview query
func buildLeadsOverviewQuery(args map[string]any) *search.Request {
	// Extract market parameter
	markets := []string{}
	if marketArg, ok := args["market"].([]any); ok {
		for _, m := range marketArg {
			if ms, ok := m.(string); ok {
				markets = append(markets, strings.ToUpper(ms))
			}
		}
	}

	// Base query - last 2 months
	baseQuery := &types.Query{
		Range: map[string]types.RangeQuery{
			"createdAt": types.DateRangeQuery{
				Gte:      ptr("now-2M"),
				Lt:       ptr("now"),
				TimeZone: ptr("Europe/Stockholm"),
			},
		},
	}

	// Add market filter if specified
	var finalQuery *types.Query
	if len(markets) > 0 {
		finalQuery = &types.Query{
			Bool: &types.BoolQuery{
				Must: []types.Query{
					*baseQuery,
					{
						Terms: &types.TermsQuery{
							TermsQuery: map[string]types.TermsQueryField{
								"branchMarketCode.keyword": markets,
							},
						},
					},
				},
			},
		}
	} else {
		finalQuery = baseQuery
	}

	// Build aggregations
	aggs := map[string]types.Aggregations{
		"by_leadType": {
			Terms: &types.TermsAggregation{
				Field: ptr("leadType.keyword"),
				Size:  ptr(1000),
			},
			Aggregations: map[string]types.Aggregations{
				"by_mechanism": {
					Terms: &types.TermsAggregation{
						Field: ptr("leadSourceMechanism.keyword"),
						Size:  ptr(1000),
					},
					Aggregations: map[string]types.Aggregations{
						"periods": {
							Filters: &types.FiltersAggregation{
								Filters: map[string]types.Query{
									"last_24h": {
										Range: map[string]types.RangeQuery{
											"createdAt": types.DateRangeQuery{
												Gte:      ptr("now-24h"),
												Lt:       ptr("now"),
												TimeZone: ptr("Europe/Stockholm"),
											},
										},
									},
									"prev_24h": {
										Range: map[string]types.RangeQuery{
											"createdAt": types.DateRangeQuery{
												Gte:      ptr("now-48h"),
												Lt:       ptr("now-24h"),
												TimeZone: ptr("Europe/Stockholm"),
											},
										},
									},
									"last_7d": {
										Range: map[string]types.RangeQuery{
											"createdAt": types.DateRangeQuery{
												Gte:      ptr("now-7d"),
												Lt:       ptr("now"),
												TimeZone: ptr("Europe/Stockholm"),
											},
										},
									},
									"prev_7d": {
										Range: map[string]types.RangeQuery{
											"createdAt": types.DateRangeQuery{
												Gte:      ptr("now-14d"),
												Lt:       ptr("now-7d"),
												TimeZone: ptr("Europe/Stockholm"),
											},
										},
									},
									"last_30d": {
										Range: map[string]types.RangeQuery{
											"createdAt": types.DateRangeQuery{
												Gte:      ptr("now-30d"),
												Lt:       ptr("now"),
												TimeZone: ptr("Europe/Stockholm"),
											},
										},
									},
								},
								Keyed: ptr(true),
							},
						},
						"last_30d_daily": {
							Filter: &types.Query{
								Range: map[string]types.RangeQuery{
									"createdAt": types.DateRangeQuery{
										Gte:      ptr("now-30d"),
										Lt:       ptr("now"),
										TimeZone: ptr("Europe/Stockholm"),
									},
								},
							},
							Aggregations: map[string]types.Aggregations{
								"per_day": {
									DateHistogram: &types.DateHistogramAggregation{
										Field:            ptr("createdAt"),
										CalendarInterval: &calendarinterval.Day,
										TimeZone:         ptr("Europe/Stockholm"),
										MinDocCount:      ptr(0),
										Format:           ptr("yyyy-MM-dd"),
									},
								},
							},
						},
					},
				},
			},
		},
	}

	return &search.Request{
		Size:         ptr(0),
		Query:        finalQuery,
		Aggregations: aggs,
	}
}

// Helper function to create pointer to value
func ptr[T any](v T) *T {
	return &v
}
