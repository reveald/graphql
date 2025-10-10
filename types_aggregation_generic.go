package graphql

import (
	"github.com/graphql-go/graphql"
)

// Generic aggregation types that can represent any Elasticsearch aggregation response

var (
	// GenericBucketType represents a bucket from any bucketing aggregation
	GenericBucketType *graphql.Object

	// GenericAggregationType represents any aggregation result
	GenericAggregationType *graphql.Object

	// StatsValuesType represents stats aggregation values
	StatsValuesType *graphql.Object

	// genericTypesInitialized tracks whether types have been initialized
	genericTypesInitialized = false
)

// initGenericAggregationTypes initializes the generic aggregation types once
func initGenericAggregationTypes() {
	if genericTypesInitialized {
		return
	}
	genericTypesInitialized = true

	// Initialize StatsValuesType first (no dependencies)
	StatsValuesType = graphql.NewObject(graphql.ObjectConfig{
		Name:        "StatsValues",
		Description: "Statistics aggregation values",
		Fields: graphql.Fields{
			"count": &graphql.Field{
				Type:        graphql.Int,
				Description: "Number of values",
			},
			"min": &graphql.Field{
				Type:        graphql.Float,
				Description: "Minimum value",
			},
			"max": &graphql.Field{
				Type:        graphql.Float,
				Description: "Maximum value",
			},
			"avg": &graphql.Field{
				Type:        graphql.Float,
				Description: "Average value",
			},
			"sum": &graphql.Field{
				Type:        graphql.Float,
				Description: "Sum of values",
			},
		},
	})

	// Initialize GenericAggregationType (will reference GenericBucketType in thunk)
	GenericAggregationType = graphql.NewObject(graphql.ObjectConfig{
		Name:        "GenericAggregation",
		Description: "A generic aggregation result that can represent any Elasticsearch aggregation type",
		Fields: (graphql.FieldsThunk)(func() graphql.Fields {
			return graphql.Fields{
				"name": &graphql.Field{
					Type:        graphql.NewNonNull(graphql.String),
					Description: "The name of this aggregation (e.g., 'by_category', 'price_stats')",
				},
				// For bucketing aggregations (terms, histogram, date_histogram, etc.)
				"buckets": &graphql.Field{
					Type:        graphql.NewList(GenericBucketType),
					Description: "Buckets for terms, histogram, or date_histogram aggregations",
				},
				// For metric aggregations
				"value": &graphql.Field{
					Type:        graphql.Float,
					Description: "Single metric value (for avg, sum, min, max, cardinality)",
				},
				// For stats aggregation
				"stats": &graphql.Field{
					Type:        StatsValuesType,
					Description: "Statistics values (for stats aggregation)",
				},
				// For filter/filters aggregation doc count
				"doc_count": &graphql.Field{
					Type:        graphql.Int,
					Description: "Document count (for filter aggregation)",
				},
				// For nested sub-aggregations in filter/filters
				"sub_aggregations": &graphql.Field{
					Type:        graphql.NewList(GenericAggregationType),
					Description: "Sub-aggregations (for filter, filters, nested aggregations)",
				},
			}
		}),
	})

	// Initialize GenericBucketType last (references GenericAggregationType)
	GenericBucketType = graphql.NewObject(graphql.ObjectConfig{
		Name:        "GenericBucket",
		Description: "A bucket from any bucketing aggregation (terms, histogram, date_histogram, etc.)",
		Fields: (graphql.FieldsThunk)(func() graphql.Fields {
			return graphql.Fields{
				"key": &graphql.Field{
					Type:        graphql.String,
					Description: "The bucket key",
				},
				"doc_count": &graphql.Field{
					Type:        graphql.Int,
					Description: "Number of documents in this bucket",
				},
				"sub_aggregations": &graphql.Field{
					Type:        graphql.NewList(GenericAggregationType),
					Description: "Nested aggregations within this bucket",
				},
			}
		}),
	})
}
