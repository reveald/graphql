//go:build integration

package graphql

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/graphql-go/graphql"
	"github.com/reveald/reveald"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// TestContainer wraps an Elasticsearch testcontainer
type TestContainer struct {
	Container testcontainers.Container
	URI       string
}

// StartElasticsearch starts an Elasticsearch container for testing
func StartElasticsearch(ctx context.Context, t *testing.T) *TestContainer {
	req := testcontainers.ContainerRequest{
		Image:        "docker.elastic.co/elasticsearch/elasticsearch:8.11.0",
		ExposedPorts: []string{"9200/tcp"},
		Env: map[string]string{
			"discovery.type":         "single-node",
			"xpack.security.enabled": "false",
			"ES_JAVA_OPTS":           "-Xms512m -Xmx512m",
		},
		WaitingFor: wait.ForHTTP("/").WithPort("9200/tcp").WithStartupTimeout(120 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("Failed to start container: %v", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("Failed to get container host: %v", err)
	}

	port, err := container.MappedPort(ctx, "9200")
	if err != nil {
		t.Fatalf("Failed to get container port: %v", err)
	}

	uri := fmt.Sprintf("http://%s:%s", host, port.Port())

	return &TestContainer{
		Container: container,
		URI:       uri,
	}
}

// Cleanup stops and removes the test container
func (tc *TestContainer) Cleanup(ctx context.Context, t *testing.T) {
	if err := tc.Container.Terminate(ctx); err != nil {
		t.Logf("Failed to terminate container: %v", err)
	}
}

// LeadDocument represents a test lead document
type LeadDocument struct {
	ID                    string    `json:"id"`
	LeadType              string    `json:"leadType"`
	LeadSourceMechanism   string    `json:"leadSourceMechanism"`
	BranchMarketCode      string    `json:"branchMarketCode"`
	CreatedAt             time.Time `json:"createdAt"`
	TenantID              string    `json:"tenantId,omitempty"`
	CustomerName          string    `json:"customerName,omitempty"`
	CustomerEmail         string    `json:"customerEmail,omitempty"`
}

// CreateTestIndex creates a test index with mapping
func CreateTestIndex(ctx context.Context, t *testing.T, esURI, indexName string) {
	cfg := elasticsearch.Config{
		Addresses: []string{esURI},
	}
	client, err := elasticsearch.NewClient(cfg)
	if err != nil {
		t.Fatalf("Failed to create ES client: %v", err)
	}

	// Create index with mapping
	mapping := `{
		"mappings": {
			"properties": {
				"id": { "type": "keyword" },
				"leadType": { "type": "keyword" },
				"leadSourceMechanism": { "type": "keyword" },
				"branchMarketCode": { "type": "keyword" },
				"createdAt": { "type": "date" },
				"tenantId": { "type": "keyword" },
				"customerName": { "type": "text" },
				"customerEmail": { "type": "keyword" }
			}
		}
	}`

	res, err := client.Indices.Create(
		indexName,
		client.Indices.Create.WithBody(strings.NewReader(mapping)),
		client.Indices.Create.WithContext(ctx),
	)
	if err != nil {
		t.Fatalf("Failed to create index: %v", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("Failed to create index: %s - %s", res.Status(), body)
	}
}

// IndexTestData indexes test documents
func IndexTestData(ctx context.Context, t *testing.T, esURI, indexName string, docs []LeadDocument) {
	cfg := elasticsearch.Config{
		Addresses: []string{esURI},
	}
	client, err := elasticsearch.NewClient(cfg)
	if err != nil {
		t.Fatalf("Failed to create ES client: %v", err)
	}

	for _, doc := range docs {
		data, err := json.Marshal(doc)
		if err != nil {
			t.Fatalf("Failed to marshal document: %v", err)
		}

		res, err := client.Index(
			indexName,
			bytes.NewReader(data),
			client.Index.WithDocumentID(doc.ID),
			client.Index.WithContext(ctx),
			client.Index.WithRefresh("true"),
		)
		if err != nil {
			t.Fatalf("Failed to index document: %v", err)
		}
		defer res.Body.Close()

		if res.IsError() {
			body, _ := io.ReadAll(res.Body)
			t.Fatalf("Failed to index document: %s - %s", res.Status(), body)
		}
	}

	// Wait for refresh
	time.Sleep(1 * time.Second)
}

// GenerateTestLeads generates sample lead documents
func GenerateTestLeads() []LeadDocument {
	now := time.Now()
	leads := []LeadDocument{}

	types := []string{"registration", "inquiry", "testdrive"}
	mechanisms := []string{"web", "app", "email"}
	markets := []string{"SE", "NO", "DK"}

	id := 1
	for _, leadType := range types {
		for _, mechanism := range mechanisms {
			for _, market := range markets {
				// Last 24 hours
				leads = append(leads, LeadDocument{
					ID:                  fmt.Sprintf("lead-%d", id),
					LeadType:            leadType,
					LeadSourceMechanism: mechanism,
					BranchMarketCode:    market,
					CreatedAt:           now.Add(-12 * time.Hour),
					TenantID:            "tenant-1",
					CustomerName:        fmt.Sprintf("Customer %d", id),
					CustomerEmail:       fmt.Sprintf("customer%d@example.com", id),
				})
				id++

				// Last 7 days
				leads = append(leads, LeadDocument{
					ID:                  fmt.Sprintf("lead-%d", id),
					LeadType:            leadType,
					LeadSourceMechanism: mechanism,
					BranchMarketCode:    market,
					CreatedAt:           now.Add(-4 * 24 * time.Hour),
					TenantID:            "tenant-1",
				})
				id++

				// Last 30 days
				leads = append(leads, LeadDocument{
					ID:                  fmt.Sprintf("lead-%d", id),
					LeadType:            leadType,
					LeadSourceMechanism: mechanism,
					BranchMarketCode:    market,
					CreatedAt:           now.Add(-20 * 24 * time.Hour),
					TenantID:            "tenant-2",
				})
				id++
			}
		}
	}

	return leads
}

// GraphQLRequest represents a GraphQL request
type GraphQLRequest struct {
	Query         string                 `json:"query"`
	Variables     map[string]interface{} `json:"variables,omitempty"`
	OperationName string                 `json:"operationName,omitempty"`
}

// GraphQLResponse represents a GraphQL response
type GraphQLResponse struct {
	Data   interface{}            `json:"data"`
	Errors []GraphQLError         `json:"errors,omitempty"`
}

// GraphQLError represents a GraphQL error
type GraphQLError struct {
	Message   string                   `json:"message"`
	Locations []map[string]interface{} `json:"locations,omitempty"`
	Path      []interface{}            `json:"path,omitempty"`
}

// ExecuteGraphQLQuery executes a GraphQL query against the schema
func ExecuteGraphQLQuery(t *testing.T, schema graphql.Schema, query string, variables map[string]interface{}) *GraphQLResponse {
	params := graphql.Params{
		Schema:         schema,
		RequestString:  query,
		VariableValues: variables,
	}

	result := graphql.Do(params)

	var errors []GraphQLError
	if result.Errors != nil {
		for _, err := range result.Errors {
			errors = append(errors, GraphQLError{
				Message: err.Message,
			})
		}
	}

	return &GraphQLResponse{
		Data:   result.Data,
		Errors: errors,
	}
}

// ExecuteGraphQLQueryWithContext executes a GraphQL query with HTTP context
func ExecuteGraphQLQueryWithContext(t *testing.T, handler http.Handler, query string, variables map[string]interface{}, headers map[string]string) *GraphQLResponse {
	reqBody := GraphQLRequest{
		Query:     query,
		Variables: variables,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	req := httptest.NewRequest("POST", "/graphql", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	for key, value := range headers {
		req.Header.Set(key, value)
	}

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("Request failed with status %d: %s", w.Code, w.Body.String())
	}

	var response GraphQLResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	return &response
}

// CreateTestBackend creates a reveald backend for testing
func CreateTestBackend(t *testing.T, esURI string) reveald.Backend {
	backend, err := reveald.NewElasticBackend([]string{esURI})
	if err != nil {
		t.Fatalf("Failed to create backend: %v", err)
	}
	return backend
}

// IntrospectSchema performs GraphQL schema introspection
func IntrospectSchema(t *testing.T, schema graphql.Schema, typeName string) map[string]interface{} {
	query := fmt.Sprintf(`{
		__type(name: "%s") {
			name
			kind
			fields {
				name
				type {
					name
					kind
					ofType {
						name
						kind
						ofType {
							name
							kind
						}
					}
				}
			}
		}
	}`, typeName)

	result := ExecuteGraphQLQuery(t, schema, query, nil)

	if result.Errors != nil && len(result.Errors) > 0 {
		t.Fatalf("Schema introspection failed: %v", result.Errors[0].Message)
	}

	data, ok := result.Data.(map[string]interface{})
	if !ok {
		t.Fatal("Invalid introspection response")
	}

	typeData, ok := data["__type"].(map[string]interface{})
	if !ok {
		t.Fatal("Type not found in introspection")
	}

	return typeData
}

// AssertNoErrors checks that there are no GraphQL errors
func AssertNoErrors(t *testing.T, response *GraphQLResponse) {
	if response.Errors != nil && len(response.Errors) > 0 {
		t.Fatalf("GraphQL query returned errors: %v", response.Errors)
	}
}

// GetField extracts a field from nested map structure
func GetField(t *testing.T, data map[string]interface{}, path ...string) interface{} {
	current := data
	for i, key := range path {
		if i == len(path)-1 {
			return current[key]
		}
		next, ok := current[key].(map[string]interface{})
		if !ok {
			t.Fatalf("Failed to navigate to path %v: %s is not a map", path, key)
		}
		current = next
	}
	return nil
}
