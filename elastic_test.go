package graphql

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	elasticsearch "github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types"
	"github.com/reveald/reveald/v2"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

var (
	esContainer testcontainers.Container
	esClient    *elasticsearch.TypedClient
	esBackend   reveald.Backend
	esAddr      string
)

// TestMain sets up the Elasticsearch container once for all tests
func TestMain(m *testing.M) {
	ctx := context.Background()

	// Start Elasticsearch container
	req := testcontainers.ContainerRequest{
		Image:        "docker.elastic.co/elasticsearch/elasticsearch:8.11.0",
		ExposedPorts: []string{"9200/tcp"},
		Env: map[string]string{
			"discovery.type":         "single-node",
			"xpack.security.enabled": "false",
			"ES_JAVA_OPTS":           "-Xms512m -Xmx512m",
		},
		WaitingFor: wait.ForHTTP("/").
			WithPort("9200").
			WithStartupTimeout(180 * time.Second).
			WithPollInterval(2 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		panic(fmt.Sprintf("failed to start container: %v", err))
	}

	esContainer = container

	// Get container address
	host, err := container.Host(ctx)
	if err != nil {
		panic(err)
	}
	port, err := container.MappedPort(ctx, "9200")
	if err != nil {
		panic(err)
	}
	esAddr = fmt.Sprintf("http://%s:%s", host, port.Port())

	// Create ES client
	esClient, err = elasticsearch.NewTypedClient(elasticsearch.Config{
		Addresses: []string{esAddr},
	})
	if err != nil {
		panic(err)
	}

	// Create reveald backend
	esBackend, err = reveald.NewElasticBackend([]string{esAddr})
	if err != nil {
		panic(err)
	}

	// Run tests
	code := m.Run()

	// Cleanup
	if err := container.Terminate(ctx); err != nil {
		fmt.Printf("failed to terminate container: %v\n", err)
	}

	os.Exit(code)
}

// setupTestIndex creates a test index with sample data
func setupTestIndex(t *testing.T, indexName string) {
	ctx := context.Background()

	// Delete index if exists
	esClient.Indices.Delete(indexName).Do(ctx)

	// Create index with mapping
	mapping := map[string]any{
		"mappings": map[string]any{
			"properties": map[string]any{
				"id":          map[string]any{"type": "keyword"},
				"name":        map[string]any{"type": "text", "fields": map[string]any{"keyword": map[string]any{"type": "keyword"}}},
				"description": map[string]any{"type": "text"},
				"category":    map[string]any{"type": "text", "fields": map[string]any{"keyword": map[string]any{"type": "keyword"}}},
				"brand":       map[string]any{"type": "text", "fields": map[string]any{"keyword": map[string]any{"type": "keyword"}}},
				"price":       map[string]any{"type": "double"},
				"stock":       map[string]any{"type": "integer"},
				"active":      map[string]any{"type": "boolean"},
				"rating":      map[string]any{"type": "float"},
				"created_at":  map[string]any{"type": "date"},
			},
		},
	}

	mappingJSON, _ := json.Marshal(mapping)
	_, err := esClient.Indices.Create(indexName).Raw(bytes.NewReader(mappingJSON)).Do(ctx)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}

	// Index sample documents
	docs := []map[string]any{
		{
			"id": "1", "name": "Laptop Pro", "description": "High-performance laptop",
			"category": "electronics", "brand": "TechBrand", "price": 1299.99,
			"stock": 50, "active": true, "rating": 4.5, "created_at": "2024-01-15T10:00:00Z",
		},
		{
			"id": "2", "name": "Wireless Mouse", "description": "Ergonomic mouse",
			"category": "electronics", "brand": "TechBrand", "price": 29.99,
			"stock": 200, "active": true, "rating": 4.2, "created_at": "2024-01-20T10:00:00Z",
		},
		{
			"id": "3", "name": "Office Chair", "description": "Comfortable chair",
			"category": "furniture", "brand": "ComfortSeats", "price": 299.99,
			"stock": 30, "active": true, "rating": 4.7, "created_at": "2024-02-01T10:00:00Z",
		},
		{
			"id": "4", "name": "USB Cable", "description": "Fast charging cable",
			"category": "electronics", "brand": "TechBrand", "price": 19.99,
			"stock": 500, "active": false, "rating": 4.0, "created_at": "2024-02-10T10:00:00Z",
		},
		{
			"id": "5", "name": "Standing Desk", "description": "Adjustable desk",
			"category": "furniture", "brand": "ComfortSeats", "price": 599.99,
			"stock": 20, "active": true, "rating": 4.8, "created_at": "2024-03-01T10:00:00Z",
		},
	}

	for _, doc := range docs {
		docJSON, _ := json.Marshal(doc)
		_, err := esClient.Index(indexName).Id(doc["id"].(string)).Raw(bytes.NewReader(docJSON)).Do(ctx)
		if err != nil {
			t.Fatalf("failed to index document: %v", err)
		}
	}

	// Refresh index
	esClient.Indices.Refresh().Index(indexName).Do(ctx)
}

