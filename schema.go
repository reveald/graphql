package graphql

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/graphql-go/graphql"
	"github.com/reveald/reveald"
)

// SchemaGenerator generates GraphQL schemas from Elasticsearch mappings
type SchemaGenerator struct {
	mapping         *IndexMapping
	config          *Config
	typeCache       map[string]*graphql.Object
	resolverBuilder *ResolverBuilder
	bucketType      *graphql.Object
	paginationType  *graphql.Object
}

// NewSchemaGenerator creates a new schema generator
func NewSchemaGenerator(mapping *IndexMapping, config *Config, resolverBuilder *ResolverBuilder) *SchemaGenerator {
	sg := &SchemaGenerator{
		mapping:         mapping,
		config:          config,
		typeCache:       make(map[string]*graphql.Object),
		resolverBuilder: resolverBuilder,
	}

	// Initialize shared types
	sg.bucketType = sg.createBucketType()
	sg.paginationType = sg.createPaginationType()

	return sg
}

// Generate generates a complete GraphQL schema
func (sg *SchemaGenerator) Generate() (graphql.Schema, error) {
	// Create the root Query type
	queryFields := graphql.Fields{}

	// Add regular queries
	for queryName, queryConfig := range sg.config.Queries {
		field, err := sg.generateQueryField(queryName, queryConfig)
		if err != nil {
			return graphql.Schema{}, fmt.Errorf("failed to generate query %s: %w", queryName, err)
		}
		queryFields[queryName] = field
	}

	// Add precompiled queries
	for queryName, queryConfig := range sg.config.PrecompiledQueries {
		field, err := sg.generatePrecompiledQueryField(queryName, queryConfig)
		if err != nil {
			return graphql.Schema{}, fmt.Errorf("failed to generate precompiled query %s: %w", queryName, err)
		}
		queryFields[queryName] = field
	}

	// If QueryNamespace is set, wrap all queries in a namespace object
	var rootQueryFields graphql.Fields
	if sg.config.QueryNamespace != "" {
		// Create namespace object type
		namespaceTypeName := capitalize(sg.config.QueryNamespace) + "Entity"
		namespaceType := graphql.NewObject(graphql.ObjectConfig{
			Name:        namespaceTypeName,
			Description: fmt.Sprintf("Grouped queries for %s", sg.config.QueryNamespace),
			Fields:      queryFields,
		})

		// Root Query only has the namespace field
		rootQueryFields = graphql.Fields{
			sg.config.QueryNamespace: &graphql.Field{
				Type:        namespaceType,
				Description: fmt.Sprintf("Access %s queries", sg.config.QueryNamespace),
				Resolve: func(p graphql.ResolveParams) (interface{}, error) {
					// Return empty object - actual queries resolve independently
					return map[string]interface{}{}, nil
				},
			},
		}
	} else {
		// No namespace - queries at root level
		rootQueryFields = queryFields
	}

	rootQuery := graphql.ObjectConfig{
		Name:   "Query",
		Fields: rootQueryFields,
	}

	schemaConfig := graphql.SchemaConfig{
		Query: graphql.NewObject(rootQuery),
	}

	// Add federation directives if enabled
	if sg.config.EnableFederation {
		federationDirectives := GetFederationDirectives()
		schemaConfig.Directives = append(
			[]*graphql.Directive{
				graphql.IncludeDirective,
				graphql.SkipDirective,
				graphql.DeprecatedDirective,
			},
			federationDirectives...,
		)
	}

	return graphql.NewSchema(schemaConfig)
}

// generateQueryField generates a GraphQL field for a search query
func (sg *SchemaGenerator) generateQueryField(queryName string, queryConfig *QueryConfig) (*graphql.Field, error) {
	// Generate the result type for this query
	resultType, err := sg.generateResultType(queryName, queryConfig)
	if err != nil {
		return nil, err
	}

	// Generate arguments for the query
	args := sg.generateQueryArguments(queryName, queryConfig)

	return &graphql.Field{
		Type:        resultType,
		Description: queryConfig.Description,
		Args:        args,
		Resolve:     sg.resolverBuilder.BuildResolver(queryName, queryConfig),
	}, nil
}

