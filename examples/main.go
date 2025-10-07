package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	elasticsearch "github.com/elastic/go-elasticsearch/v8"
	revealdgraphql "github.com/reveald/graphql"
	"github.com/reveald/reveald"
	"github.com/reveald/reveald/featureset"
)

func main() {
	// Create Elasticsearch backend
	backend, err := reveald.NewElasticBackend([]string{"http://localhost:9200"})
	if err != nil {
		log.Fatalf("Failed to create backend: %v", err)
	}

	// Load the Elasticsearch mapping
	mappingData, err := os.ReadFile("products-mapping.json")
	if err != nil {
		log.Fatalf("Failed to read mapping: %v", err)
	}

	mapping, err := revealdgraphql.ParseMapping("products", mappingData)
	if err != nil {
		log.Fatalf("Failed to parse mapping: %v", err)
	}

	// Configure the GraphQL API
	config := revealdgraphql.NewConfig()

	// Add a search query with features
	config.AddQuery("searchProducts", &revealdgraphql.QueryConfig{
		Index:       "products",
		Description: "Search for products with filtering, pagination, and aggregations",
		Features: []reveald.Feature{
			featureset.NewPaginationFeature(
				featureset.WithPageSize(20),
				featureset.WithMaxPageSize(100),
			),
			// Use DynamicFilterFeature for text fields with .keyword multi-fields
			featureset.NewDynamicFilterFeature("category"),
			featureset.NewDynamicFilterFeature("brand"),
			featureset.NewDynamicFilterFeature("tags"),
			featureset.NewStaticFilterFeature(
				featureset.WithRequiredValue("active", true),
			),
		},
		EnableAggregations: true,
		EnablePagination:   true,
		EnableSorting:      true,
		// Aggregation fields are auto-detected from Features!
	})

	// Add another query for all products (no active filter)
	config.AddQuery("allProducts", &revealdgraphql.QueryConfig{
		Index:       "products",
		Description: "Get all products without filters",
		Features: []reveald.Feature{
			featureset.NewPaginationFeature(
				featureset.WithPageSize(10),
			),
		},
		EnablePagination: true,
	})

	// Add a flexible query with typed Elasticsearch querying
	config.AddQuery("flexibleSearch", &revealdgraphql.QueryConfig{
		Index:                 "products",
		Description:           "Flexible search with full ES query/aggregation support",
		EnableElasticQuerying: true,
		EnableAggregations:    true,
		EnablePagination:      true,
		// RootQuery: Always filter for active products
		// RootQuery: &types.Query{
		// 	Term: map[string]types.TermQuery{
		// 		"active": {Value: true},
		// 	},
		// },
	})

	// Create Elasticsearch typed client for flexible querying
	esClient, err := elasticsearch.NewTypedClient(elasticsearch.Config{
		Addresses: []string{"http://localhost:9200"},
	})
	if err != nil {
		log.Fatalf("Failed to create ES client: %v", err)
	}

	// Create the GraphQL API with ES client option
	api, err := revealdgraphql.New(backend, mapping, config, revealdgraphql.WithESClient(esClient))
	if err != nil {
		log.Fatalf("Failed to create GraphQL API: %v", err)
	}

	// Start the HTTP server
	fmt.Println("GraphQL server starting on http://localhost:8080/graphql")
	fmt.Println("Open http://localhost:8080/graphql in your browser to use GraphiQL")
	fmt.Println()
	fmt.Println("Example queries:")
	fmt.Println(`
# Feature-based query:
query {
  searchProducts(category: ["electronics"], limit: 10) {
    hits {
      id
      name
      price
      category
      brand
    }
    totalCount
    aggregations {
      category { value count }
      brand { value count }
      tags { value count }
    }
  }
}

# Flexible ES query:
query {
  flexibleSearch(
    query: {
      bool: {
        must: [
          { range: { field: "price", gte: 100, lte: 1000 } }
          { terms: { field: "category", values: ["electronics"] } }
        ]
      }
    }
    aggs: [
      { name: "brands", terms: { field: "brand", size: 10 } }
      { name: "price_stats", stats: { field: "price" } }
    ]
  ) {
    hits {
      id
      name
      price
      category
    }
    totalCount
    aggregations {
      brands { value count }
    }
  }
}
`)

	if err := http.ListenAndServe(":8080", api); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
