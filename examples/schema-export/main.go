package main

import (
	"fmt"
	"log"
	"os"

	"github.com/elastic/go-elasticsearch/v8/typedapi/core/search"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types/enums/calendarinterval"
	"github.com/graphql-go/graphql"
	revealdgraphql "github.com/reveald/graphql"
)

// Schema Export Tool
//
// This tool generates the GraphQL schema SDL without requiring Elasticsearch connection.
// Useful for:
// - Apollo Federation schema composition
// - CI/CD pipelines
// - Schema validation
// - Documentation generation
//
// Usage:
//   go run main.go > schema.graphql

func main() {
	// Configuration
	leadsIndex := os.Getenv("INDEX_NAME")
	if leadsIndex == "" {
		leadsIndex = "test-leads"
	}

	// Load mapping (this is the only file you need)
	mappingData, err := os.ReadFile("../leads/leads-elastic-mapping.json")
	if err != nil {
		log.Fatalf("Failed to read mapping: %v", err)
	}

	mapping, err := revealdgraphql.ParseMapping(leadsIndex, mappingData)
	if err != nil {
		log.Fatalf("Failed to parse mapping: %v", err)
	}

	// Create config with functional options (all configuration in one place!)
	extendType := os.Getenv("EXTEND_TYPE") == "true"

	config := revealdgraphql.NewConfig(
		revealdgraphql.WithEnableFederation(),
		revealdgraphql.WithQueryNamespace("Leads", extendType),
		revealdgraphql.WithPrecompiledQuery("leadsOverview", &revealdgraphql.PrecompiledQueryConfig{
			Mapping:         mapping,
			Index:           leadsIndex,
			Description:     "Leads overview with statistics",
			QueryBuilder:    buildLeadsOverviewQuery,
			EntityKeyFields: []string{"id", "conversationId"}, // Multiple @key directives for entity resolution
		}),
		revealdgraphql.WithPrecompiledQuery("leadsOverviewByMarket", &revealdgraphql.PrecompiledQueryConfig{
			Mapping:      mapping,
			Index:        leadsIndex,
			Description:  "Leads overview with market filtering",
			QueryBuilder: buildLeadsOverviewQuery,
			Parameters: graphql.FieldConfigArgument{
				"markets": &graphql.ArgumentConfig{
					Type:        graphql.NewList(graphql.String),
					Description: "Filter by market codes",
				},
			},
		}),
	)

	// Generate schema SDL without ES connection!
	sdl, err := revealdgraphql.GenerateSchemaSDL(config)
	if err != nil {
		log.Fatalf("Failed to generate schema: %v", err)
	}

	// Print to stdout (can be redirected to file)
	fmt.Println(sdl)
}

// buildLeadsOverviewQuery builds the leads overview query
func buildLeadsOverviewQuery(args map[string]any) *search.Request {
	return &search.Request{
		Size: ptr(0),
		Query: &types.Query{
			Range: map[string]types.RangeQuery{
				"createdAt": types.DateRangeQuery{
					Gte:      ptr("now-2M"),
					Lt:       ptr("now"),
					TimeZone: ptr("Europe/Stockholm"),
				},
			},
		},
		Aggregations: map[string]types.Aggregations{
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
										"last_7d": {
											Range: map[string]types.RangeQuery{
												"createdAt": types.DateRangeQuery{
													Gte:      ptr("now-7d"),
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
		},
	}
}

func ptr[T any](v T) *T {
	return &v
}