// generateResultType creates the result type for a query
func (sg *SchemaGenerator) generateResultType(queryName string, queryConfig *QueryConfig) (*graphql.Object, error) {
	// Create the document type
	docType, err := sg.generateDocumentType(queryName, queryConfig)
	if err != nil {
		return nil, err
	}

	fields := graphql.Fields{
		"hits": &graphql.Field{
			Type:        graphql.NewList(docType),
			Description: "The search results",
		},
		"totalCount": &graphql.Field{
			Type:        graphql.Int,
			Description: "Total number of hits",
		},
	}

	// Add aggregations if enabled
	if queryConfig.EnableAggregations {
		aggType := sg.generateAggregationsType(queryName, queryConfig)
		if aggType != nil {
			fields["aggregations"] = &graphql.Field{
				Type:        aggType,
				Description: "Aggregation results",
			}
		}
	}

	// Add pagination if enabled
	if queryConfig.EnablePagination {
		fields["pagination"] = &graphql.Field{
			Type:        sg.generatePaginationType(),
			Description: "Pagination information",
		}
	}

	return graphql.NewObject(graphql.ObjectConfig{
		Name:   fmt.Sprintf("%sResult", capitalize(queryName)),
		Fields: fields,
	}), nil
}

// generateDocumentType creates the GraphQL type for a document based on index name
func (sg *SchemaGenerator) generateDocumentType(queryName string, queryConfig *QueryConfig) (*graphql.Object, error) {
	// Use index name for document type (better for federation/entity resolution)
	indexName := sg.getIndexNameForType(queryConfig)
	typeName := fmt.Sprintf("%sDocument", sanitizeTypeName(indexName))

	// Check cache - multiple queries on same index share the same document type
	if cachedType, ok := sg.typeCache[typeName]; ok {
		return cachedType, nil
	}

	fields := graphql.Fields{}

	for fieldName, field := range sg.mapping.Properties {
		// Apply field filter
		if !sg.shouldIncludeField(fieldName, queryConfig.FieldFilter) {
			continue
		}

		gqlField, err := sg.convertFieldToGraphQL(field)
		if err != nil {
			return nil, fmt.Errorf("failed to convert field %s: %w", fieldName, err)
		}

		fields[fieldName] = gqlField
	}

	docType := graphql.NewObject(graphql.ObjectConfig{
		Name:   typeName,
		Fields: fields,
	})

	sg.typeCache[typeName] = docType
	return docType, nil
}

// convertFieldToGraphQL converts an ES field to a GraphQL field
func (sg *SchemaGenerator) convertFieldToGraphQL(field *Field) (*graphql.Field, error) {
	gqlType, err := sg.esTypeToGraphQLType(field, "")
	if err != nil {
		return nil, err
	}

	return &graphql.Field{
		Type: gqlType,
	}, nil
}