func TestTypedQuery_Term(t *testing.T) {
	indexName := "test_term_query"
	setupTestIndex(t, indexName)

	ctx := context.Background()
	query := &types.Query{
		Term: map[string]types.TermQuery{
			"brand.keyword": {Value: "TechBrand"},
		},
	}

	result, err := executeTypedQuery(ctx, esClient, []string{indexName}, query, nil, nil, nil)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}

	if result.TotalHitCount != 3 {
		t.Errorf("expected 3 hits, got %d", result.TotalHitCount)
	}

	// Verify all results are TechBrand
	for _, hit := range result.Hits {
		if hit["brand"] != "TechBrand" {
			t.Errorf("expected brand=TechBrand, got %v", hit["brand"])
		}
	}
}

func TestTypedQuery_Terms(t *testing.T) {
	indexName := "test_terms_query"
	setupTestIndex(t, indexName)

	ctx := context.Background()
	query := &types.Query{
		Terms: &types.TermsQuery{
			TermsQuery: map[string]types.TermsQueryField{
				"category.keyword": []types.FieldValue{"electronics", "furniture"},
			},
		},
	}

	result, err := executeTypedQuery(ctx, esClient, []string{indexName}, query, nil, nil, nil)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}

	if result.TotalHitCount != 5 {
		t.Errorf("expected 5 hits, got %d", result.TotalHitCount)
	}
}

func TestTypedQuery_Range(t *testing.T) {
	indexName := "test_range_query"
	setupTestIndex(t, indexName)

	ctx := context.Background()
	gte := types.Float64(100)
	lte := types.Float64(500)
	query := &types.Query{
		Range: map[string]types.RangeQuery{
			"price": types.NumberRangeQuery{
				Gte: &gte,
				Lte: &lte,
			},
		},
	}

	result, err := executeTypedQuery(ctx, esClient, []string{indexName}, query, nil, nil, nil)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}

	if result.TotalHitCount != 1 {
		t.Errorf("expected 1 hit (Office Chair), got %d", result.TotalHitCount)
	}

	if len(result.Hits) > 0 {
		if result.Hits[0]["name"] != "Office Chair" {
			t.Errorf("expected Office Chair, got %v", result.Hits[0]["name"])
		}
	}
}

func TestTypedQuery_Bool(t *testing.T) {
	indexName := "test_bool_query"
	setupTestIndex(t, indexName)

	ctx := context.Background()
	query := &types.Query{
		Bool: &types.BoolQuery{
			Must: []types.Query{
				{Term: map[string]types.TermQuery{"category.keyword": {Value: "electronics"}}},
				{Exists: &types.ExistsQuery{Field: "description"}},
			},
			Filter: []types.Query{
				{Term: map[string]types.TermQuery{"active": {Value: true}}},
			},
		},
	}

	result, err := executeTypedQuery(ctx, esClient, []string{indexName}, query, nil, nil, nil)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}

	if result.TotalHitCount != 2 {
		t.Errorf("expected 2 active electronics with descriptions, got %d", result.TotalHitCount)
	}

	// Verify all results meet criteria
	for _, hit := range result.Hits {
		if hit["category"] != "electronics" {
			t.Errorf("expected category=electronics, got %v", hit["category"])
		}
		if hit["active"] != true {
			t.Errorf("expected active=true, got %v", hit["active"])
		}
		if _, ok := hit["description"]; !ok {
			t.Errorf("expected description field to exist")
		}
	}
}

func TestTypedQuery_Pagination(t *testing.T) {
	indexName := "test_pagination"
	setupTestIndex(t, indexName)

	ctx := context.Background()
	query := &types.Query{
		Term: map[string]types.TermQuery{
			"active": {Value: true},
		},
	}

	// Test limit
	limit := 2
	result, err := executeTypedQuery(ctx, esClient, []string{indexName}, query, nil, &limit, nil)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}

	if len(result.Hits) != 2 {
		t.Errorf("expected 2 hits with limit=2, got %d", len(result.Hits))
	}

	if result.Pagination.PageSize != 2 {
		t.Errorf("expected PageSize=2, got %d", result.Pagination.PageSize)
	}

	// Test offset
	offset := 2
	result, err = executeTypedQuery(ctx, esClient, []string{indexName}, query, nil, &limit, &offset)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}

	if result.Pagination.Offset != 2 {
		t.Errorf("expected Offset=2, got %d", result.Pagination.Offset)
	}

	if len(result.Hits) != 2 {
		t.Errorf("expected 2 hits with limit=2, offset=2, got %d", len(result.Hits))
	}
}

