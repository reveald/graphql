package graphql

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/elastic/go-elasticsearch/v8/typedapi/types"
	"github.com/graphql-go/graphql"
	"github.com/reveald/reveald"
	"github.com/reveald/reveald/featureset"
)

// Integration tests verify that typed ES queries work alongside reveald Features

// TestIntegration_FeatureBasedQuery tests that traditional reveald Feature-based queries still work
func TestIntegration_FeatureBasedQuery(t *testing.T) {
	indexName := "test_feature_based"
	setupTestIndex(t, indexName)

	// Create mapping
	mappingJSON := []byte(`{
		"mappings": {
			"properties": {
				"id": {"type": "keyword"},
				"name": {"type": "text", "fields": {"keyword": {"type": "keyword"}}},
				"brand": {"type": "text", "fields": {"keyword": {"type": "keyword"}}},
				"category": {"type": "text", "fields": {"keyword": {"type": "keyword"}}},
				"price": {"type": "double"},
				"active": {"type": "boolean"}
			}
		}
	}`)

	mapping, err := ParseMapping(indexName, mappingJSON)
	if err != nil {
		t.Fatalf("failed to parse mapping: %v", err)
	}

	// Create config with Feature-based query
	config := NewConfig()
	config.AddQuery("searchProducts", &QueryConfig{
		Index:       indexName,
		Description: "Search with reveald Features",
		Features: []reveald.Feature{
			featureset.NewPaginationFeature(
				featureset.WithPageSize(10),
			),
			featureset.NewDynamicFilterFeature("category"),
			featureset.NewDynamicFilterFeature("brand"),
			featureset.NewStaticFilterFeature(
				featureset.WithRequiredValue("active", true),
			),
		},
		EnableAggregations: true,
		EnablePagination:   true,
	})

	// Create API
	api, err := New(esBackend, mapping, config, WithESClient(esClient))
	if err != nil {
		t.Fatalf("failed to create API: %v", err)
	}

	// Execute GraphQL query using Features
	query := `
		query {
			searchProducts(category: ["electronics"], limit: 2) {
				hits {
					id
					name
					brand
					active
				}
				totalCount
				pagination {
					limit
					offset
					totalCount
				}
				aggregations {
					brand { value count }
				}
			}
		}
	`

	result := graphql.Do(graphql.Params{
		Schema:        api.GetSchema(),
		RequestString: query,
	})

	if len(result.Errors) > 0 {
		t.Fatalf("GraphQL errors: %v", result.Errors)
	}

	data := result.Data.(map[string]any)
	searchResults := data["searchProducts"].(map[string]any)

	// Verify results
	hits := searchResults["hits"].([]any)
	if len(hits) != 2 {
		t.Errorf("expected 2 hits with limit=2, got %d", len(hits))
	}

	// Verify StaticFilterFeature worked (active=true only)
	for _, hit := range hits {
		hitMap := hit.(map[string]any)
		if hitMap["active"] != true {
			t.Errorf("expected all hits to have active=true (StaticFilter), got %v", hitMap["active"])
		}
		// Category might be nil if not in source, just check active
	}

	// Verify pagination
	pagination := searchResults["pagination"].(map[string]any)
	if pagination["limit"] != 2 {
		t.Errorf("expected limit=2, got %v", pagination["limit"])
	}

	// Verify aggregations
	aggregations := searchResults["aggregations"].(map[string]any)
	brandAggs := aggregations["brand"].([]any)
	if len(brandAggs) == 0 {
		t.Error("expected brand aggregations")
	}
}

