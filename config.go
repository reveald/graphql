package graphql

import (
	"net/http"

	"github.com/elastic/go-elasticsearch/v8/typedapi/types"
	"github.com/reveald/reveald"
)

// Config defines the GraphQL API configuration
type Config struct {
	// Queries maps GraphQL query names to their configuration
	Queries map[string]*QueryConfig

	// PrecompiledQueries maps GraphQL query names to precompiled query configurations
	PrecompiledQueries map[string]*PrecompiledQueryConfig

	// EnableFederation enables Apollo Federation v2 support
	// When enabled:
	// - Adds @shareable, @link, and @key directives to schema
	// - Marks common types (Bucket, Pagination, StatsValues, etc.) as @shareable
	// Default: false
	EnableFederation bool

	// QueryNamespace groups all queries under a namespace type
	// The value becomes the GraphQL type name (capitalized if needed)
	// and the field name (lowercased first letter)
	//
	// Examples:
	//   "Leads" → type Leads, query { leads { ... } }
	//   "CrossDomainSearchLeads" → type CrossDomainSearchLeads, query { crossDomainSearchLeads { ... } }
	//
	// If empty, queries are at root level (default)
	QueryNamespace string

	// ExtendQueryNamespace controls whether the namespace type is extended or defined
	// When true: generates "extend type Leads { ... }" (type defined in another subgraph)
	// When false: generates "type Leads { ... }" (this subgraph owns the type)
	// Only relevant when QueryNamespace is set and EnableFederation is true
	// Default: false
	ExtendQueryNamespace bool

	// Mapping is the mapping to use for the GraphQL API
	Mapping IndexMapping
}

// RootQueryBuilder is a function that builds a root query based on the HTTP request
// This allows dynamic filtering based on request context (headers, JWT, etc.)
// Used for typed Elasticsearch queries (EnableElasticQuerying: true)
type RootQueryBuilder func(r *http.Request) (*types.Query, error)

// RequestInterceptor is a function that modifies a reveald Request based on the HTTP request
// This allows injecting parameters based on request context (headers, JWT, etc.)
// Used for feature-based reveald queries
type RequestInterceptor func(httpReq *http.Request, revealdReq *reveald.Request) error

// QueryConfig defines configuration for a single GraphQL query
type QueryConfig struct {
	// Features are the reveald features to apply to this query
	Features []reveald.Feature

	// Description is an optional description for the GraphQL schema
	Description string

	// EnableAggregations determines if aggregations should be included in results
	EnableAggregations bool

	// EnablePagination determines if pagination fields should be included
	EnablePagination bool

	// EnableSorting determines if sorting fields should be included
	EnableSorting bool

	// FieldFilter allows specifying which fields to include/exclude from the schema
	FieldFilter *FieldFilter

	// AggregationFields specifies which fields should have aggregations in the schema
	// If nil or empty, no aggregation fields will be generated
	AggregationFields []string

	// EnableElasticQuerying allows passing raw Elasticsearch queries/aggregations
	// When enabled, adds 'query' and 'aggs' arguments to the GraphQL query
	EnableElasticQuerying bool

	// RootQuery is a base Elasticsearch query that is always applied (merged with user queries)
	// Useful for static filtering (e.g., always filter by active=true)
	RootQuery *types.Query

	// RootQueryBuilder dynamically builds a root query based on the HTTP request
	// If both RootQuery and RootQueryBuilder are set, both are merged with user queries
	// Only used for typed ES queries (EnableElasticQuerying: true)
	RootQueryBuilder RootQueryBuilder

	// RequestInterceptor modifies the reveald Request based on the HTTP request
	// Used for feature-based queries to inject dynamic parameters
	RequestInterceptor RequestInterceptor
}

// FieldFilter defines which fields to include or exclude
type FieldFilter struct {
	// Include lists fields to include (if empty, all fields are included)
	Include []string

	// Exclude lists fields to exclude
	Exclude []string
}

// ConfigOption is a functional option for configuring the GraphQL API
type ConfigOption func(*Config)

// WithEnableFederation enables Apollo Federation v2 support
func WithEnableFederation() ConfigOption {
	return func(c *Config) {
		c.EnableFederation = true
	}
}

// WithQueryNamespace sets the query namespace and optionally extends it
// Examples:
//
//	WithQueryNamespace("Leads", false) → type Leads { ... }, query { leads { ... } }
//	WithQueryNamespace("Leads", true) → extend type Leads { ... }, query { leads { ... } }
func WithQueryNamespace(namespace string, extend bool) ConfigOption {
	return func(c *Config) {
		c.QueryNamespace = namespace
		c.ExtendQueryNamespace = extend
	}
}

// WithQuery adds a reveald feature-based query
func WithQuery(name string, queryConfig *QueryConfig) ConfigOption {
	return func(c *Config) {
		if c.Queries == nil {
			c.Queries = make(map[string]*QueryConfig)
		}
		c.Queries[name] = queryConfig
	}
}

// WithPrecompiledQuery adds a precompiled query
func WithPrecompiledQuery(name string, queryConfig *PrecompiledQueryConfig) ConfigOption {
	return func(c *Config) {
		if c.PrecompiledQueries == nil {
			c.PrecompiledQueries = make(map[string]*PrecompiledQueryConfig)
		}
		c.PrecompiledQueries[name] = queryConfig
	}
}

// NewConfig creates a new GraphQL API configuration with optional functional options
func NewConfig(mapping IndexMapping, opts ...ConfigOption) *Config {
	config := &Config{
		Queries:            make(map[string]*QueryConfig),
		PrecompiledQueries: make(map[string]*PrecompiledQueryConfig),
		Mapping:            mapping,
	}

	// Apply options
	for _, opt := range opts {
		opt(config)
	}

	return config
}

// AddQuery adds a query configuration to the config
func (c *Config) AddQuery(name string, config *QueryConfig) {
	if c.Queries == nil {
		c.Queries = make(map[string]*QueryConfig)
	}
	c.Queries[name] = config
}

// AddPrecompiledQuery adds a precompiled query configuration to the config
func (c *Config) AddPrecompiledQuery(name string, config *PrecompiledQueryConfig) {
	if c.PrecompiledQueries == nil {
		c.PrecompiledQueries = make(map[string]*PrecompiledQueryConfig)
	}
	c.PrecompiledQueries[name] = config
}
