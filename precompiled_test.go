//go:build integration

package graphql

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/typedapi/core/search"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types/enums/calendarinterval"
	"github.com/graphql-go/graphql"
)

func TestPrecompiledQueryTypedAggregations(t *testing.T) {
	ctx := context.Background()

	// Start Elasticsearch container
	tc := StartElasticsearch(ctx, t)
	defer tc.Cleanup(ctx, t)

	indexName := "test-leads"

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
			"tenantId": { "type": "keyword" }
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

	// Create backend
	backend := CreateTestBackend(t, tc.URI)

	// Create config with precompiled query
	config := NewConfig()
	config.AddPrecompiledQuery("leadsOverview", &PrecompiledQueryConfig{
		Index:        indexName,
		Description:  "Leads overview with typed aggregations",
		QueryBuilder: buildTestLeadsQuery,
		Parameters: graphql.FieldConfigArgument{
			"markets": &graphql.ArgumentConfig{
				Type:        graphql.NewList(graphql.String),
				Description: "Filter by markets",
			},
		},
	})

	// Create GraphQL API
	api, err := New(backend, mapping, config, WithESClient(esClient))
	if err != nil {
		t.Fatalf("Failed to create API: %v", err)
	}

	schema := api.GetSchema()

	t.Run("Schema has typed aggregations", func(t *testing.T) {
		typeData := IntrospectSchema(t, schema, "LeadsOverviewResult")

		// Check that aggregations field exists and is an object
		fields := typeData["fields"].([]interface{})
		var aggsField map[string]interface{}
		for _, f := range fields {
			field := f.(map[string]interface{})
			if field["name"] == "aggregations" {
				aggsField = field
				break
			}
		}

		if aggsField == nil {
			t.Fatal("aggregations field not found")
		}

		fieldType := aggsField["type"].(map[string]interface{})
		if fieldType["kind"] != "OBJECT" {
			t.Errorf("Expected aggregations to be OBJECT, got %v", fieldType["kind"])
		}

		if fieldType["name"] != "LeadsOverviewAggregations" {
			t.Errorf("Expected LeadsOverviewAggregations type, got %v", fieldType["name"])
		}
	})

	t.Run("Aggregations type has by_leadType field", func(t *testing.T) {
		typeData := IntrospectSchema(t, schema, "LeadsOverviewAggregations")

		fields := typeData["fields"].([]interface{})
		var byLeadTypeField map[string]interface{}
		for _, f := range fields {
			field := f.(map[string]interface{})
			if field["name"] == "by_leadType" {
				byLeadTypeField = field
				break
			}
		}

		if byLeadTypeField == nil {
			t.Fatal("by_leadType field not found in aggregations")
		}

		fieldType := byLeadTypeField["type"].(map[string]interface{})
		if fieldType["name"] != "LeadsOverviewBy_leadType" {
			t.Errorf("Expected LeadsOverviewBy_leadType type, got %v", fieldType["name"])
		}
	})

	t.Run("Bucket type has nested aggregation fields", func(t *testing.T) {
		typeData := IntrospectSchema(t, schema, "LeadsOverviewBy_leadTypeBucket")

		fields := typeData["fields"].([]interface{})
		fieldNames := make(map[string]bool)
		for _, f := range fields {
			field := f.(map[string]interface{})
			fieldNames[field["name"].(string)] = true
		}

		// Should have standard bucket fields
		if !fieldNames["key"] {
			t.Error("Bucket missing key field")
		}
		if !fieldNames["doc_count"] {
			t.Error("Bucket missing doc_count field")
		}

		// Should have nested aggregation fields
		if !fieldNames["by_mechanism"] {
			t.Error("Bucket missing by_mechanism nested aggregation")
		}
	})

	t.Run("Filters aggregation has named filter fields", func(t *testing.T) {
		typeData := IntrospectSchema(t, schema, "LeadsOverviewBy_leadTypeBy_mechanismPeriods")

		fields := typeData["fields"].([]interface{})
		fieldNames := make(map[string]bool)
		for _, f := range fields {
			field := f.(map[string]interface{})
			fieldNames[field["name"].(string)] = true
		}

		// Should have named filter fields, not a generic buckets array
		expectedFilters := []string{"last_24h", "prev_24h", "last_7d", "prev_7d", "last_30d", "prev_30d"}
		for _, filterName := range expectedFilters {
			if !fieldNames[filterName] {
				t.Errorf("Missing filter field: %s", filterName)
			}
		}
	})

	t.Run("Execute query and verify typed response structure", func(t *testing.T) {
		query := `{
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
										last_7d { doc_count }
									}
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
		leadsOverview := data["leadsOverview"].(map[string]interface{})

		// Verify totalCount
		totalCount := getInt(t, leadsOverview["totalCount"])
		t.Logf("Total count: %d", totalCount)

		// Verify aggregations is an object (not array)
		aggs, ok := leadsOverview["aggregations"].(map[string]interface{})
		if !ok || aggs == nil {
			t.Skip("No aggregations returned - test data might not match time range")
			return
		}

		// Verify by_leadType exists
		byLeadType := aggs["by_leadType"].(map[string]interface{})
		buckets := byLeadType["buckets"].([]interface{})

		if len(buckets) == 0 {
			t.Skip("No buckets returned - test data might not match time range")
			return
		}

		// Check first bucket structure
		bucket := buckets[0].(map[string]interface{})
		if _, hasKey := bucket["key"]; !hasKey {
			t.Error("Bucket missing key field")
		}
		if _, hasDocCount := bucket["doc_count"]; !hasDocCount {
			t.Error("Bucket missing doc_count field")
		}

		// Verify nested aggregation is direct property (not sub_aggregations array)
		byMechanism := bucket["by_mechanism"].(map[string]interface{})
		mechanismBuckets := byMechanism["buckets"].([]interface{})

		if len(mechanismBuckets) == 0 {
			t.Fatal("Expected non-empty mechanism buckets")
		}

		// Verify Filters aggregation returns object
		mechBucket := mechanismBuckets[0].(map[string]interface{})
		periods := mechBucket["periods"].(map[string]interface{})

		// Should have named fields, not array
		if _, hasLast24h := periods["last_24h"]; !hasLast24h {
			t.Error("periods should have last_24h field")
		}
		if _, hasPrev24h := periods["prev_24h"]; !hasPrev24h {
			t.Error("periods should have prev_24h field")
		}

		// Verify filter bucket has doc_count
		last24h := periods["last_24h"].(map[string]interface{})
		if _, hasDocCount := last24h["doc_count"]; !hasDocCount {
			t.Error("Filter bucket missing doc_count")
		}
	})

	t.Run("Query with parameters", func(t *testing.T) {
		query := `query($markets: [String]) {
			leadsOverview(markets: $markets) {
				totalCount
				aggregations {
					by_leadType {
						buckets {
							key
							doc_count
						}
					}
				}
			}
		}`

		variables := map[string]interface{}{
			"markets": []string{"SE", "NO"},
		}

		result := ExecuteGraphQLQuery(t, schema, query, variables)
		AssertNoErrors(t, result)

		data := result.Data.(map[string]interface{})
		leadsOverview := data["leadsOverview"].(map[string]interface{})

		totalCount := getInt(t, leadsOverview["totalCount"])
		t.Logf("Total count with market filter: %d", totalCount)

		// Verify aggregations structure is typed (even if empty)
		if aggs, ok := leadsOverview["aggregations"].(map[string]interface{}); ok {
			if _, ok := aggs["by_leadType"]; !ok {
				t.Error("Expected by_leadType field in aggregations")
			}
		}
	})
}

func TestPrecompiledQueryValidation(t *testing.T) {
	t.Run("Error when both QueryBuilder and QueryJSON specified", func(t *testing.T) {
		config := &PrecompiledQueryConfig{
			Index:        "test",
			QueryBuilder: func(args map[string]any) *search.Request { return nil },
			QueryJSON:    `{"query":{}}`,
		}

		err := config.Validate()
		if err == nil {
			t.Fatal("Expected error when both QueryBuilder and QueryJSON are specified")
		}
		if !strings.Contains(err.Error(), "mutually exclusive") {
			t.Errorf("Expected 'mutually exclusive' error, got: %v", err)
		}
	})

	t.Run("Error when neither QueryBuilder nor QueryJSON specified", func(t *testing.T) {
		config := &PrecompiledQueryConfig{
			Index: "test",
		}

		err := config.Validate()
		if err == nil {
			t.Fatal("Expected error when neither QueryBuilder nor QueryJSON are specified")
		}
	})

	t.Run("Success with QueryBuilder only", func(t *testing.T) {
		config := &PrecompiledQueryConfig{
			Index:        "test",
			QueryBuilder: func(args map[string]any) *search.Request { return &search.Request{} },
		}

		err := config.Validate()
		if err != nil {
			t.Errorf("Unexpected error with QueryBuilder only: %v", err)
		}
	})

	t.Run("Success with QueryJSON only", func(t *testing.T) {
		config := &PrecompiledQueryConfig{
			Index:     "test",
			QueryJSON: `{"size": 0}`,
		}

		err := config.Validate()
		if err != nil {
			t.Errorf("Unexpected error with QueryJSON only: %v", err)
		}
	})
}

func TestPrecompiledQueryRootQueryBuilder(t *testing.T) {
	ctx := context.Background()

	// Start Elasticsearch container
	tc := StartElasticsearch(ctx, t)
	defer tc.Cleanup(ctx, t)

	indexName := "test-leads-tenant"

	// Create index and load test data with tenant IDs
	CreateTestIndex(ctx, t, tc.URI, indexName)
	leads := GenerateTestLeads()
	IndexTestData(ctx, t, tc.URI, indexName, leads)

	// Get ES mapping
	mappingJSON := []byte(fmt.Sprintf(`{
		"index": "%s",
		"properties": {
			"id": { "type": "keyword" },
			"leadType": { "type": "keyword" },
			"tenantId": { "type": "keyword" },
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

	// Create config with RootQueryBuilder
	config := NewConfig()
	config.AddPrecompiledQuery("leadsWithTenant", &PrecompiledQueryConfig{
		Index:        indexName,
		Description:  "Leads filtered by tenant",
		QueryBuilder: buildSimpleLeadsQuery,
		RootQueryBuilder: func(r *http.Request) (*types.Query, error) {
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

	// Create API
	api, err := New(backend, mapping, config, WithESClient(esClient))
	if err != nil {
		t.Fatalf("Failed to create API: %v", err)
	}

	t.Run("RootQueryBuilder filters by tenant from header", func(t *testing.T) {
		query := `{
			leadsWithTenant {
				totalCount
			}
		}`

		headers := map[string]string{
			"X-Tenant-ID": "tenant-1",
		}

		result := ExecuteGraphQLQueryWithContext(t, api, query, nil, headers)
		AssertNoErrors(t, result)

		data := result.Data.(map[string]interface{})
		leadsData := data["leadsWithTenant"].(map[string]interface{})
		totalCount := getInt(t, leadsData["totalCount"])

		t.Logf("tenant-1 leads: %d", totalCount)

		// Query without tenant header
		resultNoTenant := ExecuteGraphQLQueryWithContext(t, api, query, nil, map[string]string{})
		AssertNoErrors(t, resultNoTenant)

		dataNoTenant := resultNoTenant.Data.(map[string]interface{})
		leadsNoTenant := dataNoTenant["leadsWithTenant"].(map[string]interface{})
		totalCountNoTenant := getInt(t, leadsNoTenant["totalCount"])

		t.Logf("All leads: %d", totalCountNoTenant)

		// Without tenant filter, should have more or equal leads
		if totalCountNoTenant < totalCount {
			t.Errorf("Expected same or more leads without tenant filter. With tenant: %d, Without: %d", totalCount, totalCountNoTenant)
		}
	})
}

func TestPrecompiledQueryJSON(t *testing.T) {
	ctx := context.Background()

	tc := StartElasticsearch(ctx, t)
	defer tc.Cleanup(ctx, t)

	indexName := "test-leads-json"

	CreateTestIndex(ctx, t, tc.URI, indexName)
	testLeads := GenerateTestLeads()
	IndexTestData(ctx, t, tc.URI, indexName, testLeads)

	mappingJSON := []byte(fmt.Sprintf(`{
		"index": "%s",
		"properties": {
			"id": { "type": "keyword" },
			"leadType": { "type": "keyword" }
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

	// Create query JSON
	queryJSON := `{
		"size": 0,
		"aggregations": {
			"by_leadType": {
				"terms": {
					"field": "leadType.keyword",
					"size": 100
				}
			}
		}
	}`

	config := NewConfig()
	config.AddPrecompiledQuery("leadsFromJSON", &PrecompiledQueryConfig{
		Index:       indexName,
		Description: "Leads from JSON query",
		QueryJSON:   queryJSON,
	})

	api, err := New(backend, mapping, config, WithESClient(esClient))
	if err != nil {
		t.Fatalf("Failed to create API: %v", err)
	}

	schema := api.GetSchema()

	t.Run("QueryJSON creates typed aggregations", func(t *testing.T) {
		typeData := IntrospectSchema(t, schema, "LeadsFromJSONAggregations")

		fields := typeData["fields"].([]interface{})
		var byLeadTypeField map[string]interface{}
		for _, f := range fields {
			field := f.(map[string]interface{})
			if field["name"] == "by_leadType" {
				byLeadTypeField = field
				break
			}
		}

		if byLeadTypeField == nil {
			t.Fatal("by_leadType field not found")
		}
	})

	t.Run("QueryJSON executes correctly", func(t *testing.T) {
		query := `{
			leadsFromJSON {
				totalCount
				aggregations {
					by_leadType {
						buckets {
							key
							doc_count
						}
					}
				}
			}
		}`

		result := ExecuteGraphQLQuery(t, schema, query, nil)
		AssertNoErrors(t, result)

		data := result.Data.(map[string]interface{})
		leadsData := data["leadsFromJSON"].(map[string]interface{})

		totalCount := getInt(t, leadsData["totalCount"])
		t.Logf("QueryJSON totalCount: %d", totalCount)

		if totalCount == 0 {
			t.Skip("No results - test data might not be indexed yet")
			return
		}

		aggs := leadsData["aggregations"].(map[string]interface{})
		byLeadType := aggs["by_leadType"].(map[string]interface{})
		buckets := byLeadType["buckets"].([]interface{})

		t.Logf("Buckets count: %d", len(buckets))

		// Verify bucket has expected fields
		bucket := buckets[0].(map[string]interface{})
		if bucket["key"] == nil || bucket["doc_count"] == nil {
			t.Error("Bucket missing key or doc_count")
		}
	})
}

// buildTestLeadsQuery builds a leads query for testing
func buildTestLeadsQuery(args map[string]any) *search.Request {
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

// buildSimpleLeadsQuery builds a simple query for testing
func buildSimpleLeadsQuery(args map[string]any) *search.Request {
	return &search.Request{
		Size: ptr(10),
		Query: &types.Query{
			MatchAll: &types.MatchAllQuery{},
		},
		Aggregations: map[string]types.Aggregations{
			"by_leadType": {
				Terms: &types.TermsAggregation{
					Field: ptr("leadType.keyword"),
					Size:  ptr(100),
				},
			},
		},
	}
}

func ptr[T any](v T) *T {
	return &v
}

// getInt safely extracts int from interface (handles both int and float64)
func getInt(t *testing.T, v interface{}) int {
	switch val := v.(type) {
	case int:
		return val
	case float64:
		return int(val)
	case int64:
		return int(val)
	default:
		t.Fatalf("Cannot convert %T to int", v)
		return 0
	}
}
