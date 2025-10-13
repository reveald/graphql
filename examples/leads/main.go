package main

import (
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

func main() {
	// Get ES configuration from environment
	esURL := os.Getenv("ELASTICSEARCH_URL")
	if esURL == "" {
		esURL = os.Getenv("elasticsearch_hosts")
	}
	if esURL == "" {
		esURL = "http://localhost:9200"
	}

	leadsIndex := os.Getenv("INDEX_NAME")
	if leadsIndex == "" {
		leadsIndex = os.Getenv("index_name")
	}
	if leadsIndex == "" {
		leadsIndex = "test-leads"
	}

	esUsername := os.Getenv("elasticsearch_username")
	esPassword := os.Getenv("elasticsearch_password")

	fmt.Printf("Connecting to Elasticsearch at: %s\n", esURL)
	fmt.Printf("Index: %s\n", leadsIndex)

	// Create Elasticsearch backend
	backend, err := reveald.NewElasticBackend([]string{esURL})
	if err != nil {
		log.Fatalf("Failed to create backend: %v", err)
	}

	// Load the Elasticsearch mapping
	mappingData, err := os.ReadFile("leads-elastic-mapping.json")
	if err != nil {
		log.Fatalf("Failed to read mapping: %v", err)
	}

	mapping, err := revealdgraphql.ParseMapping(leadsIndex, mappingData)
	if err != nil {
		log.Fatalf("Failed to parse mapping: %v", err)
	}

	// Configure the GraphQL API
	config := revealdgraphql.NewConfig(
		mapping,
		revealdgraphql.WithEnableFederation(),
		revealdgraphql.WithQueryNamespace("Leads", false), // false = define type, true = extend type
	)

	// Add PRECOMPILED QUERY with simple QueryBuilder (no parameters)
	config.AddPrecompiledQuery("leadsOverview", &revealdgraphql.PrecompiledQueryConfig{
		Index:        leadsIndex,
		Description:  "Leads overview with statistics by type and mechanism",
		QueryBuilder: func(args map[string]any) *search.Request { return buildLeadsOverviewQuery(args) },
	})

	// Add PRECOMPILED QUERY with QueryBuilder (supports market filtering)
	config.AddPrecompiledQuery("leadsOverviewByMarket", &revealdgraphql.PrecompiledQueryConfig{
		Index:        leadsIndex,
		Description:  "Leads overview with market filtering",
		QueryBuilder: buildLeadsOverviewQuery,
		Parameters: graphql.FieldConfigArgument{
			"markets": &graphql.ArgumentConfig{
				Type:        graphql.NewList(graphql.String),
				Description: "Filter by market codes (e.g., ['SE', 'NO', 'DK'])",
			},
		},
	})

	// Add PRECOMPILED QUERY with RootQueryBuilder (demonstrates tenant/permission filtering)
	config.AddPrecompiledQuery("leadsOverviewWithTenant", &revealdgraphql.PrecompiledQueryConfig{
		Index:        leadsIndex,
		Description:  "Leads overview with dynamic tenant filtering from HTTP headers",
		QueryBuilder: func(args map[string]any) *search.Request { return buildLeadsOverviewQuery(args) },
		RootQueryBuilder: func(r *http.Request) (*types.Query, error) {
			// Example: Filter by tenant ID from header
			tenantID := r.Header.Get("X-Tenant-ID")
			if tenantID != "" {
				return &types.Query{
					Term: map[string]types.TermQuery{
						"tenantId.keyword": {Value: tenantID},
					},
				}, nil
			}
			return nil, nil
		},
	})

	// Create Elasticsearch typed client
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

	// Create the GraphQL API
	api, err := revealdgraphql.New(backend, config, revealdgraphql.WithESClient(esClient))
	if err != nil {
		log.Fatalf("Failed to create GraphQL API: %v", err)
	}

	// Start the HTTP server
	fmt.Println("GraphQL server starting on http://localhost:8080/graphql")
	fmt.Println("Open http://localhost:8080/graphql in your browser to use GraphiQL")
	fmt.Println()
	fmt.Println("Example queries (namespaced under 'leads' with strongly-typed aggregations):")
	fmt.Println(`# 1. Leads overview (no filters):
			query {
			leads {
				leadsOverview {
				totalCount
				aggregations {
					by_leadType {
					buckets {
						key
						doc_count
						by_mechanism {
						buckets {
							key
							doc_count
							periods {
							last_24h { doc_count }
							prev_24h { doc_count }
							}
						}
						}
					}
					}
				}
				}
			}
			}

			# 2. Leads overview with market filter:
			query {
			leads {
				leadsOverviewByMarket(markets: ["SE", "NO"]) {
				totalCount
				aggregations {
					by_leadType {
					buckets {
						key
						doc_count
						by_mechanism {
						buckets {
							key
							doc_count
							periods {
							last_24h { doc_count }
							prev_24h { doc_count }
							prev_24h_7d_ago { doc_count }
							last_7d { doc_count }
							prev_7d { doc_count }
							last_30d { doc_count }
							prev_30d { doc_count }
							}
							last_30d_daily {
							doc_count
							per_day {
								buckets {
								key
								doc_count
								}
							}
							}
						}
						}
						last_30d_daily_all_mechanisms {
						doc_count
						per_day {
							buckets {
							key
							doc_count
							}
						}
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

// buildLeadsOverviewQuery builds the leads overview query with market filtering support
func buildLeadsOverviewQuery(args map[string]any) *search.Request {
	// Extract markets parameter
	markets := []string{}
	if marketsArg, ok := args["markets"].([]any); ok {
		for _, m := range marketsArg {
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

	// Build the exact same aggregations as the TypeScript version
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
									"prev_24h_7d_ago": {
										Range: map[string]types.RangeQuery{
											"createdAt": types.DateRangeQuery{
												Gte:      ptr("now-8d"),
												Lt:       ptr("now-7d"),
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
				"last_30d_daily_all_mechanisms": {
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