// esTypeToGraphQLType maps Elasticsearch types to GraphQL types
func (sg *SchemaGenerator) esTypeToGraphQLType(field *Field, parentPath string) (graphql.Output, error) {
	switch field.Type {
	case FieldTypeText, FieldTypeKeyword:
		return graphql.String, nil
	case FieldTypeLong, FieldTypeInteger, FieldTypeShort, FieldTypeByte:
		return graphql.Int, nil
	case FieldTypeDouble, FieldTypeFloat:
		return graphql.Float, nil
	case FieldTypeBoolean:
		return graphql.Boolean, nil
	case FieldTypeDate:
		return graphql.String, nil // Dates as ISO8601 strings
	case FieldTypeObject, FieldTypeNested:
		// Create nested object type
		if len(field.Properties) == 0 {
			return graphql.String, nil // Generic object as JSON string
		}

		// Create unique type name using parent path
		typeName := capitalize(field.Name) + "Object"
		if parentPath != "" {
			// Sanitize parent path for GraphQL type name
			sanitizedPath := strings.ReplaceAll(parentPath, ".", "_")
			sanitizedPath = strings.ReplaceAll(sanitizedPath, "/", "_")
			sanitizedPath = strings.ReplaceAll(sanitizedPath, "-", "_")
			typeName = capitalize(sanitizedPath) + capitalize(field.Name) + "Object"
		}

		// Check if we already created this type
		if cachedType, ok := sg.typeCache[typeName]; ok {
			if field.Type == FieldTypeNested {
				return graphql.NewList(cachedType), nil
			}
			return cachedType, nil
		}

		objFields := graphql.Fields{}
		childPath := field.Name
		if parentPath != "" {
			childPath = parentPath + "." + field.Name
		}

		for propName, prop := range field.Properties {
			gqlField, err := sg.esTypeToGraphQLType(prop, childPath)
			if err != nil {
				return nil, err
			}
			objFields[propName] = &graphql.Field{Type: gqlField}
		}

		objType := graphql.NewObject(graphql.ObjectConfig{
			Name:   typeName,
			Fields: objFields,
		})

		sg.typeCache[typeName] = objType

		if field.Type == FieldTypeNested {
			return graphql.NewList(objType), nil
		}
		return objType, nil
	default:
		return graphql.String, nil // Default to string
	}
}

// generateQueryArguments creates the arguments for a search query
func (sg *SchemaGenerator) generateQueryArguments(queryName string, queryConfig *QueryConfig) graphql.FieldConfigArgument {
	args := graphql.FieldConfigArgument{}

	// Add ES query/aggs arguments if EnableElasticQuerying is true
	if queryConfig.EnableElasticQuerying {
		args["query"] = &graphql.ArgumentConfig{
			Type:        createESQueryInputType(),
			Description: "Elasticsearch query DSL",
		}
		args["aggs"] = &graphql.ArgumentConfig{
			Type:        graphql.NewList(createESAggInputType()),
			Description: "Elasticsearch aggregations",
		}
	}

	// Add common search arguments from mapping
	for fieldName, field := range sg.mapping.Properties {
		if !sg.shouldIncludeField(fieldName, queryConfig.FieldFilter) {
			continue
		}

		// Only add filterable fields as arguments
		if sg.isFilterableField(field) {
			argType := sg.getFilterArgumentType(field)
			// Convert dots to underscores for GraphQL field names
			gqlFieldName := strings.ReplaceAll(fieldName, ".", "_")
			args[gqlFieldName] = &graphql.ArgumentConfig{
				Type: argType,
			}
		}
	}

	// Add arguments for auto-detected aggregation fields (like nested task filters)
	// These may not exist in the mapping but should still be filterable
	autoDetectedFields := extractAggregationFields(queryConfig.Features)
	for _, fieldName := range autoDetectedFields {
		gqlFieldName := strings.ReplaceAll(fieldName, ".", "_")
		// Skip if already added from mapping
		if _, exists := args[gqlFieldName]; !exists {
			// Default to list of strings for virtual fields
			args[gqlFieldName] = &graphql.ArgumentConfig{
				Type: graphql.NewList(graphql.String),
			}
		}
	}

	// Add pagination arguments
	if queryConfig.EnablePagination {
		args["limit"] = &graphql.ArgumentConfig{
			Type: graphql.Int,
		}
		args["offset"] = &graphql.ArgumentConfig{
			Type: graphql.Int,
		}
	}

	// Add sorting argument
	if queryConfig.EnableSorting {
		// Try to extract sort options from features and create enum
		sortOptions := extractSortOptions(queryConfig.Features)
		if len(sortOptions) > 0 {
			sortEnum := sg.createSortEnum(queryName, sortOptions)
			args["sort"] = &graphql.ArgumentConfig{
				Type:        sortEnum,
				Description: "Sort option",
			}
		} else {
			// Fallback to string if no options found
			args["sort"] = &graphql.ArgumentConfig{
				Type:        graphql.String,
				Description: "Sort option (e.g., 'lastCustomerUpdate-desc', 'status-asc')",
			}
		}
	}

	return args
}

