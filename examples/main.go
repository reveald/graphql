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
	"github.com/graphql-go/graphql"
	"github.com/reveald/reveald/v2"
	"github.com/reveald/reveald/v2/featureset"
	revealdgraphql "github.com/wayke-se/reveald-graphql"
)

func main() {
	// Create Elasticsearch backend
	backend, err := reveald.NewElasticBackend([]string{"http://localhost:9200"})
	if err != nil {
		log.Fatalf("Failed to create backend: %v", err)
	}

	// Load the Elasticsearch mapping
	mappingData, err := os.ReadFile("products-mapping.json")
	if err != nil {
		log.Fatalf("Failed to read mapping: %v", err)
	}

	mapping, err := revealdgraphql.ParseMapping("products", mappingData)
	if err != nil {
		log.Fatalf("Failed to parse mapping: %v", err)
	}

	// Configure the GraphQL API with functional options
	config := revealdgraphql.NewConfig(
		mapping,
		revealdgraphql.WithEnableFederation(),
		// Optional: revealdgraphql.WithQueryNamespace("Products", false),
	)

	// Add a search query with features
	config.AddQuery("searchProducts", &revealdgraphql.QueryConfig{
		Description: "Search for products with filtering, pagination, and aggregations",
		Features: []reveald.Feature{
			featureset.NewPaginationFeature(
				featureset.WithPageSize(20),
				featureset.WithMaxPageSize(100),
			),
			// Use DynamicFilterFeature for text fields with .keyword multi-fields
			featureset.NewDynamicFilterFeature("category"),
			featureset.NewDynamicFilterFeature("brand"),
			featureset.NewDynamicFilterFeature("tags"),
			featureset.NewStaticFilterFeature(
				featureset.WithRequiredValue("active", true),
			),
		},
		EnableAggregations: true,
		EnablePagination:   true,
		EnableSorting:      true,
		// Aggregation fields are auto-detected from Features!
	})

	// Add another query for all products (no active filter)
	config.AddQuery("allProducts", &revealdgraphql.QueryConfig{
		Description: "Get all products without filters",
		Features: []reveald.Feature{
			featureset.NewPaginationFeature(
				featureset.WithPageSize(10),
			),
		},
		EnablePagination: true,
	})

	// Add a flexible query with typed Elasticsearch querying
	config.AddQuery("flexibleSearch", &revealdgraphql.QueryConfig{
		Description:           "Flexible search with full ES query/aggregation support",
		EnableElasticQuerying: true,
		EnableAggregations:    true,
		EnablePagination:      true,
		// RootQuery: Always filter for active products
		// RootQuery: &types.Query{
		// 	Term: map[string]types.TermQuery{
		// 		"active": {Value: true},
		// 	},
		// },
	})

	// Add a PRECOMPILED QUERY with complex nested aggregations (using QueryBuilder)
	config.AddPrecompiledQuery("productAnalytics", &revealdgraphql.PrecompiledQueryConfig{
		Description:  "Complex product analytics with nested aggregations and filters",
		QueryBuilder: buildProductAnalyticsQuery,
		Parameters: graphql.FieldConfigArgument{
			"minPrice": &graphql.ArgumentConfig{
				Type:        graphql.Float,
				Description: "Minimum price filter",
			},
			"maxPrice": &graphql.ArgumentConfig{
				Type:        graphql.Float,
				Description: "Maximum price filter",
			},
			"categories": &graphql.ArgumentConfig{
				Type:        graphql.NewList(graphql.String),
				Description: "Filter by categories",
			},
		},
		SampleParameters: map[string]any{
			"minPrice": 0.0,
			"maxPrice": 1000.0,
		},
	})

	// Add a PRECOMPILED QUERY using QueryJSON (embedded JSON)
	config.AddPrecompiledQuery("productTrends", &revealdgraphql.PrecompiledQueryConfig{
		Description: "Product trends using QueryJSON",
		QueryJSON: `{
			"size": 0,
			"query": {
				"bool": {
					"must": [
						{"range": {"price": {"gte": 0, "lte": 1000}}}
					]
				}
			},
			"aggs": {
				"by_category": {
					"terms": {"field": "category.keyword", "size": 10},
					"aggs": {
						"price_ranges": {
							"filters": {
								"filters": {
									"low": {"range": {"price": {"lt": 100}}},
									"medium": {"range": {"price": {"gte": 100, "lt": 300}}},
									"high": {"range": {"price": {"gte": 300}}}
								}
							}
						},
						"avg_price": {
							"avg": {"field": "price"}
						}
					}
				}
			}
		}`,
	})

	// Create Elasticsearch typed client for flexible querying
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
	fmt.Println("Example queries:")
	fmt.Println(`
# Feature-based query:
query {
  searchProducts(category: ["electronics"], limit: 10) {
    hits {
      id
      name
      price
      category
      brand
    }
    totalCount
    aggregations {
      category { value count }
      brand { value count }
      tags { value count }
    }
  }
}

# Flexible ES query:
query {
  flexibleSearch(
    query: {
      bool: {
        must: [
          { range: { field: "price", gte: 100, lte: 1000 } }
          { terms: { field: "category", values: ["electronics"] } }
        ]
      }
    }
    aggs: [
      { name: "brands", terms: { field: "brand", size: 10 } }
      { name: "price_stats", stats: { field: "price" } }
    ]
  ) {
    hits {
      id
      name
      price
      category
    }
    totalCount
    aggregations {
      brands { value count }
    }
  }
}

# PRECOMPILED QUERY with QueryBuilder (strongly-typed aggregations):
query {
  productAnalytics(minPrice: 50, maxPrice: 500, categories: ["electronics"]) {
    totalCount
    aggregations {
      by_category {
        buckets {
          key
          doc_count
          by_brand {
            buckets {
              key
              doc_count
              price_ranges {
                budget { doc_count }
                mid_range { doc_count }
                premium { doc_count }
              }
              price_stats {
                min
                max
                avg
                sum
                count
              }
            }
          }
          avg_price_in_category
        }
      }
      price_distribution {
        buckets {
          key
          doc_count
        }
      }
    }
  }
}

# PRECOMPILED QUERY with QueryJSON (strongly-typed aggregations):
query {
  productTrends {
    totalCount
    aggregations {
      by_category {
        buckets {
          key
          doc_count
          price_ranges {
            low { doc_count }
            medium { doc_count }
            high { doc_count }
          }
          avg_price
        }
      }
    }
  }
}`)

	if err := http.ListenAndServe(":8080", api); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

// buildProductAnalyticsQuery builds a complex analytics query with nested aggregations
func buildProductAnalyticsQuery(args map[string]any) *search.Request {
	// Build query based on parameters
	var queries []types.Query

	// Add price range filter if specified
	if minPrice, ok := args["minPrice"].(float64); ok {
		if maxPrice, ok := args["maxPrice"].(float64); ok {
			queries = append(queries, types.Query{
				Range: map[string]types.RangeQuery{
					"price": types.NumberRangeQuery{
						Gte: ptr(types.Float64(minPrice)),
						Lte: ptr(types.Float64(maxPrice)),
					},
				},
			})
		}
	}

	// Add category filter if specified
	if categoriesArg, ok := args["categories"].([]any); ok && len(categoriesArg) > 0 {
		var categories []string
		for _, cat := range categoriesArg {
			if catStr, ok := cat.(string); ok {
				categories = append(categories, strings.ToLower(catStr))
			}
		}
		if len(categories) > 0 {
			queries = append(queries, types.Query{
				Terms: &types.TermsQuery{
					TermsQuery: map[string]types.TermsQueryField{
						"category.keyword": categories,
					},
				},
			})
		}
	}

	// Build final query
	var finalQuery *types.Query
	if len(queries) > 0 {
		finalQuery = &types.Query{
			Bool: &types.BoolQuery{
				Must: queries,
			},
		}
	}

	// Build complex nested aggregations
	aggs := map[string]types.Aggregations{
		// Category breakdown with nested brand analysis
		"by_category": {
			Terms: &types.TermsAggregation{
				Field: ptr("category.keyword"),
				Size:  ptr(10),
			},
			Aggregations: map[string]types.Aggregations{
				// Brand breakdown within each category
				"by_brand": {
					Terms: &types.TermsAggregation{
						Field: ptr("brand.keyword"),
						Size:  ptr(20),
					},
					Aggregations: map[string]types.Aggregations{
						// Price range buckets (filters aggregation)
						"price_ranges": {
							Filters: &types.FiltersAggregation{
								Filters: map[string]types.Query{
									"budget": {
										Range: map[string]types.RangeQuery{
											"price": types.NumberRangeQuery{
												Lt: ptr(types.Float64(100)),
											},
										},
									},
									"mid_range": {
										Range: map[string]types.RangeQuery{
											"price": types.NumberRangeQuery{
												Gte: ptr(types.Float64(100)),
												Lt:  ptr(types.Float64(500)),
											},
										},
									},
									"premium": {
										Range: map[string]types.RangeQuery{
											"price": types.NumberRangeQuery{
												Gte: ptr(types.Float64(500)),
											},
										},
									},
								},
								Keyed: ptr(true),
							},
						},
						// Price statistics per brand
						"price_stats": {
							Stats: &types.StatsAggregation{
								Field: ptr("price"),
							},
						},
					},
				},
				// Average price per category
				"avg_price_in_category": {
					Avg: &types.AverageAggregation{
						Field: ptr("price"),
					},
				},
			},
		},
		// Overall price distribution histogram
		"price_distribution": {
			Histogram: &types.HistogramAggregation{
				Field:    ptr("price"),
				Interval: ptr(types.Float64(100)),
			},
		},
	}

	return &search.Request{
		Size:         ptr(0), // We only want aggregations, not documents
		Query:        finalQuery,
		Aggregations: aggs,
	}
}

// Helper function to create pointer to value
func ptr[T any](v T) *T {
	return &v
}
