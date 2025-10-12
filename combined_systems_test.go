//go:build integration

package graphql

import (
	"context"
	"fmt"
	"testing"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/typedapi/core/search"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types"
)

func TestCombinedSystems(t *testing.T) {
	ctx := context.Background()

	// Start Elasticsearch container
	tc := StartElasticsearch(ctx, t)
	defer tc.Cleanup(ctx, t)

	indexName := "test-combined-systems"

	// Create index and load test data
	CreateTestIndex(ctx, t, tc.URI, indexName)
	testLeads := GenerateTestLeads()
	IndexTestData(ctx, t, tc.URI, indexName, testLeads)

	// Get ES mapping
	mappingJSON := []byte(fmt.Sprintf(`{
		"index": "%s",
		"properties": {
			"id": { "type": "keyword" },
			"leadType": { "type": "keyword" },
			"leadSourceMechanism": { "type": "keyword" },
			"branchMarketCode": { "type": "keyword" },
			"createdAt": { "type": "date" }
		}
	}`, indexName))

	mapping, err := ParseMapping(indexName, mappingJSON)
	if err != nil {
		t.Fatalf("Failed to parse mapping: %v", err)
	}

	// Create ES typed client
	esClient, err := elasticsearch.NewTypedClient(elasticsearch.Config{
		Addresses: []string{tc.URI},
	})
	if err != nil {
		t.Fatalf("Failed to create ES client: %v", err)
	}

	backend := CreateTestBackend(t, tc.URI)

	// Create config with BOTH reveald and precompiled queries
	config := NewConfig()

	// Add reveald feature-based query (simple, no specific features for now)
	config.AddQuery("revealdLeads", &QueryConfig{
		Indices:            []string{indexName},
		EnableAggregations: false, // Disable for simplicity
		EnablePagination:   true,
	})

	// Add precompiled query with typed aggregations
	config.AddPrecompiledQuery("precompiledLeads", &PrecompiledQueryConfig{
		Index:        indexName,
		Description:  "Precompiled leads query with typed aggregations",
		QueryBuilder: buildCombinedTestQuery,
	})

	// Create API with both query types
	api, err := New(backend, mapping, config, WithESClient(esClient))
	if err != nil {
		t.Fatalf("Failed to create API: %v", err)
	}

	schema := api.GetSchema()

	t.Run("Both query types exist in schema", func(t *testing.T) {
		queryType := IntrospectSchema(t, schema, "Query")

		fields := queryType["fields"].([]interface{})
		fieldNames := make(map[string]bool)
		for _, f := range fields {
			field := f.(map[string]interface{})
			fieldNames[field["name"].(string)] = true
		}

		if !fieldNames["revealdLeads"] {
			t.Error("Schema missing revealdLeads query (feature-based)")
		}
		if !fieldNames["precompiledLeads"] {
			t.Error("Schema missing precompiledLeads query (typed aggregations)")
		}
	})

	t.Run("Reveald query returns results", func(t *testing.T) {
		query := `{
			revealdLeads {
				totalCount
				hits {
					id
					leadType
				}
			}
		}`

		result := ExecuteGraphQLQuery(t, schema, query, nil)
		AssertNoErrors(t, result)

		data := result.Data.(map[string]interface{})
		revealdData := data["revealdLeads"].(map[string]interface{})

		totalCount := int(revealdData["totalCount"].(float64))
		if totalCount == 0 {
			t.Error("Expected reveald results")
		}

		hits := revealdData["hits"].([]interface{})
		if len(hits) == 0 {
			t.Error("Expected reveald hits")
		}
	})

	t.Run("Precompiled query returns typed aggregations", func(t *testing.T) {
		query := `{
			precompiledLeads {
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
								}
							}
						}
					}
				}
			}
		}`

		result := ExecuteGraphQLQuery(t, schema, query, nil)
		AssertNoErrors(t, result)

		data := result.Data.(map[string]interface{})
		precompiledData := data["precompiledLeads"].(map[string]interface{})

		// Precompiled uses typed aggregations with key/doc_count
		aggs := precompiledData["aggregations"].(map[string]interface{})
		byLeadType := aggs["by_leadType"].(map[string]interface{})
		buckets := byLeadType["buckets"].([]interface{})

		if len(buckets) == 0 {
			t.Error("Expected precompiled aggregation buckets")
		}

		bucket := buckets[0].(map[string]interface{})
		if bucket["key"] == nil {
			t.Error("Precompiled bucket missing key field")
		}
		if bucket["doc_count"] == nil {
			t.Error("Precompiled bucket missing doc_count field")
		}

		// Verify nested aggregation is object property (not array)
		byMechanism := bucket["by_mechanism"].(map[string]interface{})
		mechBuckets := byMechanism["buckets"].([]interface{})

		if len(mechBuckets) == 0 {
			t.Error("Expected nested aggregation buckets")
		}
	})

	t.Run("Execute both queries simultaneously", func(t *testing.T) {
		query := `{
			revealdLeads {
				totalCount
			}
			precompiledLeads {
				totalCount
			}
		}`

		result := ExecuteGraphQLQuery(t, schema, query, nil)
		AssertNoErrors(t, result)

		data := result.Data.(map[string]interface{})

		revealdData := data["revealdLeads"].(map[string]interface{})
		precompiledData := data["precompiledLeads"].(map[string]interface{})

		revealdCount := int(revealdData["totalCount"].(float64))
		precompiledCount := int(precompiledData["totalCount"].(float64))

		// Both query same index with different filters, but should both have results
		if revealdCount == 0 {
			t.Error("Expected reveald results")
		}
		if precompiledCount == 0 {
			t.Error("Expected precompiled results")
		}
	})

	t.Run("Type names are unique per query", func(t *testing.T) {
		revealdType := IntrospectSchema(t, schema, "RevealdLeadsResult")
		precompiledType := IntrospectSchema(t, schema, "PrecompiledLeadsResult")

		if revealdType["name"] == precompiledType["name"] {
			t.Error("Result type names should be unique")
		}

		// Verify aggregations types are different
		precompiledFields := precompiledType["fields"].([]interface{})

		var precompiledAggsType string
		for _, f := range precompiledFields {
			field := f.(map[string]interface{})
			if field["name"] == "aggregations" {
				fieldType := field["type"].(map[string]interface{})
				if fieldType["name"] != nil {
					precompiledAggsType = fieldType["name"].(string)
				}
			}
		}

		// Precompiled has typed aggregations object
		if precompiledAggsType != "PrecompiledLeadsAggregations" {
			t.Errorf("Expected PrecompiledLeadsAggregations, got %v", precompiledAggsType)
		}
	})
}

// buildCombinedTestQuery builds a query for combined systems testing
func buildCombinedTestQuery(args map[string]any) *search.Request {
	return &search.Request{
		Size: ptr(10),
		Query: &types.Query{
			Range: map[string]types.RangeQuery{
				"createdAt": types.DateRangeQuery{
					Gte:      ptr("now-60d"),
					Lt:       ptr("now"),
					TimeZone: ptr("Europe/Stockholm"),
				},
			},
		},
		Aggregations: map[string]types.Aggregations{
			"by_leadType": {
				Terms: &types.TermsAggregation{
					Field: ptr("leadType.keyword"),
					Size:  ptr(100),
				},
				Aggregations: map[string]types.Aggregations{
					"by_mechanism": {
						Terms: &types.TermsAggregation{
							Field: ptr("leadSourceMechanism.keyword"),
							Size:  ptr(100),
						},
					},
				},
			},
		},
	}
}