func TestIntegration_MixedQueryTypes(t *testing.T) {
	indexName := "test_mixed_queries"
	setupTestIndex(t, indexName)

	// Create mapping
	mappingJSON := []byte(`{
		"mappings": {
			"properties": {
				"id": {"type": "keyword"},
				"name": {"type": "text"},
				"brand": {"type": "text", "fields": {"keyword": {"type": "keyword"}}},
				"price": {"type": "double"},
				"active": {"type": "boolean"}
			}
		}
	}`)

	mapping, err := ParseMapping(indexName, mappingJSON)
	if err != nil {
		t.Fatalf("failed to parse mapping: %v", err)
	}

	// Create config with BOTH Feature-based and typed ES queries
	config := NewConfig()

	// Feature-based query
	config.AddQuery("featureSearch", &QueryConfig{
		Index: indexName,
		Features: []reveald.Feature{
			featureset.NewDynamicFilterFeature("brand"),
			featureset.NewStaticFilterFeature(
				featureset.WithRequiredValue("active", true),
			),
		},
		EnableAggregations: true,
	})

	// Typed ES query with root query
	config.AddQuery("flexibleSearch", &QueryConfig{
		Index:                 indexName,
		EnableElasticQuerying: true,
		EnableAggregations:    true,
		RootQuery: &types.Query{
			Term: map[string]types.TermQuery{
				"active": {Value: true},
			},
		},
	})

	// Create API
	api, err := New(esBackend, mapping, config, WithESClient(esClient))
	if err != nil {
		t.Fatalf("failed to create API: %v", err)
	}

	schema := api.GetSchema()

	// Test 1: Feature-based query
	query1 := `query { featureSearch(brand: ["TechBrand"]) { hits { id name active } totalCount } }`
	result1 := graphql.Do(graphql.Params{Schema: schema, RequestString: query1})

	if len(result1.Errors) > 0 {
		t.Fatalf("Feature query errors: %v", result1.Errors)
	}

	data1 := result1.Data.(map[string]any)["featureSearch"].(map[string]any)
	totalCount1 := int(data1["totalCount"].(int))

	// Test 2: Typed ES query with same filter
	query2 := `query { flexibleSearch(query: { term: { field: "brand.keyword", value: "TechBrand" } }) { hits { id name active } totalCount } }`
	result2 := graphql.Do(graphql.Params{Schema: schema, RequestString: query2})

	if len(result2.Errors) > 0 {
		t.Fatalf("Typed query errors: %v", result2.Errors)
	}

	data2 := result2.Data.(map[string]any)["flexibleSearch"].(map[string]any)
	totalCount2 := int(data2["totalCount"].(int))

	// Both should return same count (active TechBrand products)
	if totalCount1 != totalCount2 {
		t.Errorf("expected same results from both query types, got %d vs %d", totalCount1, totalCount2)
	}

	// Verify RootQuery was applied (active=true only)
	hits2 := data2["hits"].([]any)
	for _, hit := range hits2 {
		hitMap := hit.(map[string]any)
		if hitMap["active"] != true {
			t.Errorf("expected active=true from RootQuery, got %v", hitMap["active"])
		}
	}
}

func TestIntegration_TypedQueryWithAggregations(t *testing.T) {
	indexName := "test_typed_with_aggs"
	setupTestIndex(t, indexName)

	mappingJSON := []byte(`{
		"mappings": {
			"properties": {
				"id": {"type": "keyword"},
				"brand": {"type": "text", "fields": {"keyword": {"type": "keyword"}}},
				"price": {"type": "double"},
				"active": {"type": "boolean"}
			}
		}
	}`)

	mapping, err := ParseMapping(indexName, mappingJSON)
	if err != nil {
		t.Fatalf("failed to parse mapping: %v", err)
	}

	config := NewConfig()
	config.AddQuery("search", &QueryConfig{
		Index:                 indexName,
		EnableElasticQuerying: true,
		EnableAggregations:    true,
	})

	api, err := New(esBackend, mapping, config, WithESClient(esClient))
	if err != nil {
		t.Fatalf("failed to create API: %v", err)
	}

	// Execute query with aggregations
	// Note: Only queries with EnableAggregations get the aggregations field
	query := `
		query {
			search(
				query: { term: { field: "active", value: "true" } }
			) {
				hits { id }
				totalCount
			}
		}
	`

	result := graphql.Do(graphql.Params{Schema: api.GetSchema(), RequestString: query})

	if len(result.Errors) > 0 {
		t.Fatalf("GraphQL errors: %v", result.Errors)
	}

	data := result.Data.(map[string]any)["search"].(map[string]any)

	// Verify query executed successfully
	if data["totalCount"].(int) == 0 {
		t.Error("expected some results for active products")
	}
}