// extractSortOptions extracts sort option names from SortingFeature in the features
func extractSortOptions(features []reveald.Feature) []string {
	for _, feature := range features {
		// Use reflection to check if this is a SortingFeature
		val := reflect.ValueOf(feature)
		if val.Kind() == reflect.Ptr {
			val = val.Elem()
		}

		typeName := val.Type().Name()
		if typeName == "SortingFeature" {
			// Access the private "options" field using reflection
			optionsField := val.FieldByName("options")
			if optionsField.IsValid() && optionsField.Kind() == reflect.Map {
				var sortOptions []string
				for _, key := range optionsField.MapKeys() {
					sortOptions = append(sortOptions, key.String())
				}
				return sortOptions
			}
		}
	}
	return nil
}

// extractAggregationFields extracts field names from ANY feature that exposes a property field
func extractAggregationFields(features []reveald.Feature) []string {
	var aggFields []string
	seenFields := make(map[string]bool)

	for _, feature := range features {
		val := reflect.ValueOf(feature)
		if val.Kind() == reflect.Ptr {
			val = val.Elem()
		}

		// Special handling for features with both "Prefix" and "Property" fields
		// (like MultiNestedDynamicFilterFeature)
		propField := val.FieldByName("Property")
		prefField := val.FieldByName("Prefix")
		subaggsField := val.FieldByName("subaggs")

		if propField.IsValid() && prefField.IsValid() &&
			propField.Kind() == reflect.String && prefField.Kind() == reflect.String {

			property := propField.String()
			prefix := prefField.String()

			// The actual aggregation name is prefix + property
			fieldName := prefix + property

			if !seenFields[fieldName] {
				aggFields = append(aggFields, fieldName)
				seenFields[fieldName] = true
			}

			// Also extract sub-aggregations
			if subaggsField.IsValid() && subaggsField.Kind() == reflect.Slice {
				for i := 0; i < subaggsField.Len(); i++ {
					subaggElem := subaggsField.Index(i)
					aggFieldField := subaggElem.FieldByName("AggField")

					if aggFieldField.IsValid() && aggFieldField.Kind() == reflect.String {
						subAggName := aggFieldField.String()

						// Add if not duplicate (subagg names already have prefix applied)
						if !seenFields[subAggName] {
							aggFields = append(aggFields, subAggName)
							seenFields[subAggName] = true
						}
					}
				}
			}

			continue
		}

		// Try to find a "property" field (used by most features)
		var fieldName string
		propertyField := val.FieldByName("property")
		if propertyField.IsValid() && propertyField.Kind() == reflect.String {
			fieldName = propertyField.String()
		}

		// Also check for "field" (some features might use this)
		if fieldName == "" {
			fieldField := val.FieldByName("field")
			if fieldField.IsValid() && fieldField.Kind() == reflect.String {
				fieldName = fieldField.String()
			}
		}

		// Also check for "name" (used by some features like RangeSlotFeature)
		if fieldName == "" {
			nameField := val.FieldByName("name")
			if nameField.IsValid() && nameField.Kind() == reflect.String {
				fieldName = nameField.String()
			}
		}

		// Add to list if we found a field name and haven't seen it before
		if fieldName != "" && !seenFields[fieldName] {
			aggFields = append(aggFields, fieldName)
			seenFields[fieldName] = true
		}
	}

	return aggFields
}

