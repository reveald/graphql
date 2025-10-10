package main

import (
	_ "embed"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	elasticsearch "github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/typedapi/core/search"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types/enums/calendarinterval"
	"github.com/graphql-go/graphql"
	revealdgraphql "github.com/reveald/graphql"
	"github.com/reveald/reveald"
)

// Embed the query JSON file
//go:embed queries/leads-overview.json
var leadsOverviewQueryJSON string

func main() {
	esURL := os.Getenv("elasticsearch_hosts")
	if esURL == "" {
		esURL = "http://localhost:9200"
	}

	leadsIndex := os.Getenv("index_name")
	if leadsIndex == "" {
		leadsIndex = "test-leads"
	}

	esUsername := os.Getenv("elasticsearch_username")
	esPassword := os.Getenv("elasticsearch_password")

	fmt.Printf("Using embedded query JSON for leads overview\n")
	fmt.Printf("Connecting to Elasticsearch at: %s\n", esURL)
	fmt.Printf("Index: %s\n", leadsIndex)

	backend, err := reveald.NewElasticBackend([]string{esURL})
	if err != nil {
		log.Fatalf("Failed to create backend: %v", err)
	}

	mappingData, err := os.ReadFile("leads-elastic-mapping.json")
	if err != nil {
		log.Fatalf("Failed to read mapping: %v", err)
	}

	mapping, err := revealdgraphql.ParseMapping(leadsIndex, mappingData)
	if err != nil {
		log.Fatalf("Failed to parse mapping: %v", err)
	}

	config := revealdgraphql.NewConfig()

	// PRECOMPILED QUERY using embedded JSON string
	config.AddPrecompiledQuery("leadsOverview", &revealdgraphql.PrecompiledQueryConfig{
		Index:       leadsIndex,
		Description: "Leads overview from embedded JSON",
		QueryJSON:   leadsOverviewQueryJSON, // ← Using embedded string!
	})

	// PRECOMPILED QUERY with QueryBuilder
	config.AddPrecompiledQuery("leadsOverviewByMarket", &revealdgraphql.PrecompiledQueryConfig{
		Index:       leadsIndex,
		Description: "Leads overview with market filtering",
		QueryBuilder: buildLeadsOverviewQuery,
		Parameters: graphql.FieldConfigArgument{
			"markets": &graphql.ArgumentConfig{
				Type:        graphql.NewList(graphql.String),
				Description: "Filter by market codes",
			},
		},
	})

	esConfig := elasticsearch.Config{
		Addresses: []string{esURL},
	}
	if esUsername != "" && esPassword != "" {
		esConfig.Username = esUsername
		esConfig.Password = esPassword
	}
	esClient, err := elasticsearch.NewTypedClient(esConfig)
	if err != nil {
		log.Fatalf("Failed to create ES client: %v", err)
	}

	api, err := revealdgraphql.New(backend, mapping, config, revealdgraphql.WithESClient(esClient))
	if err != nil {
		log.Fatalf("Failed to create GraphQL API: %v", err)
	}

	fmt.Println("GraphQL server starting on http://localhost:8080/graphql")
	if err := http.ListenAndServe(":8080", api); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

// buildLeadsOverviewQuery is the same as in main.go
func buildLeadsOverviewQuery(args map[string]any) *search.Request {
	markets := []string{}
	if marketsArg, ok := args["markets"].([]any); ok {
		for _, m := range marketsArg {
			if ms, ok := m.(string); ok {
				markets = append(markets, strings.ToUpper(ms))
			}
		}
	}

	baseQuery := &types.Query{
		Range: map[string]types.RangeQuery{
			"createdAt": types.DateRangeQuery{
				Gte:      ptr("now-2M"),
				Lt:       ptr("now"),
				TimeZone: ptr("Europe/Stockholm"),
			},
		},
	}

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
									"prev_30d": {
										Range: map[string]types.RangeQuery{
											"createdAt": types.DateRangeQuery{
												Gte:      ptr("now-60d"),
												Lt:       ptr("now-30d"),
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

func ptr[T any](v T) *T {
	return &v
}