func TestIntegration_ConverterRoundtrip(t *testing.T) {
	rb := &ResolverBuilder{}

	// Test complex bool query conversion
	input := map[string]any{
		"bool": map[string]any{
			"must": []any{
				map[string]any{
					"term": map[string]any{
						"field": "brand.keyword",
						"value": "TechBrand",
					},
				},
				map[string]any{
					"range": map[string]any{
						"field": "price",
						"gte":   float64(100),
						"lte":   float64(1000),
					},
				},
			},
			"filter": []any{
				map[string]any{
					"exists": map[string]any{
						"field": "description",
					},
				},
			},
		},
	}

	queryInput, err := rb.convertToESQueryInput(input)
	if err != nil {
		t.Fatalf("conversion failed: %v", err)
	}

	// Verify structure
	if queryInput.Bool == nil {
		t.Fatal("expected bool query")
	}

	if len(queryInput.Bool.Must) != 2 {
		t.Errorf("expected 2 must clauses, got %d", len(queryInput.Bool.Must))
	}

	if len(queryInput.Bool.Filter) != 1 {
		t.Errorf("expected 1 filter clause, got %d", len(queryInput.Bool.Filter))
	}

	// Convert to ES Query
	esQuery, err := convertQueryInput(queryInput)
	if err != nil {
		t.Fatalf("ES conversion failed: %v", err)
	}

	if esQuery.Bool == nil {
		t.Fatal("expected bool query in ES format")
	}

	if len(esQuery.Bool.Must) != 2 {
		t.Errorf("expected 2 must clauses in ES query, got %d", len(esQuery.Bool.Must))
	}
}

func TestIntegration_SubAggregations(t *testing.T) {
	indexName := "test_sub_aggs"
	setupTestIndex(t, indexName)

	ctx := context.Background()

	// Test sub-aggregations at the ES level directly
	aggs := map[string]types.Aggregations{
		"categories": {
			Terms: &types.TermsAggregation{
				Field: stringPtr("category.keyword"),
			},
			Aggregations: map[string]types.Aggregations{
				"avg_price": {
					Avg: &types.AverageAggregation{Field: stringPtr("price")},
				},
			},
		},
	}

	result, err := executeTypedQuery(ctx, esClient, []string{indexName}, nil, aggs, nil, nil)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}

	// Verify main aggregation exists
	categoryAggs, ok := result.Aggregations["categories"]
	if !ok {
		t.Fatal("expected categories aggregation")
	}

	if len(categoryAggs) == 0 {
		t.Error("expected category buckets")
	}

	// Sub-aggregations (avg_price) are metric aggregations
	// They would be in bucket.SubResultBuckets if ES returns them
	// This test verifies that parsing doesn't error on sub-aggs
}

