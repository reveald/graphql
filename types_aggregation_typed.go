package graphql

import (
	"fmt"
	"strings"

	"github.com/elastic/go-elasticsearch/v8/typedapi/types"
	"github.com/graphql-go/graphql"
)

// generateTypedAggregationsType creates a strongly-typed aggregations object from ES aggregation definitions
func (sg *SchemaGenerator) generateTypedAggregationsType(queryName string, aggs map[string]types.Aggregations) *graphql.Object {
	if len(aggs) == 0 {
		return nil
	}

	typeName := fmt.Sprintf("%sAggregations", capitalize(queryName))

	// Check cache
	if cachedType, ok := sg.typeCache[typeName]; ok {
		return cachedType
	}

	fields := graphql.Fields{}
	for aggName, aggDef := range aggs {
		fieldName := sanitizeFieldName(aggName)
		fieldType := sg.generateAggregationType(queryName, aggName, aggDef, "")
		if fieldType != nil {
			fields[fieldName] = &graphql.Field{
				Type:        fieldType,
				Description: fmt.Sprintf("Aggregation: %s", aggName),
			}
		}
	}

	if len(fields) == 0 {
		return nil
	}

	aggregationsType := graphql.NewObject(graphql.ObjectConfig{
		Name:        typeName,
		Description: "Strongly-typed aggregation results",
		Fields:      fields,
	})

	sg.typeCache[typeName] = aggregationsType
	return aggregationsType
}

// generateAggregationType generates a GraphQL type for a single aggregation
func (sg *SchemaGenerator) generateAggregationType(
	queryName string,
	aggName string,
	aggDef types.Aggregations,
	pathPrefix string,
) graphql.Type {
	// Build hierarchical type path
	typePath := pathPrefix + capitalize(aggName)

	// Check which aggregation type is set and generate appropriate type
	if aggDef.Terms != nil {
		return sg.generateTermsAggType(queryName, typePath, aggDef)
	} else if aggDef.DateHistogram != nil {
		return sg.generateDateHistogramAggType(queryName, typePath, aggDef)
	} else if aggDef.Histogram != nil {
		return sg.generateHistogramAggType(queryName, typePath, aggDef)
	} else if aggDef.Filters != nil {
		return sg.generateFiltersAggType(queryName, typePath, aggDef)
	} else if aggDef.Filter != nil {
		return sg.generateFilterAggType(queryName, typePath, aggDef)
	} else if aggDef.Nested != nil {
		return sg.generateNestedAggType(queryName, typePath, aggDef)
	} else if aggDef.Avg != nil || aggDef.Sum != nil || aggDef.Min != nil || aggDef.Max != nil {
		// Metric aggregations return scalar values
		return graphql.Float
	} else if aggDef.Cardinality != nil {
		return graphql.Int
	} else if aggDef.Stats != nil {
		// Reuse existing StatsValuesType
		initGenericAggregationTypes()
		return StatsValuesType
	}

	// Fallback to generic type for unknown aggregation types
	initGenericAggregationTypes()
	return GenericAggregationType
}

// generateTermsAggType generates a type for Terms aggregation
func (sg *SchemaGenerator) generateTermsAggType(
	queryName string,
	typePath string,
	aggDef types.Aggregations,
) *graphql.Object {
	typeName := fmt.Sprintf("%s%s", capitalize(queryName), typePath)

	// Check cache
	if cachedType, ok := sg.typeCache[typeName]; ok {
		return cachedType
	}

	// Create bucket type for this terms aggregation
	bucketType := sg.generateBucketType(queryName, typePath, aggDef.Aggregations)

	termsType := graphql.NewObject(graphql.ObjectConfig{
		Name:        typeName,
		Description: "Terms aggregation result",
		Fields: graphql.Fields{
			"buckets": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(bucketType))),
				Description: "Buckets grouped by term values",
			},
		},
	})

	sg.typeCache[typeName] = termsType
	return termsType
}

