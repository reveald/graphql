package graphql

import (
	"net/http"

	"github.com/elastic/go-elasticsearch/v8/typedapi/types"
	"github.com/graphql-go/graphql"
	"github.com/reveald/reveald/v2"
)

// FieldExtension defines a custom field to add to a generated GraphQL type
type FieldExtension struct {
	// FieldName is the name of the field to add (e.g., "reviews")
	FieldName string

	// Field is the complete GraphQL field definition including type and resolver
	// The resolver receives full graphql.ResolveParams with access to parent document via p.Source
	Field *graphql.Field

	// Directives are federation directives to apply to this field
	// Map keys are directive names (e.g., "requires", "external")
	// Map values are directive arguments (e.g., "item { displayName }")
	// Example: map[string]string{"requires": "item { displayName }"}
	Directives map[string]string
}

// TypeExtension defines custom fields to add to a specific generated type
type TypeExtension struct {
	// TypeName is the name of the generated type to extend (e.g., "ProductDocument")
	TypeName string

	// Fields are the custom fields to add to this type
	Fields []FieldExtension
}

// CustomTypeWithKeys defines a custom GraphQL type with optional entity keys for Federation
type CustomTypeWithKeys struct {
	// Type is the GraphQL object type
	Type *graphql.Object

	// EntityKeys specifies the fields to use as entity keys for Apollo Federation
	// Each element represents one @key directive with space-separated field names
	// Examples:
	//   []string{"id"} → @key(fields: "id")
	//   []string{"id", "productId"} → @key(fields: "id") @key(fields: "productId")
	EntityKeys []string

	// Resolvable indicates whether this subgraph can resolve this entity via _entities query
	// When true: Type is added to _Entity union (this subgraph owns/resolves the entity)
	// When false: Only @key appears in SDL (this subgraph references but doesn't resolve the entity)
	// Default: false (reference only)
	Resolvable bool

	// ExternalFields specifies which fields should be marked with @external directive
	// Used in Federation to indicate fields owned by another subgraph
	// Examples:
	//   []string{"displayName"} → displayName: String! @external
	//   []string{"name", "email"} → name: String @external, email: String @external
	ExternalFields []string
}

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

	// TypeExtensions defines custom fields to add to generated types
	// The custom fields are shared across all queries that return the same type
	// Example: Add "reviews" field to ProductDocument type
	TypeExtensions []TypeExtension

	// CustomTypes defines additional GraphQL object types to include in the schema
	// Useful for types referenced by field extensions (e.g., Review type for reviews field)
	CustomTypes []*graphql.Object

	// CustomTypesWithKeys defines custom types with entity keys for Federation
	CustomTypesWithKeys []CustomTypeWithKeys
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
	// Mapping is the Elasticsearch index mapping for this query
	Mapping IndexMapping

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

	// FieldTypeOverrides allows overriding the GraphQL type for specific fields
	// Example: map[string]graphql.Output{"id": graphql.NewNonNull(graphql.ID)}
	// This is useful for:
	// - Making fields non-nullable (NewNonNull)
	// - Using GraphQL ID type for identifiers
	// - Custom scalar types
	FieldTypeOverrides map[string]graphql.Output

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

	// EntityKeyFields specifies the fields to use as entity keys for Apollo Federation
	// Overrides the IndexMapping.EntityKeyFields for this specific query
	// Each element represents one @key directive with space-separated field names
	// Examples:
	//   []string{"id"} → @key(fields: "id")
	//   []string{"id", "email"} → @key(fields: "id") @key(fields: "email")
	EntityKeyFields []string

	// HitsTypeName is an optional custom name for the document type returned in the hits field
	// If not provided, defaults to "{IndexName}Document" (e.g., "ProductsDocument")
	// Example: "Lead" instead of "TestLeadsDocument"
	HitsTypeName string
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

// WithTypeExtension adds custom fields to a generated GraphQL type
// Example: Add "reviews" field to ProductDocument type
func WithTypeExtension(typeName string, fields []FieldExtension) ConfigOption {
	return func(c *Config) {
		c.TypeExtensions = append(c.TypeExtensions, TypeExtension{
			TypeName: typeName,
			Fields:   fields,
		})
	}
}

// WithCustomTypes adds custom GraphQL object types to the schema
// These types can be referenced by field extensions
func WithCustomTypes(types ...*graphql.Object) ConfigOption {
	return func(c *Config) {
		c.CustomTypes = append(c.CustomTypes, types...)
	}
}

// WithCustomTypesWithKeys adds custom GraphQL types with entity keys for Federation
// This allows custom types to be marked with @key directives in the SDL
// Example:
//
//	WithCustomTypesWithKeys(CustomTypeWithKeys{
//	    Type: reviewType,
//	    EntityKeys: []string{"id"},
//	})
func WithCustomTypesWithKeys(types ...CustomTypeWithKeys) ConfigOption {
	return func(c *Config) {
		c.CustomTypesWithKeys = append(c.CustomTypesWithKeys, types...)
	}
}

// NewConfig creates a new GraphQL API configuration with optional functional options
func NewConfig(opts ...ConfigOption) *Config {
	config := &Config{
		Queries:            make(map[string]*QueryConfig),
		PrecompiledQueries: make(map[string]*PrecompiledQueryConfig),
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

// AddTypeExtension adds custom fields to a generated GraphQL type
func (c *Config) AddTypeExtension(typeName string, fields []FieldExtension) {
	c.TypeExtensions = append(c.TypeExtensions, TypeExtension{
		TypeName: typeName,
		Fields:   fields,
	})
}

// AddCustomTypes adds custom GraphQL object types to the schema
func (c *Config) AddCustomTypes(types ...*graphql.Object) {
	c.CustomTypes = append(c.CustomTypes, types...)
}

// AddCustomTypesWithKeys adds custom types with entity keys for Federation
func (c *Config) AddCustomTypesWithKeys(types ...CustomTypeWithKeys) {
	c.CustomTypesWithKeys = append(c.CustomTypesWithKeys, types...)
}