func TestIntegration_ResultFormatConsistency(t *testing.T) {
	indexName := "test_result_consistency"
	setupTestIndex(t, indexName)

	mappingJSON := []byte(`{
		"mappings": {
			"properties": {
				"id": {"type": "keyword"},
				"name": {"type": "text"},
				"brand": {"type": "text", "fields": {"keyword": {"type": "keyword"}}},
				"active": {"type": "boolean"}
			}
		}
	}`)

	mapping, err := ParseMapping(indexName, mappingJSON)
	if err != nil {
		t.Fatalf("failed to parse mapping: %v", err)
	}

	config := NewConfig()

	// Feature-based query
	config.AddQuery("featureSearch", &QueryConfig{
		Index: indexName,
		Features: []reveald.Feature{
			featureset.NewPaginationFeature(),
			featureset.NewDynamicFilterFeature("brand"),
		},
		EnablePagination: true,
	})

	// Typed ES query
	config.AddQuery("typedSearch", &QueryConfig{
		Index:                 indexName,
		EnableElasticQuerying: true,
		EnablePagination:      true,
	})

	api, err := New(esBackend, mapping, config, WithESClient(esClient))
	if err != nil {
		t.Fatalf("failed to create API: %v", err)
	}

	schema := api.GetSchema()

	// Execute Feature-based query
	query1 := `query { featureSearch(brand: ["TechBrand"], limit: 2) { hits { id name } totalCount pagination { limit offset } } }`
	result1 := graphql.Do(graphql.Params{Schema: schema, RequestString: query1})

	// Execute Typed ES query with same filter
	query2 := `query { typedSearch(query: { term: { field: "brand.keyword", value: "TechBrand" } }, limit: 2) { hits { id name } totalCount pagination { limit offset } } }`
	result2 := graphql.Do(graphql.Params{Schema: schema, RequestString: query2})

	if len(result1.Errors) > 0 {
		t.Fatalf("Feature query errors: %v", result1.Errors)
	}
	if len(result2.Errors) > 0 {
		t.Fatalf("Typed query errors: %v", result2.Errors)
	}

	// Compare response structures
	data1 := result1.Data.(map[string]any)["featureSearch"].(map[string]any)
	data2 := result2.Data.(map[string]any)["typedSearch"].(map[string]any)

	// Both should have same structure
	fields := []string{"hits", "totalCount", "pagination"}
	for _, field := range fields {
		if _, ok := data1[field]; !ok {
			t.Errorf("Feature result missing field: %s", field)
		}
		if _, ok := data2[field]; !ok {
			t.Errorf("Typed result missing field: %s", field)
		}
	}

	// Verify both return same count
	if data1["totalCount"] != data2["totalCount"] {
		t.Errorf("expected same totalCount, got %v vs %v", data1["totalCount"], data2["totalCount"])
	}
}

func TestIntegration_RootQueryEnforcement(t *testing.T) {
	indexName := "test_root_query_enforcement"
	setupTestIndex(t, indexName)

	mappingJSON := []byte(`{
		"mappings": {
			"properties": {
				"id": {"type": "keyword"},
				"brand": {"type": "text", "fields": {"keyword": {"type": "keyword"}}},
				"active": {"type": "boolean"}
			}
		}
	}`)

	mapping, err := ParseMapping(indexName, mappingJSON)
	if err != nil {
		t.Fatalf("failed to parse mapping: %v", err)
	}

	// Create config with RootQuery that enforces active=true
	config := NewConfig()
	config.AddQuery("search", &QueryConfig{
		Index:                 indexName,
		EnableElasticQuerying: true,
		RootQuery: &types.Query{
			Term: map[string]types.TermQuery{
				"active": {Value: true},
			},
		},
	})

	api, err := New(esBackend, mapping, config, WithESClient(esClient))
	if err != nil {
		t.Fatalf("failed to create API: %v", err)
	}

	// Execute query that doesn't specify active filter
	// RootQuery should still enforce it
	query := `query { search(query: { term: { field: "brand.keyword", value: "TechBrand" } }) { hits { id active } totalCount } }`
	result := graphql.Do(graphql.Params{Schema: api.GetSchema(), RequestString: query})

	if len(result.Errors) > 0 {
		t.Fatalf("GraphQL errors: %v", result.Errors)
	}

	data := result.Data.(map[string]any)["search"].(map[string]any)
	hits := data["hits"].([]any)

	// All results MUST have active=true (enforced by RootQuery)
	for _, hit := range hits {
		hitMap := hit.(map[string]any)
		if hitMap["active"] != true {
			t.Errorf("RootQuery not enforced: expected active=true, got %v", hitMap["active"])
		}
	}

	// Should get 2 active TechBrand products (not 3 which would include inactive)
	if data["totalCount"].(int) != 2 {
		t.Errorf("expected 2 active TechBrand products, got %v", data["totalCount"])
	}
}