// createSortEnum creates a GraphQL enum type from sort options
func (sg *SchemaGenerator) createSortEnum(queryName string, sortOptions []string) *graphql.Enum {
	if len(sortOptions) == 0 {
		return nil
	}

	enumValues := graphql.EnumValueConfigMap{}
	for _, option := range sortOptions {
		// Convert "lastCustomerUpdate-desc" to "lastCustomerUpdate_desc" for GraphQL enum
		enumKey := strings.ReplaceAll(option, "-", "_")
		enumValues[enumKey] = &graphql.EnumValueConfig{
			Value:       option, // The actual value passed to reveald
			Description: option,
		}
	}

	return graphql.NewEnum(graphql.EnumConfig{
		Name:   fmt.Sprintf("%sSortOption", capitalize(queryName)),
		Values: enumValues,
	})
}

// createBucketType creates the shared Bucket type with support for nested buckets
func (sg *SchemaGenerator) createBucketType() *graphql.Object {
	// Use a recursive approach - bucket can contain buckets
	var bucketType *graphql.Object

	bucketType = graphql.NewObject(graphql.ObjectConfig{
		Name: "Bucket",
		Fields: (graphql.FieldsThunk)(func() graphql.Fields {
			return graphql.Fields{
				"value": &graphql.Field{
					Type:        graphql.String,
					Description: "The bucket value",
				},
				"count": &graphql.Field{
					Type:        graphql.Int,
					Description: "Number of documents in this bucket",
				},
				"filterValue": &graphql.Field{
					Type:        graphql.String,
					Description: "The full hierarchical value to use for filtering (e.g., 'Parent>Child')",
				},
				"buckets": &graphql.Field{
					Type:        graphql.NewList(bucketType),
					Description: "Nested buckets for hierarchical aggregations",
				},
			}
		}),
	})

	return bucketType
}

// createPaginationType creates the shared Pagination type
func (sg *SchemaGenerator) createPaginationType() *graphql.Object {
	return graphql.NewObject(graphql.ObjectConfig{
		Name: "Pagination",
		Fields: graphql.Fields{
			"offset": &graphql.Field{
				Type: graphql.Int,
			},
			"limit": &graphql.Field{
				Type: graphql.Int,
			},
			"totalCount": &graphql.Field{
				Type: graphql.Int,
			},
		},
	})
}

// generateAggregationsType creates the aggregations type
func (sg *SchemaGenerator) generateAggregationsType(queryName string, queryConfig *QueryConfig) *graphql.Object {
	aggFields := graphql.Fields{}

	// First try to get aggregation fields from configured Features
	autoDetectedFields := extractAggregationFields(queryConfig.Features)

	// Merge auto-detected fields with manual configuration
	fieldsMap := make(map[string]bool)

	// Add auto-detected fields
	for _, field := range autoDetectedFields {
		fieldsMap[field] = true
	}

	// Add manual fields
	for _, field := range queryConfig.AggregationFields {
		fieldsMap[field] = true
	}

	// Convert map to slice
	var fieldsToUse []string
	for field := range fieldsMap {
		fieldsToUse = append(fieldsToUse, field)
	}

	// Create set of auto-detected fields to trust unconditionally
	autoDetectedSet := make(map[string]bool)
	for _, field := range autoDetectedFields {
		autoDetectedSet[field] = true
	}

	// Add aggregation fields
	for _, fieldName := range fieldsToUse {
		isAutoDetected := autoDetectedSet[fieldName]
		fieldExists := sg.mapping.GetField(fieldName) != nil

		// Trust auto-detected fields (features know what they create)
		// OR fields that exist in mapping (for manual additions)
		if isAutoDetected || fieldExists {
			// Convert dots to underscores for GraphQL field names
			gqlFieldName := strings.ReplaceAll(fieldName, ".", "_")
			aggFields[gqlFieldName] = &graphql.Field{
				Type: graphql.NewList(sg.bucketType),
			}
		}
	}

	if len(aggFields) == 0 {
		return nil
	}

	return graphql.NewObject(graphql.ObjectConfig{
		Name:   fmt.Sprintf("%sAggregations", capitalize(queryName)),
		Fields: aggFields,
	})
}

