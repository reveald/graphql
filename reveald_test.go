//go:build integration

package graphql

import (
	"context"
	"fmt"
	"testing"

	"github.com/elastic/go-elasticsearch/v8"
)

func TestRevealdFeatureBasedQueries(t *testing.T) {
	ctx := context.Background()

	// Start Elasticsearch container
	tc := StartElasticsearch(ctx, t)
	defer tc.Cleanup(ctx, t)

	indexName := "test-leads-reveald"

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
			"createdAt": { "type": "date" },
			"customerName": { "type": "text" },
			"customerEmail": { "type": "keyword" }
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

	// Create config with reveald features
	config := NewConfig()
	config.AddQuery("searchLeads", &QueryConfig{
		Indices:            []string{indexName},
		EnableAggregations: true,
		EnablePagination:   true,
	})

	// Create API
	api, err := New(backend, mapping, config, WithESClient(esClient))
	if err != nil {
		t.Fatalf("Failed to create API: %v", err)
	}

	schema := api.GetSchema()

	t.Run("Basic search query", func(t *testing.T) {
		query := `{
			searchLeads {
				totalCount
				hits {
					id
					leadType
					branchMarketCode
				}
			}
		}`

		result := ExecuteGraphQLQuery(t, schema, query, nil)
		AssertNoErrors(t, result)

		data := result.Data.(map[string]interface{})
		searchData := data["searchLeads"].(map[string]interface{})

		totalCount := int(searchData["totalCount"].(float64))
		if totalCount == 0 {
			t.Error("Expected non-zero totalCount")
		}

		hits := searchData["hits"].([]interface{})
		if len(hits) == 0 {
			t.Error("Expected non-empty hits")
		}

		// Verify hit structure
		hit := hits[0].(map[string]interface{})
		if hit["id"] == nil {
			t.Error("Hit missing id")
		}
		if hit["leadType"] == nil {
			t.Error("Hit missing leadType")
		}
	})

	t.Run("Search with pagination", func(t *testing.T) {
		query := `{
			searchLeads(limit: 5, offset: 0) {
				totalCount
				hits {
					id
				}
				pagination {
					limit
					offset
					totalCount
				}
			}
		}`

		result := ExecuteGraphQLQuery(t, schema, query, nil)
		AssertNoErrors(t, result)

		data := result.Data.(map[string]interface{})
		searchData := data["searchLeads"].(map[string]interface{})

		hits := searchData["hits"].([]interface{})
		if len(hits) > 5 {
			t.Errorf("Expected max 5 hits, got %d", len(hits))
		}

		pagination := searchData["pagination"].(map[string]interface{})
		if int(pagination["limit"].(float64)) != 5 {
			t.Error("Pagination limit mismatch")
		}
		if int(pagination["offset"].(float64)) != 0 {
			t.Error("Pagination offset mismatch")
		}
	})

	t.Run("Search with filters", func(t *testing.T) {
		query := `{
			searchLeads(filters: [{field: "leadType", value: "registration"}]) {
				totalCount
				hits {
					leadType
				}
			}
		}`

		result := ExecuteGraphQLQuery(t, schema, query, nil)
		AssertNoErrors(t, result)

		data := result.Data.(map[string]interface{})
		searchData := data["searchLeads"].(map[string]interface{})

		hits := searchData["hits"].([]interface{})
		for _, h := range hits {
			hit := h.(map[string]interface{})
			if hit["leadType"] != "registration" {
				t.Errorf("Expected only registration leads, got: %v", hit["leadType"])
			}
		}
	})

}

func TestRevealdFieldNameConversion(t *testing.T) {
	ctx := context.Background()

	tc := StartElasticsearch(ctx, t)
	defer tc.Cleanup(ctx, t)

	indexName := "test-field-conversion"

	// Create index with mapping including dotted field names
	CreateTestIndex(ctx, t, tc.URI, indexName)

	// Create test data
	leads := []LeadDocument{
		{
			ID:               "lead-1",
			LeadType:         "registration",
			BranchMarketCode: "SE",
		},
	}
	IndexTestData(ctx, t, tc.URI, indexName, leads)

	mappingJSON := []byte(fmt.Sprintf(`{
		"index": "%s",
		"properties": {
			"id": { "type": "keyword" },
			"leadType": { "type": "keyword" },
			"branchMarketCode": { "type": "keyword" }
		}
	}`, indexName))

	mapping, err := ParseMapping(indexName, mappingJSON)
	if err != nil {
		t.Fatalf("Failed to parse mapping: %v", err)
	}

	esClient, err := elasticsearch.NewTypedClient(elasticsearch.Config{
		Addresses: []string{tc.URI},
	})
	if err != nil {
		t.Fatalf("Failed to create ES client: %v", err)
	}

	backend := CreateTestBackend(t, tc.URI)

	config := NewConfig()
	config.AddQuery("testQuery", &QueryConfig{
		Indices: []string{indexName},
	})

	api, err := New(backend, mapping, config, WithESClient(esClient))
	if err != nil {
		t.Fatalf("Failed to create API: %v", err)
	}

	schema := api.GetSchema()

	t.Run("Field with underscore resolves correctly", func(t *testing.T) {
		// GraphQL uses underscores, ES uses CamelCase or dots
		query := `{
			testQuery(filters: [{field: "branchMarketCode", value: "SE"}]) {
				totalCount
				hits {
					branchMarketCode
				}
			}
		}`

		result := ExecuteGraphQLQuery(t, schema, query, nil)
		AssertNoErrors(t, result)

		data := result.Data.(map[string]interface{})
		testData := data["testQuery"].(map[string]interface{})

		hits := testData["hits"].([]interface{})
		if len(hits) == 0 {
			t.Error("Expected hits with branchMarketCode filter")
		}

		hit := hits[0].(map[string]interface{})
		if hit["branchMarketCode"] != "SE" {
			t.Errorf("Expected branchMarketCode SE, got %v", hit["branchMarketCode"])
		}
	})
}
