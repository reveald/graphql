# Apollo Federation Support

This implementation adds full Apollo Federation v2 support to reveald-graphql, including entity resolution with proper security context (RootQueryBuilder and RequestInterceptor).

## Features

### 1. Federation Types and Scalars
- `_Any` scalar - Represents any JSON value (for entity representations)
- `_Service` type - Contains SDL field for schema introspection
- `_Entity` union - Union of all entity types (types with @key directives)

### 2. Federation Queries
- `_service: _Service!` - Returns the SDL for this subgraph
- `_entities(representations: [_Any!]!): [_Entity]!` - Resolves entities by their key fields

### 3. Entity Resolution with Security
**Critical Feature**: Entity resolution applies RootQueryBuilder and RequestInterceptor, ensuring:
- Tenant filtering is enforced on federated queries
- Permission checks are applied
- Dynamic filtering based on HTTP headers works correctly

## Usage Example

### Basic Setup with Federation

```go
// Create config with federation enabled
config := revealdgraphql.NewConfig(
    revealdgraphql.WithEnableFederation(),
    revealdgraphql.WithQueryNamespace("Leads", false),
)

// Add a query with entity keys
config.AddPrecompiledQuery("leadsOverview", &revealdgraphql.PrecompiledQueryConfig{
    Mapping:         mapping,
    Index:           "leads",
    Description:     "Leads overview",
    QueryBuilder:    buildLeadsQuery,
    EntityKeyFields: []string{"id", "conversationId"}, // Multiple keys = multiple @key directives

    // IMPORTANT: RootQueryBuilder is applied to _entities queries
    RootQueryBuilder: func(r *http.Request) (*types.Query, error) {
        // Example: Filter by tenant ID from header
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

// Create GraphQL API
api, err := revealdgraphql.New(backend, config, revealdgraphql.WithESClient(esClient))
```

### Generated Schema (SDL)

The implementation automatically generates complete Apollo Federation v2 schema. The `_service` query returns the full SDL including:
- Federation directives and types
- All your document types with entity keys
- All result types and aggregation types
- All input types (for queries, filters, etc.)
- The root Query type with all queries and federation queries

Example SDL output:

```graphql
extend schema
  @link(url: "https://specs.apollo.dev/federation/v2.3",
        import: ["@key", "@shareable"])

scalar _Any

type _Service {
  sdl: String!
}

union _Entity = CrossDomainSearchLeadsDocument | CrossDomainSearchIterationDocument

# Common shareable types
type Bucket @shareable {
  value: String
  count: Int
  filterValue: String
  buckets: [Bucket]
}

type Pagination @shareable {
  offset: Int
  limit: Int
  totalCount: Int
}

# Entity types with @key directives
type CrossDomainSearchLeadsDocument @key(fields: "id") @key(fields: "conversationId") {
  id: String
  conversationId: String
  leadType: String
  status: String
  createdAt: String
  # ... all other fields from your mapping
}

type CrossDomainSearchIterationDocument @key(fields: "id") {
  id: String
  iterationId: String
  timestamp: String
  # ... all other fields
}

# Result types
type LeadsOverviewResult {
  totalCount: Int
  hits: [CrossDomainSearchLeadsDocument]
  aggregations: LeadsOverviewAggregations
}

type LeadsOverviewAggregations {
  by_leadType: LeadsOverviewBy_leadType
  by_mechanism: LeadsOverviewBy_mechanism
  # ... all aggregation types
}

# Input types
input ESQueryInput {
  # ... ES query fields
}

# Root Query type
type Query {
  # Your namespaced queries
  leads: Leads

  # Federation queries (always at root level)
  _service: _Service!
  _entities(representations: [_Any!]!): [_Entity]!
}

type Leads {
  leadsOverview: LeadsOverviewResult
  leadsOverviewByMarket(markets: [String]): LeadsOverviewResult
  # ... all your queries
}
```

**Important**: The `_service` query dynamically generates the SDL from the actual schema at runtime, ensuring it always reflects the current schema structure.

### Federation Queries

#### Query the SDL
```graphql
query {
  _service {
    sdl
  }
}
```

#### Resolve Entities
```graphql
query {
  _entities(representations: [
    {__typename: "LeadsDocument", id: "123"}
  ]) {
    ... on LeadsDocument {
      id
      leadType
      status
    }
  }
}
```

