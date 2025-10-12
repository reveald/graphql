# reveald-graphql

A GraphQL API generator for Elasticsearch based on [reveald](https://github.com/reveald/reveald). Automatically generates GraphQL schemas from Elasticsearch index mappings and exposes search functionality through GraphQL queries with **zero manual configuration**.

## Features

- **Automatic Schema Generation**: Generate complete GraphQL schemas from Elasticsearch index mappings
- **Flexible Elasticsearch Querying**: Full ES query/aggregation support with typed GraphQL inputs
- **Auto-Detected Aggregations**: Aggregations automatically detected from configured Features - no manual lists needed
- **Auto-Generated Sort Enums**: Sort options extracted from SortingFeature and exposed as type-safe GraphQL enums
- **Type Safety**: Automatic conversion of Elasticsearch types to GraphQL types
- **Nested Field Support**: Seamless handling of nested objects (e.g., `vehicle.manufacturer` → `vehicle_manufacturer`)
- **Hierarchical Aggregations**: Full support for nested aggregations with `filterValue` for easy filtering
- **Full reveald Feature Support**: Leverage all reveald features (pagination, filtering, sorting, aggregations, histograms, date histograms, etc.)
- **Custom Features**: Extensible with custom feature implementations
- **GraphiQL Interface**: Integrated GraphiQL UI for interactive query development
- **Flexible Configuration**: Define multiple queries with different feature sets
- **Static Root Queries**: Apply base filters (like tenant isolation) that are always merged with user queries

## Installation

```bash
go get github.com/reveald/graphql
```

## Quick Start

### 1. Define Your Elasticsearch Mapping

```json
{
  "mappings": {
    "properties": {
      "id": { "type": "keyword" },
      "name": {
        "type": "text",
        "fields": {
          "keyword": { "type": "keyword" }
        }
      },
      "category": {
        "type": "text",
        "fields": {
          "keyword": { "type": "keyword" }
        }
      },
      "price": { "type": "double" },
      "active": { "type": "boolean" }
    }
  }
}
```

### 2. Create Your GraphQL API

```go
package main

import (
    "log"
    "net/http"
    "os"

    revealdgraphql "github.com/reveald/graphql"
    "github.com/reveald/reveald"
    "github.com/reveald/reveald/featureset"
)

func main() {
    backend, _ := reveald.NewElasticBackend([]string{"http://localhost:9200"})

    mappingData, _ := os.ReadFile("mapping.json")
    mapping, _ := revealdgraphql.ParseMapping("products", mappingData)

    // Configure with functional options
    config := revealdgraphql.NewConfig(
        revealdgraphql.WithQuery("searchProducts", &revealdgraphql.QueryConfig{
            Features: []reveald.Feature{
                featureset.NewPaginationFeature(),
                featureset.NewSortingFeature("sort",
                    featureset.WithSortOption("price-asc", "price", true),
                    featureset.WithSortOption("price-desc", "price", false),
                    featureset.WithDefaultSortOption("price-desc"),
                ),
                featureset.NewDynamicFilterFeature("category"),
                featureset.NewDynamicFilterFeature("brand"),
            },
            EnableAggregations: true,
            EnablePagination:   true,
            EnableSorting:      true,
            // Aggregations and sort options are AUTO-DETECTED from Features!
        }),
    )

    api, _ := revealdgraphql.New(backend, mapping, config)

    http.ListenAndServe(":8080", api)
}
```

### 3. Query Your Data

Open `http://localhost:8080/graphql`:

```graphql
query {
  searchProducts(
    category: ["electronics"]
    sort: price_desc  # Auto-complete from enum!
    limit: 10
  ) {
    hits {
      id
      name
      price
      category
    }
    totalCount
    aggregations {
      category { value count }
      brand { value count }
    }
    pagination {
      offset
      limit
      totalCount
    }
  }
}
```

## Auto-Detection Features

### Aggregations

**No manual configuration needed!** Aggregations are automatically detected from your Features:

```go
Features: []reveald.Feature{
    featureset.NewDynamicFilterFeature("category"),      // ← Auto-detected
    featureset.NewDynamicFilterFeature("brand"),         // ← Auto-detected
    featureset.NewDynamicBooleanFilterFeature("active"), // ← Auto-detected
}
```

The GraphQL schema will automatically include:
```graphql
type Aggregations {
  category: [Bucket!]
  brand: [Bucket!]
  active: [Bucket!]
}
```

### Sort Enums

Sort options are automatically extracted from `SortingFeature` and exposed as GraphQL enums:

```go
featureset.NewSortingFeature("sort",
    featureset.WithSortOption("price-asc", "price", true),
    featureset.WithSortOption("price-desc", "price", false),
    featureset.WithSortOption("updated-desc", "updatedAt", false),
)
```

Generates:
```graphql
enum SearchProductsSortOption {
  price_asc
  price_desc
  updated_desc
}

type Query {
  searchProducts(sort: SearchProductsSortOption): SearchProductsResult
}
```

You get **full autocomplete** in GraphiQL!

### Nested Fields

Nested object fields are automatically converted:
- ES field: `vehicle.manufacturer` → GraphQL: `vehicle_manufacturer`
- ES field: `ad.price` → GraphQL: `ad_price`

The API automatically converts between the two formats.

## Hierarchical Aggregations

For complex nested aggregations (like task hierarchies), buckets include a `filterValue` field:

```graphql
query {
  searchVehicles {
    aggregations {
      processes_tasks_process {
        value          # "Iordningställande"
        count          # 191
        filterValue    # "Iordningställande" (ready to use for filtering)
        buckets {
          value        # "Förkalkyl"
          count        # 180
          filterValue  # "Iordningställande>Förkalkyl" (full hierarchical path)
          buckets {
            value        # "urn:pf:task:state:completed"
            filterValue  # "Iordningställande>Förkalkyl>urn:pf:task:state:completed"
          }
        }
      }
    }
  }
}
```

Use the `filterValue` directly for filtering:

```graphql
query {
  searchVehicles(
    processes_tasks_process: ["Iordningställande>Förkalkyl"]
  ) {
    totalCount
  }
}
```

## Type Mappings

| Elasticsearch Type | GraphQL Type |
|-------------------|--------------|
| text, keyword     | String       |
| long, integer, short, byte | Int |
| double, float     | Float        |
| boolean           | Boolean      |
| date              | String (ISO8601) |
| object            | Object       |
| nested            | [Object]     |

## Examples

The repository includes three production-ready examples:

### Products Example (`examples/`)
Basic e-commerce search with:
- Product filtering (category, brand, tags)
- Price ranges
- Static filters
- Simple configuration

```bash
cd examples
go run main.go
# Open http://localhost:8080/graphql
```

```bash
cd examples/process-flow
# Configure .env with your Elasticsearch credentials
go run main.go
# Open http://localhost:8081/graphql
```

## Custom Features

The process-flow example demonstrates custom feature implementation:

- **HiddenFilterFeature**: Filters without showing aggregations
- **DateBoolFeature**: Boolean filters based on date field existence
- **WildcardQueryFilterFeature**: Prefix/suffix wildcard search
- **RangeSlotFeature**: Range aggregations with predefined slots
- **InsightsFeature**: Scripted metric aggregations
- **MultiNestedDocumentFilterFeature**: Complex nested aggregations with hierarchies

See `examples/process-flow/features/` for implementations.

## Configuration

### Functional Configuration Pattern

The library uses functional options for clean, flexible configuration:

```go
// Create config with options
config := revealdgraphql.NewConfig(
    revealdgraphql.WithEnableFederation(),
    revealdgraphql.WithQueryNamespace("Products", false),
    revealdgraphql.WithQuery("searchProducts", &QueryConfig{...}),
    revealdgraphql.WithQuery("allProducts", &QueryConfig{...}),
)

// Or use the traditional approach
config := revealdgraphql.NewConfig()
config.AddQuery("searchProducts", &QueryConfig{...})
config.AddQuery("allProducts", &QueryConfig{...})
```

### Configuration Options

```go
// WithEnableFederation enables Apollo Federation v2 support
revealdgraphql.WithEnableFederation()

// WithQueryNamespace groups queries under a namespace type
// Example: WithQueryNamespace("Leads", false) → query { leads { ... } }
// The second parameter controls "extend type" for federation
revealdgraphql.WithQueryNamespace("Products", false)

// WithQuery adds a reveald feature-based query
revealdgraphql.WithQuery("name", &QueryConfig{...})

// WithPrecompiledQuery adds a precompiled query
revealdgraphql.WithPrecompiledQuery("name", &PrecompiledQueryConfig{...})
```

### QueryConfig

```go
type QueryConfig struct {
    Features    []reveald.Feature   // reveald features to apply
    Description string              // Query description for schema

    EnableAggregations bool         // Enable aggregations in results
    EnablePagination   bool         // Enable pagination fields
    EnableSorting      bool         // Enable sorting arguments

    FieldFilter        *FieldFilter // Include/exclude specific fields
    AggregationFields  []string     // Optional: manually add aggregation fields (auto-detected by default)
}
```

### Environment Configuration

All examples support environment-based configuration:

```env
ELASTICSEARCH_URL=http://localhost:9200
ELASTICSEARCH_INDEX=your-index-name
ELASTICSEARCH_USERNAME=elastic
ELASTICSEARCH_PASSWORD=your-password
SERVER_PORT=8080
```

## Flexible Elasticsearch Querying

For maximum flexibility, enable direct Elasticsearch query/aggregation support with typed GraphQL inputs:

```go
import (
    elasticsearch "github.com/elastic/go-elasticsearch/v8"
    "github.com/elastic/go-elasticsearch/v8/typedapi/types"
)

// Create ES typed client
esClient, _ := elasticsearch.NewTypedClient(elasticsearch.Config{
    Addresses: []string{"http://localhost:9200"},
})

// Configure with functional options
config := revealdgraphql.NewConfig(
    revealdgraphql.WithQuery("flexibleSearch", &QueryConfig{
        EnableElasticQuerying: true,
        EnableAggregations: true,

        // Optional: Always apply static filtering (e.g., tenant isolation)
        RootQuery: &types.Query{
            Term: map[string]types.TermQuery{
                "active": {Value: true},
            },
        },
    }),
)

// Create API with ES client
api, _ := New(backend, mapping, config, WithESClient(esClient))
```

### Query with Full ES DSL

```graphql
query {
  flexibleSearch(
    query: {
      bool: {
        must: [
          { range: { field: "price", gte: 100, lte: 1000 } }
          { terms: { field: "category", values: ["electronics", "computers"] } }
        ]
        should: [
          { match: { field: "description", query: "gaming" } }
        ]
      }
    }
    aggs: [
      {
        name: "brands",
        terms: { field: "brand.keyword", size: 20 }
        aggs: [
          { name: "avg_price", avg: { field: "price" } }
        ]
      }
      { name: "price_ranges", histogram: { field: "price", interval: 100 } }
      { name: "price_stats", stats: { field: "price" } }
    ]
  ) {
    hits { id name price brand }
    totalCount
    aggregations {
      brands { value count }
      price_ranges { value count }
    }
  }
}
```

### Supported Query Types

- **term**, **terms**: Exact matching
- **match**, **matchPhrase**: Full-text search
- **range**: Numeric/date ranges
- **bool**: Combine queries with must/should/filter/mustNot
- **exists**: Check field existence
- **nested**: Query nested objects
- **prefix**, **wildcard**: Pattern matching

### Supported Aggregations

- **terms**: Term buckets
- **dateHistogram**, **histogram**: Bucketing
- **stats**, **avg**, **sum**, **min**, **max**: Metrics
- **cardinality**: Unique value counts
- **Nested aggregations**: Full sub-aggregation support

### Root Query for Static Filtering

The `RootQuery` field lets you apply base filters that are always merged with user queries using a bool must clause. Perfect for:
- Multi-tenant isolation
- Soft-delete filtering
- Access control
- Default data scoping

## Advanced Features

### Filtering

All aggregation fields are automatically filterable:

```graphql
query {
  searchProducts(
    category: ["electronics", "computers"]
    price_min: 100
    price_max: 1000
  ) {
    hits { name price }
  }
}
```

### Pagination

```graphql
query {
  searchProducts(limit: 20, offset: 40) {
    hits { name }
    pagination {
      offset
      limit
      totalCount
    }
  }
}
```

### Sorting with Enums

```graphql
query {
  searchProducts(sort: price_desc) {  # Autocomplete!
    hits { name price }
  }
}
```

### Nested Aggregations

```graphql
query {
  searchVehicles {
    aggregations {
      tasks_process {
        value
        count
        filterValue  # Use this for filtering
        buckets {    # Nested sub-aggregations
          value
          count
          filterValue
        }
      }
    }
  }
}
```

## Architecture

1. **MappingParser** (`mapping.go`): Parses ES mapping JSON
2. **SchemaGenerator** (`schema.go`): Generates GraphQL schemas with auto-detection
3. **ResolverBuilder** (`resolver.go`): Creates resolvers executing reveald queries
4. **ArgumentReader** (`reader.go`): Converts GraphQL args to reveald Parameters
5. **GraphQLAPI** (`server.go`): HTTP server with GraphiQL

## How It Works

1. **Parse your ES mapping** → Extract field types and structure
2. **Configure your Features** → Standard reveald features
3. **Auto-detect everything**:
   - Aggregation fields from filter features
   - Sort options from SortingFeature
   - Filter arguments from aggregation fields
4. **Generate GraphQL schema** → Types, queries, arguments, enums
5. **Execute queries** → GraphQL args → reveald Parameters → ES query → Results

## Zero Configuration Philosophy

The library automatically detects:
- Which fields should have aggregations (from your Features)
- Which sort options are available (from SortingFeature)
- Which fields are filterable (from aggregations)
- Type conversions (ES types → GraphQL types)
- Nested object structures
- Hierarchical aggregation paths

**You just configure your reveald Features - everything else is automatic!**

## License

Same as reveald
