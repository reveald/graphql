package graphql

import "github.com/graphql-go/graphql"

// createESQueryInputType creates GraphQL input type for ES queries
func createESQueryInputType() *graphql.InputObject {
	// Forward declare for recursive Bool query
	var esQueryInputType *graphql.InputObject

	esQueryInputType = graphql.NewInputObject(graphql.InputObjectConfig{
		Name: "ESQueryInput",
		Fields: graphql.InputObjectConfigFieldMap{
			"term": &graphql.InputObjectFieldConfig{
				Type: graphql.NewInputObject(graphql.InputObjectConfig{
					Name: "ESTermQueryInput",
					Fields: graphql.InputObjectConfigFieldMap{
						"field": &graphql.InputObjectFieldConfig{Type: graphql.NewNonNull(graphql.String)},
						"value": &graphql.InputObjectFieldConfig{Type: graphql.NewNonNull(graphql.String)},
					},
				}),
			},
			"terms": &graphql.InputObjectFieldConfig{
				Type: graphql.NewInputObject(graphql.InputObjectConfig{
					Name: "ESTermsQueryInput",
					Fields: graphql.InputObjectConfigFieldMap{
						"field":  &graphql.InputObjectFieldConfig{Type: graphql.NewNonNull(graphql.String)},
						"values": &graphql.InputObjectFieldConfig{Type: graphql.NewNonNull(graphql.NewList(graphql.String))},
					},
				}),
			},
			"match": &graphql.InputObjectFieldConfig{
				Type: graphql.NewInputObject(graphql.InputObjectConfig{
					Name: "ESMatchQueryInput",
					Fields: graphql.InputObjectConfigFieldMap{
						"field": &graphql.InputObjectFieldConfig{Type: graphql.NewNonNull(graphql.String)},
						"query": &graphql.InputObjectFieldConfig{Type: graphql.NewNonNull(graphql.String)},
					},
				}),
			},
			"range": &graphql.InputObjectFieldConfig{
				Type: graphql.NewInputObject(graphql.InputObjectConfig{
					Name: "ESRangeQueryInput",
					Fields: graphql.InputObjectConfigFieldMap{
						"field": &graphql.InputObjectFieldConfig{Type: graphql.NewNonNull(graphql.String)},
						"gte":   &graphql.InputObjectFieldConfig{Type: graphql.Float},
						"gt":    &graphql.InputObjectFieldConfig{Type: graphql.Float},
						"lte":   &graphql.InputObjectFieldConfig{Type: graphql.Float},
						"lt":    &graphql.InputObjectFieldConfig{Type: graphql.Float},
					},
				}),
			},
			"bool": &graphql.InputObjectFieldConfig{
				Type: graphql.NewInputObject(graphql.InputObjectConfig{
					Name: "ESBoolQueryInput",
					Fields: (graphql.InputObjectConfigFieldMapThunk)(func() graphql.InputObjectConfigFieldMap {
						return graphql.InputObjectConfigFieldMap{
							"must":    &graphql.InputObjectFieldConfig{Type: graphql.NewList(esQueryInputType)},
							"should":  &graphql.InputObjectFieldConfig{Type: graphql.NewList(esQueryInputType)},
							"filter":  &graphql.InputObjectFieldConfig{Type: graphql.NewList(esQueryInputType)},
							"mustNot": &graphql.InputObjectFieldConfig{Type: graphql.NewList(esQueryInputType)},
						}
					}),
				}),
			},
			"exists": &graphql.InputObjectFieldConfig{
				Type: graphql.NewInputObject(graphql.InputObjectConfig{
					Name: "ESExistsQueryInput",
					Fields: graphql.InputObjectConfigFieldMap{
						"field": &graphql.InputObjectFieldConfig{Type: graphql.NewNonNull(graphql.String)},
					},
				}),
			},
			"nested": &graphql.InputObjectFieldConfig{
				Type: graphql.NewInputObject(graphql.InputObjectConfig{
					Name: "ESNestedQueryInput",
					Fields: (graphql.InputObjectConfigFieldMapThunk)(func() graphql.InputObjectConfigFieldMap {
						return graphql.InputObjectConfigFieldMap{
							"path":  &graphql.InputObjectFieldConfig{Type: graphql.NewNonNull(graphql.String)},
							"query": &graphql.InputObjectFieldConfig{Type: graphql.NewNonNull(esQueryInputType)},
						}
					}),
				}),
			},
			"prefix": &graphql.InputObjectFieldConfig{
				Type: graphql.NewInputObject(graphql.InputObjectConfig{
					Name: "ESPrefixQueryInput",
					Fields: graphql.InputObjectConfigFieldMap{
						"field": &graphql.InputObjectFieldConfig{Type: graphql.NewNonNull(graphql.String)},
						"value": &graphql.InputObjectFieldConfig{Type: graphql.NewNonNull(graphql.String)},
					},
				}),
			},
			"wildcard": &graphql.InputObjectFieldConfig{
				Type: graphql.NewInputObject(graphql.InputObjectConfig{
					Name: "ESWildcardQueryInput",
					Fields: graphql.InputObjectConfigFieldMap{
						"field": &graphql.InputObjectFieldConfig{Type: graphql.NewNonNull(graphql.String)},
						"value": &graphql.InputObjectFieldConfig{Type: graphql.NewNonNull(graphql.String)},
					},
				}),
			},
		},
	})

	return esQueryInputType
}