// generateDateHistogramAggType generates a type for DateHistogram aggregation
func (sg *SchemaGenerator) generateDateHistogramAggType(
	queryName string,
	typePath string,
	aggDef types.Aggregations,
) *graphql.Object {
	typeName := fmt.Sprintf("%s%s", capitalize(queryName), typePath)

	// Check cache
	if cachedType, ok := sg.typeCache[typeName]; ok {
		return cachedType
	}

	// Create bucket type for this date histogram aggregation
	bucketType := sg.generateBucketType(queryName, typePath, aggDef.Aggregations)

	dateHistogramType := graphql.NewObject(graphql.ObjectConfig{
		Name:        typeName,
		Description: "Date histogram aggregation result",
		Fields: graphql.Fields{
			"buckets": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(bucketType))),
				Description: "Buckets grouped by date intervals",
			},
		},
	})

	sg.typeCache[typeName] = dateHistogramType
	return dateHistogramType
}

// generateHistogramAggType generates a type for Histogram aggregation
func (sg *SchemaGenerator) generateHistogramAggType(
	queryName string,
	typePath string,
	aggDef types.Aggregations,
) *graphql.Object {
	typeName := fmt.Sprintf("%s%s", capitalize(queryName), typePath)

	// Check cache
	if cachedType, ok := sg.typeCache[typeName]; ok {
		return cachedType
	}

	// Create bucket type for this histogram aggregation
	bucketType := sg.generateBucketType(queryName, typePath, aggDef.Aggregations)

	histogramType := graphql.NewObject(graphql.ObjectConfig{
		Name:        typeName,
		Description: "Histogram aggregation result",
		Fields: graphql.Fields{
			"buckets": &graphql.Field{
				Type:        graphql.NewNonNull(graphql.NewList(graphql.NewNonNull(bucketType))),
				Description: "Buckets grouped by numeric intervals",
			},
		},
	})

	sg.typeCache[typeName] = histogramType
	return histogramType
}

// generateBucketType generates a bucket type for bucketing aggregations
func (sg *SchemaGenerator) generateBucketType(
	queryName string,
	typePath string,
	nestedAggs map[string]types.Aggregations,
) *graphql.Object {
	typeName := fmt.Sprintf("%s%sBucket", capitalize(queryName), typePath)

	// Check cache
	if cachedType, ok := sg.typeCache[typeName]; ok {
		return cachedType
	}

	fields := graphql.Fields{
		"key": &graphql.Field{
			Type:        graphql.NewNonNull(graphql.String),
			Description: "The bucket key value",
		},
		"doc_count": &graphql.Field{
			Type:        graphql.NewNonNull(graphql.Int),
			Description: "Number of documents in this bucket",
		},
	}

	// Add nested aggregation fields
	for nestedName, nestedDef := range nestedAggs {
		fieldName := sanitizeFieldName(nestedName)
		fieldType := sg.generateAggregationType(queryName, nestedName, nestedDef, typePath)
		if fieldType != nil {
			fields[fieldName] = &graphql.Field{
				Type:        fieldType,
				Description: fmt.Sprintf("Nested aggregation: %s", nestedName),
			}
		}
	}

	bucketType := graphql.NewObject(graphql.ObjectConfig{
		Name:        typeName,
		Description: "Bucket with aggregated data",
		Fields:      fields,
	})

	sg.typeCache[typeName] = bucketType
	return bucketType
}

// generateFiltersAggType generates a type for Filters aggregation (named filters)
func (sg *SchemaGenerator) generateFiltersAggType(
	queryName string,
	typePath string,
	aggDef types.Aggregations,
) *graphql.Object {
	typeName := fmt.Sprintf("%s%s", capitalize(queryName), typePath)

	// Check cache
	if cachedType, ok := sg.typeCache[typeName]; ok {
		return cachedType
	}

	fields := graphql.Fields{}

	// Get filter names from the Filters map
	if aggDef.Filters != nil && aggDef.Filters.Filters != nil {
		// Try to cast to map[string]types.Query (keyed filters)
		if filterMap, ok := aggDef.Filters.Filters.(map[string]types.Query); ok {
			// Create a common bucket type for all filter buckets
			bucketTypeName := fmt.Sprintf("%s%sFilterBucket", capitalize(queryName), typePath)

			// Check if bucket type already cached
			var bucketType *graphql.Object
			if cachedBucketType, ok := sg.typeCache[bucketTypeName]; ok {
				bucketType = cachedBucketType
			} else {
				bucketFields := graphql.Fields{
					"doc_count": &graphql.Field{
						Type:        graphql.NewNonNull(graphql.Int),
						Description: "Number of documents matching this filter",
					},
				}

				// Add nested aggregation fields
				for nestedName, nestedDef := range aggDef.Aggregations {
					fieldName := sanitizeFieldName(nestedName)
					fieldType := sg.generateAggregationType(queryName, nestedName, nestedDef, typePath)
					if fieldType != nil {
						bucketFields[fieldName] = &graphql.Field{
							Type:        fieldType,
							Description: fmt.Sprintf("Nested aggregation: %s", nestedName),
						}
					}
				}

				bucketType = graphql.NewObject(graphql.ObjectConfig{
					Name:        bucketTypeName,
					Description: "Filter bucket result",
					Fields:      bucketFields,
				})

				sg.typeCache[bucketTypeName] = bucketType
			}

			// Add a field for each named filter
			for filterName := range filterMap {
				fieldName := sanitizeFieldName(filterName)
				fields[fieldName] = &graphql.Field{
					Type:        bucketType,
					Description: fmt.Sprintf("Filter: %s", filterName),
				}
			}
		}
	}

	if len(fields) == 0 {
		// Fallback if no filters defined or if filters are array-based
		initGenericAggregationTypes()
		return GenericAggregationType
	}

	filtersType := graphql.NewObject(graphql.ObjectConfig{
		Name:        typeName,
		Description: "Filters aggregation result with named filters",
		Fields:      fields,
	})

	sg.typeCache[typeName] = filtersType
	return filtersType
}

