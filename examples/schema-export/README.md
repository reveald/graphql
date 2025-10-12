# GraphQL Schema Export Tool

This tool generates the GraphQL schema SDL (Schema Definition Language) without requiring Elasticsearch connection.

**No Elasticsearch needed!** Just define your queries and export the schema.

## Usage

### Print to stdout
```bash
cd examples/schema-export
go run main.go
```

### Save to file
```bash
go run main.go > schema.graphql
```

### Compile and use
```bash
go build -o schema-export
./schema-export > schema.graphql
```

### In CI/CD
```bash
# Validate schema hasn't changed
go run main.go > schema.graphql
git diff --exit-code schema.graphql

# Or commit updated schema
go run main.go > schema.graphql
git add schema.graphql
git commit -m "Update GraphQL schema"
```

## What It Generates

With `config.EnableFederation = true`, the generated schema includes:

### 1. Federation Schema Extension
```graphql
extend schema
  @link(url: "https://specs.apollo.dev/federation/v2.3",
        import: ["@key", "@shareable"])
```

### 2. Strongly-Typed Aggregations
```graphql
type LeadsOverviewAggregations {
  by_leadType: LeadsOverviewBy_leadType
}
```

### 3. @shareable Directives on Common Types
```graphql
type LeadsOverviewBy_leadTypeBucket @shareable {
  key: String!
  doc_count: Int!
  by_mechanism: LeadsOverviewBy_leadTypeBy_mechanism
}
```

### 4. Index-Based Document Types
```graphql
type TestLeadsDocument {
  id: String
}
```

All bucket types are automatically marked as `@shareable` so they can be shared across multiple subgraphs in an Apollo Federation supergraph.

## Use Cases

- **Apollo Federation**: Generate schema for subgraph composition
- **CI/CD**: Validate schema changes in pipelines
- **Documentation**: Auto-generate GraphQL docs
- **Schema Versioning**: Track schema evolution over time

## Query Namespace

The example uses `config.QueryNamespace = "leads"` to group all queries:

**Without namespace** (default):
```graphql
query {
  leadsOverview { totalCount }
  leadsOverviewByMarket { totalCount }
}
```

**With namespace** (`config.QueryNamespace = "leads"`):
```graphql
query {
  leads {
    leadsOverview { totalCount }
    leadsOverviewByMarket { totalCount }
  }
}
```

The generated SDL shows:
```graphql
type Query {
  leads: LeadsEntity
}

type LeadsEntity {
  leadsOverview: LeadsOverviewResult
  leadsOverviewByMarket: LeadsOverviewByMarketResult
}
```

## Customization

Edit `main.go` to:
- Add your query configurations
- Change federation settings (`config.EnableFederation`)
- Set query namespace (`config.QueryNamespace`)
- Customize which types are shareable (see `federation.go`)
