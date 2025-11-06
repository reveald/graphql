package graphql

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	elasticsearch "github.com/elastic/go-elasticsearch/v8"
	"github.com/graphql-go/graphql"
	"github.com/reveald/reveald/v2"
)

// contextKey is a custom type for context keys to avoid collisions
type contextKey string

// httpRequestKey is the context key for storing the HTTP request
const httpRequestKey = contextKey("httpRequest")

// GraphQLAPI is the main GraphQL server
type GraphQLAPI struct {
	backend         reveald.Backend
	esClient        *elasticsearch.TypedClient
	schema          graphql.Schema
	config          *Config
	entityKeys      map[string][]string                       // Resolvable entities (in _Entity union)
	sdlEntityKeys   map[string][]string                       // All entities with @key directives (for SDL generation)
	fieldDirectives map[string]map[string]map[string]string   // Field-level directives (e.g., @requires, @external)
}

// Option is a functional option for configuring the GraphQL API
type Option func(*GraphQLAPI)

// New creates a new GraphQL API
func New(backend reveald.Backend, config *Config, opts ...Option) (*GraphQLAPI, error) {
	api := &GraphQLAPI{
		backend: backend,
		config:  config,
	}

	for _, opt := range opts {
		opt(api)
	}

	// Build the resolver
	resolverBuilder := NewResolverBuilder(backend, api.esClient)

	// Generate the schema
	generator := NewSchemaGenerator(config, resolverBuilder)
	schema, err := generator.Generate()
	if err != nil {
		return nil, fmt.Errorf("failed to generate schema: %w", err)
	}

	api.schema = schema
	api.entityKeys = generator.entityKeys
	api.sdlEntityKeys = generator.sdlEntityKeys
	api.fieldDirectives = generator.fieldDirectives

	return api, nil
}

// WithESClient sets the Elasticsearch typed client for raw query support
func WithESClient(client *elasticsearch.TypedClient) Option {
	return func(api *GraphQLAPI) {
		api.esClient = client
	}
}

// ServeHTTP implements http.Handler
func (api *GraphQLAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		// Serve GraphiQL for GET requests
		api.serveGraphiQL(w, r)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse request
	var req struct {
		Query         string         `json:"query"`
		Variables     map[string]any `json:"variables"`
		OperationName string         `json:"operationName"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Execute query with HTTP request in context
	ctx := context.WithValue(r.Context(), httpRequestKey, r)
	result := graphql.Do(graphql.Params{
		Schema:         api.schema,
		RequestString:  req.Query,
		VariableValues: req.Variables,
		OperationName:  req.OperationName,
		Context:        ctx,
	})

	// Write response
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(result); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// serveGraphiQL serves the GraphiQL interface
func (api *GraphQLAPI) serveGraphiQL(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(graphiQLHTML))
}

// GetSchema returns the generated GraphQL schema
func (api *GraphQLAPI) GetSchema() graphql.Schema {
	return api.schema
}

// ExportSDL exports the schema as SDL (Schema Definition Language)
// with optional Apollo Federation v2 annotations
func (api *GraphQLAPI) ExportSDL() string {
	return ExportFederationSDL(api.schema, api.config, api.sdlEntityKeys, api.entityKeys, api.fieldDirectives)
}

const graphiQLHTML = `
<!DOCTYPE html>
<html>
<head>
  <title>GraphiQL</title>
  <style>
    body {
      height: 100vh;
      margin: 0;
      overflow: hidden;
    }
    #graphiql {
      height: 100vh;
    }
  </style>
  <link href="https://unpkg.com/graphiql/graphiql.min.css" rel="stylesheet" />
</head>
<body>
  <div id="graphiql">Loading...</div>
  <script
    crossorigin
    src="https://unpkg.com/react/umd/react.production.min.js"
  ></script>
  <script
    crossorigin
    src="https://unpkg.com/react-dom/umd/react-dom.production.min.js"
  ></script>
  <script
    crossorigin
    src="https://unpkg.com/graphiql/graphiql.min.js"
  ></script>
  <script>
    const fetcher = GraphiQL.createFetcher({ url: window.location.pathname });
    ReactDOM.render(
      React.createElement(GraphiQL, { fetcher: fetcher }),
      document.getElementById('graphiql'),
    );
  </script>
</body>
</html>
`
