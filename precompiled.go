package graphql

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/elastic/go-elasticsearch/v8/typedapi/core/search"
	"github.com/graphql-go/graphql"
)

// QueryBuilderFunc is a function that builds an Elasticsearch search request
// based on GraphQL arguments
type QueryBuilderFunc func(args map[string]any) *search.Request

// PrecompiledQueryConfig defines a precompiled Elasticsearch query with automatically
// generated strongly-typed schema.
//
// Precompiled queries are ideal for:
// - Complex aggregations with nested structure
// - Queries that need strongly-typed GraphQL schemas
// - Performance-critical queries (pre-validated at startup)
// - Queries with predictable structure
//
// The system automatically generates GraphQL types by introspecting the ES query structure,
// creating strongly-typed aggregation schemas instead of generic arrays.
//
// Example:
//
//	config.AddPrecompiledQuery("leadsOverview", &PrecompiledQueryConfig{
//	    Index:        "leads",
//	    QueryBuilder: buildLeadsQuery,  // OR QueryJSON: string(embeddedQuery)
//	    Parameters: graphql.FieldConfigArgument{
//	        "markets": &graphql.ArgumentConfig{Type: graphql.NewList(graphql.String)},
//	    },
//	    RootQueryBuilder: func(r *http.Request) (*types.Query, error) {
//	        // Optional: Add tenant filtering from headers
//	        return buildTenantFilter(r.Header.Get("X-Tenant-ID"))
//	    },
//	})
//
// This generates a GraphQL schema with typed aggregations:
//
//	type LeadsOverviewResult {
//	    totalCount: Int
//	    aggregations: LeadsOverviewAggregations
//	}
//
//	type LeadsOverviewAggregations {
//	    by_leadType: LeadsOverviewBy_leadType
//	}
//
// Instead of generic: aggregations: [GenericAggregation]
type PrecompiledQueryConfig struct {
	// Index is the Elasticsearch index to query
	Index string

	// Indices are multiple Elasticsearch indices to query
	Indices []string

	// Mapping is the Elasticsearch index mapping for this query
	Mapping IndexMapping

	// QueryBuilder builds the Elasticsearch search request from GraphQL arguments
	// Mutually exclusive with QueryJSON - specify only one
	QueryBuilder QueryBuilderFunc

	// QueryJSON is a JSON string containing the Elasticsearch query
	// Useful with Go embed: QueryJSON: string(embeddedQuery)
	// Mutually exclusive with QueryBuilder - specify only one
	QueryJSON string

	// Parameters defines the GraphQL input arguments for this query
	Parameters graphql.FieldConfigArgument

	// Description is an optional description for the GraphQL schema
	Description string

	// SampleParameters are sample values used to generate the schema
	// The QueryBuilder will be called with these parameters to introspect
	// the aggregation structure
	SampleParameters map[string]any

	// RootQueryBuilder dynamically builds a root query based on the HTTP request
	// The root query is merged with the main query
	// Useful for tenant filtering, permissions, etc.
	RootQueryBuilder RootQueryBuilder

	// EntityKeyFields specifies the fields to use as entity keys for Apollo Federation
	// Overrides the IndexMapping.EntityKeyFields for this specific query
	// Each element represents one @key directive with space-separated field names
	// Examples:
	//   []string{"id"} → @key(fields: "id")
	//   []string{"leadId", "conversationId"} → @key(fields: "leadId") @key(fields: "conversationId")
	EntityKeyFields []string
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
	// Exactly one of QueryBuilder or QueryJSON must be specified
	hasBuilder := pc.QueryBuilder != nil
	hasJSON := pc.QueryJSON != ""

	if !hasBuilder && !hasJSON {
		return fmt.Errorf("either QueryBuilder or QueryJSON must be specified")
	}
	if hasBuilder && hasJSON {
		return fmt.Errorf("QueryBuilder and QueryJSON are mutually exclusive - specify only one")
	}

	if len(pc.GetIndices()) == 0 {
		return fmt.Errorf("at least one index must be specified")
	}

	return nil
}

// LoadQuery loads the query from JSON string or builder
// If httpReq is provided, RootQueryBuilder will be called to inject dynamic base queries
func (pc *PrecompiledQueryConfig) LoadQuery(args map[string]any, httpReq *http.Request) (*search.Request, error) {
	var req *search.Request

	// Priority: QueryBuilder > QueryJSON
	if pc.QueryBuilder != nil {
		req = pc.QueryBuilder(args)
		if req == nil {
			return nil, fmt.Errorf("QueryBuilder returned nil")
		}
	} else if pc.QueryJSON != "" {
		// Load from JSON string (e.g., from embed)
		req = &search.Request{}
		if err := json.Unmarshal([]byte(pc.QueryJSON), req); err != nil {
			return nil, fmt.Errorf("failed to unmarshal query JSON: %w", err)
		}
	}

	if req == nil {
		return nil, fmt.Errorf("no query produced")
	}

	// Apply root query builder if provided
	if pc.RootQueryBuilder != nil && httpReq != nil {
		dynamicRootQuery, err := pc.RootQueryBuilder(httpReq)
		if err != nil {
			return nil, fmt.Errorf("failed to build root query: %w", err)
		}
		if dynamicRootQuery != nil {
			req.Query = mergeQueries(dynamicRootQuery, req.Query)
		}
	}

	return req, nil
}