// generateFilterAggType generates a type for Filter aggregation (single filter)
func (sg *SchemaGenerator) generateFilterAggType(
	queryName string,
	typePath string,
	aggDef types.Aggregations,
) *graphql.Object {
	typeName := fmt.Sprintf("%s%s", capitalize(queryName), typePath)

	// Check cache
	if cachedType, ok := sg.typeCache[typeName]; ok {
		return cachedType
	}

	fields := graphql.Fields{
		"doc_count": &graphql.Field{
			Type:        graphql.NewNonNull(graphql.Int),
			Description: "Number of documents matching this filter",
		},
	}

	// Add nested aggregation fields
	for nestedName, nestedDef := range aggDef.Aggregations {
		fieldName := sanitizeFieldName(nestedName)
		fieldType := sg.generateAggregationType(queryName, nestedName, nestedDef, typePath)
		if fieldType != nil {
			fields[fieldName] = &graphql.Field{
				Type:        fieldType,
				Description: fmt.Sprintf("Nested aggregation: %s", nestedName),
			}
		}
	}

	filterType := graphql.NewObject(graphql.ObjectConfig{
		Name:        typeName,
		Description: "Filter aggregation result",
		Fields:      fields,
	})

	sg.typeCache[typeName] = filterType
	return filterType
}

// generateNestedAggType generates a type for Nested aggregation
func (sg *SchemaGenerator) generateNestedAggType(
	queryName string,
	typePath string,
	aggDef types.Aggregations,
) *graphql.Object {
	typeName := fmt.Sprintf("%s%s", capitalize(queryName), typePath)

	// Check cache
	if cachedType, ok := sg.typeCache[typeName]; ok {
		return cachedType
	}

	fields := graphql.Fields{
		"doc_count": &graphql.Field{
			Type:        graphql.NewNonNull(graphql.Int),
			Description: "Number of nested documents",
		},
	}

	// Add nested aggregation fields
	for nestedName, nestedDef := range aggDef.Aggregations {
		fieldName := sanitizeFieldName(nestedName)
		fieldType := sg.generateAggregationType(queryName, nestedName, nestedDef, typePath)
		if fieldType != nil {
			fields[fieldName] = &graphql.Field{
				Type:        fieldType,
				Description: fmt.Sprintf("Nested aggregation: %s", nestedName),
			}
		}
	}

	nestedType := graphql.NewObject(graphql.ObjectConfig{
		Name:        typeName,
		Description: "Nested aggregation result",
		Fields:      fields,
	})

	sg.typeCache[typeName] = nestedType
	return nestedType
}

// sanitizeFieldName converts aggregation names to valid GraphQL field names
func sanitizeFieldName(name string) string {
	// Replace dots and hyphens with underscores
	result := strings.ReplaceAll(name, ".", "_")
	result = strings.ReplaceAll(result, "-", "_")

	// Ensure it starts with a letter or underscore
	if len(result) > 0 && !isLetter(rune(result[0])) && result[0] != '_' {
		result = "_" + result
	}

	return result
}

// isLetter checks if a rune is a letter
func isLetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}