// createESAggInputType creates GraphQL input type for ES aggregations
func createESAggInputType() *graphql.InputObject {
	// Forward declare for recursive sub-aggs
	var esAggInputType *graphql.InputObject

	esAggInputType = graphql.NewInputObject(graphql.InputObjectConfig{
		Name: "ESAggInput",
		Fields: (graphql.InputObjectConfigFieldMapThunk)(func() graphql.InputObjectConfigFieldMap {
			return graphql.InputObjectConfigFieldMap{
				"name": &graphql.InputObjectFieldConfig{Type: graphql.NewNonNull(graphql.String)},
				"terms": &graphql.InputObjectFieldConfig{
					Type: graphql.NewInputObject(graphql.InputObjectConfig{
						Name: "ESTermsAggInput",
						Fields: graphql.InputObjectConfigFieldMap{
							"field": &graphql.InputObjectFieldConfig{Type: graphql.NewNonNull(graphql.String)},
							"size":  &graphql.InputObjectFieldConfig{Type: graphql.Int},
						},
					}),
				},
				"dateHistogram": &graphql.InputObjectFieldConfig{
					Type: graphql.NewInputObject(graphql.InputObjectConfig{
						Name: "ESDateHistogramAggInput",
						Fields: graphql.InputObjectConfigFieldMap{
							"field":            &graphql.InputObjectFieldConfig{Type: graphql.NewNonNull(graphql.String)},
							"calendarInterval": &graphql.InputObjectFieldConfig{Type: graphql.String},
							"fixedInterval":    &graphql.InputObjectFieldConfig{Type: graphql.String},
							"format":           &graphql.InputObjectFieldConfig{Type: graphql.String},
							"minDocCount":      &graphql.InputObjectFieldConfig{Type: graphql.Int},
						},
					}),
				},
				"histogram": &graphql.InputObjectFieldConfig{
					Type: graphql.NewInputObject(graphql.InputObjectConfig{
						Name: "ESHistogramAggInput",
						Fields: graphql.InputObjectConfigFieldMap{
							"field":    &graphql.InputObjectFieldConfig{Type: graphql.NewNonNull(graphql.String)},
							"interval": &graphql.InputObjectFieldConfig{Type: graphql.NewNonNull(graphql.Float)},
						},
					}),
				},
				"stats": &graphql.InputObjectFieldConfig{
					Type: graphql.NewInputObject(graphql.InputObjectConfig{
						Name: "ESStatsAggInput",
						Fields: graphql.InputObjectConfigFieldMap{
							"field": &graphql.InputObjectFieldConfig{Type: graphql.NewNonNull(graphql.String)},
						},
					}),
				},
				"avg": &graphql.InputObjectFieldConfig{
					Type: graphql.NewInputObject(graphql.InputObjectConfig{
						Name: "ESAvgAggInput",
						Fields: graphql.InputObjectConfigFieldMap{
							"field": &graphql.InputObjectFieldConfig{Type: graphql.NewNonNull(graphql.String)},
						},
					}),
				},
				"sum": &graphql.InputObjectFieldConfig{
					Type: graphql.NewInputObject(graphql.InputObjectConfig{
						Name: "ESSumAggInput",
						Fields: graphql.InputObjectConfigFieldMap{
							"field": &graphql.InputObjectFieldConfig{Type: graphql.NewNonNull(graphql.String)},
						},
					}),
				},
				"min": &graphql.InputObjectFieldConfig{
					Type: graphql.NewInputObject(graphql.InputObjectConfig{
						Name: "ESMinAggInput",
						Fields: graphql.InputObjectConfigFieldMap{
							"field": &graphql.InputObjectFieldConfig{Type: graphql.NewNonNull(graphql.String)},
						},
					}),
				},
				"max": &graphql.InputObjectFieldConfig{
					Type: graphql.NewInputObject(graphql.InputObjectConfig{
						Name: "ESMaxAggInput",
						Fields: graphql.InputObjectConfigFieldMap{
							"field": &graphql.InputObjectFieldConfig{Type: graphql.NewNonNull(graphql.String)},
						},
					}),
				},
				"cardinality": &graphql.InputObjectFieldConfig{
					Type: graphql.NewInputObject(graphql.InputObjectConfig{
						Name: "ESCardinalityAggInput",
						Fields: graphql.InputObjectConfigFieldMap{
							"field": &graphql.InputObjectFieldConfig{Type: graphql.NewNonNull(graphql.String)},
						},
					}),
				},
				"aggs": &graphql.InputObjectFieldConfig{
					Type: graphql.NewList(esAggInputType),
				},
			}
		}),
	})

	return esAggInputType
}