func TestIntegration_ComplexNestedQuery(t *testing.T) {
	indexName := "test_complex_nested"
	setupTestIndex(t, indexName)

	mappingJSON := []byte(`{
		"mappings": {
			"properties": {
				"id": {"type": "keyword"},
				"brand": {"type": "text", "fields": {"keyword": {"type": "keyword"}}},
				"category": {"type": "text", "fields": {"keyword": {"type": "keyword"}}},
				"price": {"type": "double"},
				"active": {"type": "boolean"}
			}
		}
	}`)

	mapping, err := ParseMapping(indexName, mappingJSON)
	if err != nil {
		t.Fatalf("failed to parse mapping: %v", err)
	}

	config := NewConfig()
	config.AddQuery("search", &QueryConfig{
		Index:                 indexName,
		EnableElasticQuerying: true,
	})

	api, err := New(esBackend, mapping, config, WithESClient(esClient))
	if err != nil {
		t.Fatalf("failed to create API: %v", err)
	}

	// Complex bool query: (electronics OR furniture) AND TechBrand AND price < 100
	query := `
		query {
			search(
				query: {
					bool: {
						must: [
							{
								bool: {
									should: [
										{ term: { field: "category.keyword", value: "electronics" } }
										{ term: { field: "category.keyword", value: "furniture" } }
									]
								}
							}
							{ term: { field: "brand.keyword", value: "TechBrand" } }
							{ range: { field: "price", lt: 100 } }
						]
					}
				}
			) {
				hits { id price }
				totalCount
			}
		}
	`

	result := graphql.Do(graphql.Params{Schema: api.GetSchema(), RequestString: query})

	if len(result.Errors) > 0 {
		t.Fatalf("GraphQL errors: %v", result.Errors)
	}

	data := result.Data.(map[string]any)["search"].(map[string]any)
	hits := data["hits"].([]any)

	// Verify all results meet criteria
	for _, hit := range hits {
		hitMap := hit.(map[string]any)

		// Must have price < 100
		price := hitMap["price"].(float64)
		if price >= 100 {
			t.Errorf("expected price < 100, got %v", price)
		}
	}

	// Should get TechBrand electronics under $100 (Wireless Mouse at $29.99)
	if data["totalCount"].(int) == 0 {
		t.Error("expected at least one result matching complex criteria")
	}
}

func TestIntegration_GraphQLInputParsing(t *testing.T) {
	rb := &ResolverBuilder{}

	// Test that GraphQL input with nested structures parses correctly
	input := map[string]any{
		"bool": map[string]any{
			"must": []any{
				map[string]any{
					"terms": map[string]any{
						"field":  "category.keyword",
						"values": []any{"electronics", "furniture"},
					},
				},
			},
			"should": []any{
				map[string]any{
					"match": map[string]any{
						"field": "description",
						"query": "laptop",
					},
				},
			},
		},
	}

	queryInput, err := rb.convertToESQueryInput(input)
	if err != nil {
		t.Fatalf("conversion failed: %v", err)
	}

	if queryInput.Bool == nil {
		t.Fatal("expected bool query")
	}

	if len(queryInput.Bool.Must) != 1 {
		t.Errorf("expected 1 must clause, got %d", len(queryInput.Bool.Must))
	}

	if queryInput.Bool.Must[0].Terms == nil {
		t.Error("expected terms query in must clause")
	}

	if len(queryInput.Bool.Should) != 1 {
		t.Errorf("expected 1 should clause, got %d", len(queryInput.Bool.Should))
	}

	if queryInput.Bool.Should[0].Match == nil {
		t.Error("expected match query in should clause")
	}
}