func TestTypedQuery_Aggregations(t *testing.T) {
	indexName := "test_aggregations"
	setupTestIndex(t, indexName)

	ctx := context.Background()

	// Terms aggregation
	aggs := map[string]types.Aggregations{
		"brands": {
			Terms: &types.TermsAggregation{
				Field: stringPtr("brand.keyword"),
			},
		},
	}

	result, err := executeTypedQuery(ctx, esClient, []string{indexName}, nil, aggs, nil, nil)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}

	brandAggs, ok := result.Aggregations["brands"]
	if !ok {
		t.Fatal("expected brands aggregation")
	}

	if len(brandAggs) != 2 {
		t.Errorf("expected 2 brand buckets (TechBrand, ComfortSeats), got %d", len(brandAggs))
	}

	// Verify bucket counts
	for _, bucket := range brandAggs {
		if bucket.Value == "TechBrand" && bucket.HitCount != 3 {
			t.Errorf("expected TechBrand count=3, got %d", bucket.HitCount)
		}
		if bucket.Value == "ComfortSeats" && bucket.HitCount != 2 {
			t.Errorf("expected ComfortSeats count=2, got %d", bucket.HitCount)
		}
	}
}

func TestTypedQuery_RootQueryMerge(t *testing.T) {
	indexName := "test_root_query"
	setupTestIndex(t, indexName)

	ctx := context.Background()

	// Root query: only active products
	rootQuery := &types.Query{
		Term: map[string]types.TermQuery{
			"active": {Value: true},
		},
	}

	// User query: only TechBrand
	userQuery := &types.Query{
		Term: map[string]types.TermQuery{
			"brand.keyword": {Value: "TechBrand"},
		},
	}

	// Merge queries
	finalQuery := mergeQueries(rootQuery, userQuery)

	result, err := executeTypedQuery(ctx, esClient, []string{indexName}, finalQuery, nil, nil, nil)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}

	// Should get active TechBrand products only (2 products: Laptop, Mouse)
	if result.TotalHitCount != 2 {
		t.Errorf("expected 2 active TechBrand products, got %d", result.TotalHitCount)
	}

	// Verify all results meet both criteria
	for _, hit := range result.Hits {
		if hit["active"] != true {
			t.Errorf("expected active=true, got %v", hit["active"])
		}
		if hit["brand"] != "TechBrand" {
			t.Errorf("expected brand=TechBrand, got %v", hit["brand"])
		}
	}
}

func TestGraphQL_FlexibleSearch(t *testing.T) {
	indexName := "test_graphql_flexible"
	setupTestIndex(t, indexName)

	// Create mapping
	mappingJSON := []byte(`{
		"mappings": {
			"properties": {
				"id": {"type": "keyword"},
				"name": {"type": "text", "fields": {"keyword": {"type": "keyword"}}},
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

	// Create config with flexible querying
	config := NewConfig()
	config.AddQuery("flexibleSearch", &QueryConfig{
		Mapping:               mapping,
		EnableElasticQuerying: true,
		EnableAggregations:    true,
		EnablePagination:      true,
	})

	// Create API
	_, err = New(esBackend, config, WithESClient(esClient))
	if err != nil {
		t.Fatalf("failed to create API: %v", err)
	}

	// Schema created successfully - core functionality works
}

func TestConverter_ESQueryInput(t *testing.T) {
	rb := &ResolverBuilder{}

	// Test term query conversion
	input := map[string]any{
		"term": map[string]any{
			"field": "brand.keyword",
			"value": "TechBrand",
		},
	}

	queryInput, err := rb.convertToESQueryInput(input)
	if err != nil {
		t.Fatalf("conversion failed: %v", err)
	}

	if queryInput.Term == nil {
		t.Fatal("expected term query")
	}

	if queryInput.Term.Field != "brand.keyword" {
		t.Errorf("expected field=brand.keyword, got %s", queryInput.Term.Field)
	}

	if queryInput.Term.Value != "TechBrand" {
		t.Errorf("expected value=TechBrand, got %s", queryInput.Term.Value)
	}
}

func TestConverter_ESAggInput(t *testing.T) {
	rb := &ResolverBuilder{}

	// Test terms aggregation conversion
	input := []any{
		map[string]any{
			"name": "brands",
			"terms": map[string]any{
				"field": "brand.keyword",
				"size":  10,
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

	if aggInputs[0].Name != "brands" {
		t.Errorf("expected name=brands, got %s", aggInputs[0].Name)
	}

	if aggInputs[0].Terms == nil {
		t.Fatal("expected terms aggregation")
	}

	if aggInputs[0].Terms.Field != "brand.keyword" {
		t.Errorf("expected field=brand.keyword, got %s", aggInputs[0].Terms.Field)
	}
}

// Helper function
func stringPtr(s string) *string {
	return &s
}