// generatePaginationType returns the shared pagination type
func (sg *SchemaGenerator) generatePaginationType() *graphql.Object {
	return sg.paginationType
}

// isFilterableField determines if a field can be used for filtering
func (sg *SchemaGenerator) isFilterableField(field *Field) bool {
	switch field.Type {
	case FieldTypeKeyword, FieldTypeBoolean, FieldTypeLong, FieldTypeInteger:
		return true
	case FieldTypeText:
		// Text fields are filterable if they have a keyword multi-field
		_, hasKeyword := field.Fields["keyword"]
		return hasKeyword
	default:
		return false
	}
}

// getFilterArgumentType returns the GraphQL argument type for filtering
func (sg *SchemaGenerator) getFilterArgumentType(field *Field) graphql.Input {
	switch field.Type {
	case FieldTypeText, FieldTypeKeyword:
		return graphql.NewList(graphql.String)
	case FieldTypeBoolean:
		return graphql.Boolean
	case FieldTypeLong, FieldTypeInteger, FieldTypeShort, FieldTypeByte:
		return graphql.Int
	case FieldTypeDouble, FieldTypeFloat:
		return graphql.Float
	default:
		return graphql.String
	}
}

// shouldIncludeField checks if a field should be included based on the filter
func (sg *SchemaGenerator) shouldIncludeField(fieldName string, filter *FieldFilter) bool {
	if filter == nil {
		return true
	}

	// If include list is specified, field must be in it
	if len(filter.Include) > 0 {
		included := false
		for _, name := range filter.Include {
			if name == fieldName {
				included = true
				break
			}
		}
		if !included {
			return false
		}
	}

	// Check exclude list
	for _, name := range filter.Exclude {
		if name == fieldName {
			return false
		}
	}

	return true
}

func capitalize(s string) string {
	if len(s) == 0 {
		return s
	}
	// Only capitalize if first character is lowercase
	if s[0] >= 'a' && s[0] <= 'z' {
		return string(s[0]-32) + s[1:]
	}
	return s
}

// getIndexNameForType extracts the primary index name from query config
func (sg *SchemaGenerator) getIndexNameForType(queryConfig *QueryConfig) string {
	// Use first index if multiple
	if len(queryConfig.Indices) > 0 {
		return queryConfig.Indices[0]
	}
	// Fall back to single index
	if queryConfig.Index != "" {
		return queryConfig.Index
	}
	// Fall back to mapping index name
	return sg.mapping.IndexName
}

// getPrecompiledIndexNameForType extracts the primary index name from precompiled query config
func (sg *SchemaGenerator) getPrecompiledIndexNameForType(queryConfig *PrecompiledQueryConfig) string {
	// Use first index if multiple
	if len(queryConfig.Indices) > 0 {
		return queryConfig.Indices[0]
	}
	// Fall back to single index
	if queryConfig.Index != "" {
		return queryConfig.Index
	}
	// Fall back to mapping index name
	return sg.mapping.IndexName
}

// sanitizeTypeName converts index name to valid GraphQL type name
// Examples:
//   - "test-leads" → "TestLeads"
//   - "products" → "Products"
//   - "cross-domain-search-leads" → "CrossDomainSearchLeads"
func sanitizeTypeName(indexName string) string {
	// Replace hyphens and underscores with spaces for splitting
	name := strings.ReplaceAll(indexName, "-", " ")
	name = strings.ReplaceAll(name, "_", " ")

	// Split into words and capitalize each
	words := strings.Fields(name)
	for i, word := range words {
		words[i] = capitalize(word)
	}

	// Join without spaces
	return strings.Join(words, "")
}