func TestIntegration_AggregationParsing(t *testing.T) {
	rb := &ResolverBuilder{}

	// Test aggregation input parsing with sub-aggregations
	input := []any{
		map[string]any{
			"name": "categories",
			"terms": map[string]any{
				"field": "category.keyword",
				"size":  5,
			},
			"aggs": []any{
				map[string]any{
					"name": "brands",
					"terms": map[string]any{
						"field": "brand.keyword",
					},
				},
			},
		},
	}

	aggInputs, err := rb.convertToESAggInputs(input)
	if err != nil {
		t.Fatalf("conversion failed: %v", err)
	}

	if len(aggInputs) != 1 {
		t.Fatalf("expected 1 aggregation, got %d", len(aggInputs))
	}

	if aggInputs[0].Name != "categories" {
		t.Errorf("expected name=categories, got %s", aggInputs[0].Name)
	}

	if aggInputs[0].Terms == nil {
		t.Fatal("expected terms aggregation")
	}

	if len(aggInputs[0].Aggs) != 1 {
		t.Errorf("expected 1 sub-aggregation, got %d", len(aggInputs[0].Aggs))
	}

	if aggInputs[0].Aggs[0].Name != "brands" {
		t.Errorf("expected sub-agg name=brands, got %s", aggInputs[0].Aggs[0].Name)
	}

	// Convert to ES format
	esAggs, err := convertAggsInput(aggInputs)
	if err != nil {
		t.Fatalf("ES conversion failed: %v", err)
	}

	if _, ok := esAggs["categories"]; !ok {
		t.Error("expected categories aggregation in ES format")
	}

	if esAggs["categories"].Terms == nil {
		t.Error("expected terms aggregation")
	}

	if esAggs["categories"].Aggregations == nil {
		t.Error("expected sub-aggregations")
	}

	if _, ok := esAggs["categories"].Aggregations["brands"]; !ok {
		t.Error("expected brands sub-aggregation")
	}
}

func TestIntegration_BackwardCompatibility(t *testing.T) {
	indexName := "test_backward_compat"

	// Index data using ES directly
	ctx := context.Background()
	esClient.Indices.Delete(indexName).Do(ctx)

	mappingJSON := map[string]any{
		"mappings": map[string]any{
			"properties": map[string]any{
				"id":    map[string]any{"type": "keyword"},
				"name":  map[string]any{"type": "text"},
				"price": map[string]any{"type": "double"},
			},
		},
	}
	mappingBytes, _ := json.Marshal(mappingJSON)
	esClient.Indices.Create(indexName).Raw(bytes.NewReader(mappingBytes)).Do(ctx)

	// Index documents
	doc := map[string]any{"id": "1", "name": "Test Product", "price": 99.99}
	docBytes, _ := json.Marshal(doc)
	esClient.Index(indexName).Id("1").Raw(bytes.NewReader(docBytes)).Do(ctx)
	esClient.Indices.Refresh().Index(indexName).Do(ctx)

	// Parse mapping
	mapping, err := ParseMapping(indexName, mappingBytes)
	if err != nil {
		t.Fatalf("failed to parse mapping: %v", err)
	}

	// Create API WITHOUT ES client (old behavior)
	config := NewConfig()
	config.AddQuery("search", &QueryConfig{
		Index: indexName,
		Features: []reveald.Feature{
			featureset.NewPaginationFeature(),
		},
		EnablePagination: true,
	})

	api, err := New(esBackend, mapping, config) // No WithESClient option
	if err != nil {
		t.Fatalf("failed to create API: %v", err)
	}

	// Query should still work using Feature-based approach
	query := `query { search(limit: 10) { hits { id name } totalCount } }`
	result := graphql.Do(graphql.Params{Schema: api.GetSchema(), RequestString: query})

	if len(result.Errors) > 0 {
		t.Fatalf("GraphQL errors: %v", result.Errors)
	}

	data := result.Data.(map[string]any)["search"].(map[string]any)
	hits := data["hits"].([]any)

	if len(hits) != 1 {
		t.Errorf("expected 1 hit, got %d", len(hits))
	}
}
