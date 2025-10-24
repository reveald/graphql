# SDL Export Fix - _service Query Returns Complete Schema

## Problem

The `_service` query was only returning federation-specific types (_Any, _Service, _Entity) but not the complete schema including document types, result types, aggregation types, input types, and queries.

**Before the fix:**
```json
{
  "_service": {
    "sdl": "extend schema\n  @link(...)\n\nscalar _Any\n\ntype _Service {...}\n\nunion _Entity = ...\n\n"
  }
}
```

Missing: All document types, result types, queries, etc.

## Root Cause

In `schema.go:80-81`, the `_service` resolver was passing an empty schema to `ExportFederationSDL()`:

```go
queryFields["_service"] = &graphql.Field{
    Resolve: func(p graphql.ResolveParams) (any, error) {
        // BUG: Empty schema has no types!
        sdl := ExportFederationSDL(graphql.Schema{}, sg.config, sg.entityKeys)
        return map[string]any{"sdl": sdl}, nil
    },
}
```

The problem is a chicken-and-egg situation: We're defining the `_service` resolver *while* building the schema, so the schema doesn't exist yet.

## Solution

Implemented a **schema reference pattern** that captures the schema after it's fully built:

### 1. Added schemaRef holder (schema.go:12-15)

```go
type schemaRef struct {
    schema *graphql.Schema
}
```

### 2. Initialize schemaRef in SchemaGenerator (schema.go:36)

```go
schemaRef: &schemaRef{},
```

### 3. Capture schemaRef in _service resolver closure (schema.go:82-100)

```go
// Capture references for closures
schemaRef := sg.schemaRef
config := sg.config
entityKeys := sg.entityKeys

queryFields["_service"] = &graphql.Field{
    Resolve: func(p graphql.ResolveParams) (any, error) {
        // Use the captured schemaRef (populated after schema creation)
        if schemaRef.schema == nil {
            return nil, fmt.Errorf("schema not yet initialized")
        }
        sdl := ExportFederationSDL(*schemaRef.schema, config, entityKeys)
        return map[string]any{"sdl": sdl}, nil
    },
}
```

### 4. Populate schemaRef after schema creation (schema.go:182-190)

```go
schema, err := graphql.NewSchema(schemaConfig)
if err != nil {
    return schema, err
}

// Populate the schema reference for _service resolver
sg.schemaRef.schema = &schema

return schema, nil
```

## How It Works

1. **During schema generation**: `schemaRef` is created but empty
2. **_service resolver defined**: Captures `schemaRef` via closure
3. **Schema built**: `graphql.NewSchema()` creates the complete schema
4. **schemaRef populated**: The reference now points to the complete schema
5. **_service queried**: Resolver uses `schemaRef.schema` to export full SDL

## Result

Now the `_service` query returns the **complete SDL** including:

```graphql
extend schema @link(...)

scalar _Any
type _Service {...}
union _Entity = TypeA | TypeB

# All your document types with entity keys
type CrossDomainSearchLeadsDocument @key(fields: "id") {...}
type CrossDomainSearchIterationDocument @key(fields: "id") {...}

# All result types
type LeadsOverviewResult {...}

# All aggregation types
type LeadsOverviewAggregations {...}

# All input types
input ESQueryInput {...}

# Complete Query type
type Query {
  leads: Leads
  _service: _Service!
  _entities(representations: [_Any!]!): [_Entity]!
}

type Leads {
  leadsOverview: LeadsOverviewResult
  leadsOverviewByMarket(markets: [String]): LeadsOverviewResult
  ...
}
```

## Testing

The fix is verified by:
1. `go build` succeeds without errors
2. `TestSchemaReferencePattern` verifies the pattern is correctly initialized
3. Runtime behavior: `_service` query now returns complete SDL

## Apollo Federation Gateway Compatibility

This fix ensures the Apollo Federation Gateway can:
1. Discover the complete subgraph schema via `_service`
2. Compose the supergraph with all types and queries
3. Route queries correctly across subgraphs
4. Resolve entities with proper type information
