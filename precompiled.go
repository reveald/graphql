package graphql

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/elastic/go-elasticsearch/v8/typedapi/core/search"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types"
	"github.com/graphql-go/graphql"
)

// QueryBuilderFunc is a function that builds an Elasticsearch search request
// based on GraphQL arguments
type QueryBuilderFunc func(args map[string]any) *search.Request

// PrecompiledQueryConfig defines a precompiled Elasticsearch query with auto-generated schema
type PrecompiledQueryConfig struct {
	// Index is the Elasticsearch index to query
	Index string

	// Indices are multiple Elasticsearch indices to query
	Indices []string

	// QueryBuilder builds the Elasticsearch search request from GraphQL arguments
	// Either QueryBuilder, QueryFile, or QueryJSON must be specified
	QueryBuilder QueryBuilderFunc

	// QueryFile is a path to a JSON file containing the Elasticsearch query
	// The file should contain a valid search.Request JSON
	QueryFile string

	// QueryJSON is a JSON string containing the Elasticsearch query
	// Useful with Go embed: QueryJSON: string(embeddedQuery)
	// Priority: QueryJSON > QueryFile > QueryBuilder
	QueryJSON string

	// Parameters defines the GraphQL input arguments for this query
	Parameters graphql.FieldConfigArgument

	// Description is an optional description for the GraphQL schema
	Description string

	// SampleParameters are sample values used to generate the schema
	// The QueryBuilder will be called with these parameters to introspect
	// the aggregation structure
	SampleParameters map[string]any

	// RootQuery is a base Elasticsearch query that is always applied (merged with user queries)
	// Useful for static filtering (e.g., always filter by active=true)
	RootQuery *types.Query

	// RootQueryBuilder dynamically builds a root query based on the HTTP request
	// If both RootQuery and RootQueryBuilder are set, both are merged with the final query
	// Useful for tenant filtering, permissions, etc.
	RootQueryBuilder RootQueryBuilder
}

// GetIndices returns all indices configured for this query
func (pc *PrecompiledQueryConfig) GetIndices() []string {
	if len(pc.Indices) > 0 {
		return pc.Indices
	}
	if pc.Index != "" {
		return []string{pc.Index}
	}
	return []string{}
}

// Validate checks if the configuration is valid
func (pc *PrecompiledQueryConfig) Validate() error {
	if pc.QueryBuilder == nil && pc.QueryFile == "" && pc.QueryJSON == "" {
		return fmt.Errorf("either QueryBuilder, QueryFile, or QueryJSON must be specified")
	}

	if len(pc.GetIndices()) == 0 {
		return fmt.Errorf("at least one index must be specified")
	}

	return nil
}

// LoadQuery loads the query, either from JSON string, file, or builder
// If httpReq is provided, RootQueryBuilder will be called to inject dynamic base queries
func (pc *PrecompiledQueryConfig) LoadQuery(args map[string]any, httpReq *http.Request) (*search.Request, error) {
	var req *search.Request
	var data []byte
	var err error

	// Priority: QueryJSON > QueryFile > QueryBuilder
	if pc.QueryJSON != "" {
		// Load from JSON string (e.g., from embed)
		data = []byte(pc.QueryJSON)
	} else if pc.QueryFile != "" {
		// Load from file
		data, err = os.ReadFile(pc.QueryFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read query file %s: %w", pc.QueryFile, err)
		}
	}

	// Unmarshal JSON if we have data
	if len(data) > 0 {
		req = &search.Request{}
		if err := json.Unmarshal(data, req); err != nil {
			return nil, fmt.Errorf("failed to unmarshal query JSON: %w", err)
		}
	}

	// Apply QueryBuilder if specified
	if pc.QueryBuilder != nil {
		builderReq := pc.QueryBuilder(args)
		if builderReq == nil {
			return nil, fmt.Errorf("QueryBuilder returned nil")
		}

		// If we loaded from JSON, QueryBuilder can modify it
		// For now, QueryBuilder result takes precedence (replaces JSON-based query)
		// TODO: Could add merging logic here if needed
		req = builderReq
	}

	if req == nil {
		return nil, fmt.Errorf("no query produced")
	}

	// Apply root queries (merge static and dynamic root queries with the main query)
	var dynamicRootQuery *types.Query
	if pc.RootQueryBuilder != nil && httpReq != nil {
		var err error
		dynamicRootQuery, err = pc.RootQueryBuilder(httpReq)
		if err != nil {
			return nil, fmt.Errorf("failed to build root query: %w", err)
		}
	}

	// Merge root queries with the request query
	if pc.RootQuery != nil || dynamicRootQuery != nil {
		req.Query = mergeQueries(pc.RootQuery, dynamicRootQuery, req.Query)
	}

	return req, nil
}