### Security Enforcement

When the Apollo Gateway sends entity resolution requests, the implementation:

1. **Parses the entity representation** - Extracts `__typename` and key fields (e.g., `id: "123"`)

2. **Builds the base query** - Creates an Elasticsearch query matching the key fields:
   ```json
   {"term": {"id.keyword": "123"}}
   ```

3. **Applies RootQueryBuilder** - Merges with tenant/permission filters:
   ```json
   {
     "bool": {
       "must": [
         {"term": {"id.keyword": "123"}},
         {"term": {"tenantId.keyword": "tenant-xyz"}}
       ]
     }
   }
   ```

4. **Executes the query** - Returns the entity only if it matches all filters

This ensures that users can only access entities they're authorized to see, even through federation.

## Implementation Details

### Files Modified/Created

1. **federation.go** - Added `_Any` scalar, `_Service` type, `CreateEntityUnion()`, and helper functions

2. **federation_resolver.go** (new) - Entity resolution logic:
   - `EntityResolver` - Manages entity type mappings and resolves entities
   - `EntityTypeMapping` - Tracks query config, mapping, and keys for each entity type
   - `ResolveEntities()` - Main resolver for `_entities` query
   - Applies both RootQueryBuilder and RequestInterceptor

3. **schema.go** - Updated to:
   - Initialize `EntityResolver` when federation is enabled
   - Add `_service` and `_entities` queries to schema
   - Register entity types during schema generation
   - Keep federation queries at root level (not under namespace)
   - **Schema reference pattern**: Uses `schemaRef` to capture the schema after creation, allowing the `_service` resolver to access the complete schema for SDL generation

4. **schema_precompiled.go** - Register precompiled entity types with resolver

5. **federation_sdl.go** - Export federation types and unions in SDL

### _service Query Implementation

The `_service` query uses a clever pattern to return the complete SDL:

1. During schema generation, a `schemaRef` holder is created
2. The `_service` resolver captures this reference via closure
3. After the schema is fully built, the reference is populated
4. When `_service` is queried, it exports SDL from the actual schema

This ensures the SDL always includes:
- All types (Document, Result, Aggregation, Input types)
- All queries (both your queries and federation queries)
- All entity keys and directives
- Complete type definitions with all fields

### Entity Type Registration

During schema generation, each document type with `EntityKeyFields` is automatically registered:

```go
// For regular queries (with reveald features)
sg.entityResolver.RegisterEntityType(typeName, &EntityTypeMapping{
    QueryName:       queryName,
    QueryConfig:     queryConfig,
    RevealdEndpoint: endpoint,
    ArgumentReader:  reader,
    UseFeatureFlow:  true,
    Mapping:         mapping,
    EntityKeys:      queryConfig.EntityKeyFields,
})

// For precompiled queries
sg.entityResolver.RegisterEntityType(docTypeName, &EntityTypeMapping{
    QueryName:         queryName,
    PrecompiledConfig: queryConfig,
    UseFeatureFlow:    false,
    Mapping:           &queryConfig.Mapping,
    EntityKeys:        queryConfig.EntityKeyFields,
})
```

## Testing

Basic unit tests are provided in `federation_test.go`:
- `TestFederationTypes` - Verifies federation types initialize correctly
- `TestCreateEntityUnion` - Tests entity union creation
- `TestParseEntityRepresentation` - Tests entity representation parsing
- `TestEntityResolverRegistration` - Tests entity resolver registration

## Apollo Federation Gateway Integration

To use this subgraph with Apollo Federation Gateway:

```javascript
const gateway = new ApolloGateway({
  supergraphSdl: /* your supergraph SDL */,
  // Or use managed federation
});

// The gateway will automatically:
// 1. Call _service to get the SDL
// 2. Call _entities to resolve cross-graph references
// 3. Merge results from multiple subgraphs
```

## Notes

- Federation queries (`_service`, `_entities`) are always at root level, even when using `QueryNamespace`
- Entity resolution works for both regular queries (with reveald features) and precompiled queries
- Multiple `@key` directives are supported by specifying multiple entity key fields
- RootQueryBuilder and RequestInterceptor are ALWAYS applied to entity resolution for security
