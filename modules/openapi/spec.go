package openapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
)

const (
	openAPIVersion = "3.1.0"
	jsonMediaType  = "application/json"
)

type RouteSpec struct {
	Method      string
	Path        string
	OperationID string
	Summary     string
	Description string
	Tags        []string
	Auth        bool
	Parameters  []ParameterSpec
	RequestBody any
	Responses   []ResponseSpec
}

type ParameterSpec struct {
	Name        string
	In          string
	Required    bool
	Description string
	Schema      any
}

type ResponseSpec struct {
	Status      int
	Description string
	Body        any
	// Headers documents response headers that carry part of the contract, such
	// as a cache-generation label a client is expected to act on.
	Headers []HeaderSpec
}

// HeaderSpec describes one documented response header.
type HeaderSpec struct {
	Name        string
	Description string
	Schema      any
}

type DocumentSpec struct {
	Title             string
	Description       string
	OperationIDPrefix string
	Authenticated     bool
	Tags              []openapi3.Tag
	Routes            []RouteSpec
}

func BuildDocument(spec DocumentSpec) (*openapi3.T, error) {
	doc := &openapi3.T{
		OpenAPI: openAPIVersion,
		Info: &openapi3.Info{
			Title:       spec.Title,
			Version:     "1.0.0",
			Description: spec.Description,
		},
		Paths: openapi3.NewPaths(),
		Components: &openapi3.Components{
			SecuritySchemes: openapi3.SecuritySchemes{},
		},
		Servers: openapi3.Servers{
			&openapi3.Server{URL: "/"},
		},
		Tags: toOpenAPITags(spec.Tags),
	}

	if spec.Authenticated {
		doc.Components.SecuritySchemes["bearerAuth"] = &openapi3.SecuritySchemeRef{
			Value: openapi3.NewSecurityScheme().
				WithType("http").
				WithScheme("bearer").
				WithBearerFormat("JWT").
				WithDescription("ZITADEL/OIDC bearer token"),
		}
	}

	for _, route := range spec.Routes {
		if !hasParameter(route.Parameters, "locale", "query") {
			route.Parameters = append([]ParameterSpec{localeParam()}, route.Parameters...)
		}
		op, err := operationFor(route, spec.Authenticated)
		if err != nil {
			return nil, err
		}
		doc.AddOperation(route.Path, route.Method, op)
	}

	if err := doc.Validate(context.Background()); err != nil {
		return nil, err
	}

	return doc, nil
}

func MarshalDocument(doc *openapi3.T) ([]byte, error) {
	if err := doc.Validate(context.Background()); err != nil {
		return nil, err
	}

	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func ValidateDocument(doc *openapi3.T) error {
	return doc.Validate(context.Background())
}

func NewSpecHandler(specBytes []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", jsonMediaType)
		_, _ = w.Write(specBytes)
	}
}

func operationFor(route RouteSpec, authenticated bool) (*openapi3.Operation, error) {
	op := openapi3.NewOperation()
	op.OperationID = route.OperationID
	op.Summary = route.Summary
	op.Description = route.Description
	op.Tags = append([]string(nil), route.Tags...)

	for _, param := range route.Parameters {
		schemaRef, err := schemaRefFor(param.Schema)
		if err != nil {
			return nil, err
		}
		op.AddParameter(&openapi3.Parameter{
			Name:        param.Name,
			In:          param.In,
			Required:    param.Required,
			Description: param.Description,
			Schema:      schemaRef,
		})
	}

	if route.RequestBody != nil {
		schemaRef, err := schemaRefFor(route.RequestBody)
		if err != nil {
			return nil, err
		}
		op.RequestBody = &openapi3.RequestBodyRef{
			Value: &openapi3.RequestBody{
				Required: true,
				Content:  openapi3.NewContentWithJSONSchemaRef(schemaRef),
			},
		}
	}

	op.Responses = openapi3.NewResponses()
	for _, response := range route.Responses {
		resp := openapi3.NewResponse()
		resp.Description = &response.Description
		if response.Body != nil {
			schemaRef, err := schemaRefFor(response.Body)
			if err != nil {
				return nil, err
			}
			resp.Content = openapi3.NewContentWithJSONSchemaRef(schemaRef)
		}
		for _, header := range response.Headers {
			schemaRef, err := schemaRefFor(header.Schema)
			if err != nil {
				return nil, err
			}
			if resp.Headers == nil {
				resp.Headers = openapi3.Headers{}
			}
			resp.Headers[header.Name] = &openapi3.HeaderRef{Value: &openapi3.Header{
				Parameter: openapi3.Parameter{
					Description: header.Description,
					Schema:      schemaRef,
				},
			}}
		}
		op.AddResponse(response.Status, resp)
	}

	if authenticated || route.Auth {
		requirement := openapi3.NewSecurityRequirement().Authenticate("bearerAuth")
		requirements := openapi3.SecurityRequirements{requirement}
		op.Security = &requirements
	}

	return op, nil
}

func schemaRefFor(sample any) (*openapi3.SchemaRef, error) {
	if sample == nil {
		return &openapi3.SchemaRef{Value: openapi3.NewObjectSchema().WithAnyAdditionalProperties()}, nil
	}

	t := reflect.TypeOf(sample)
	return schemaRefForType(t, map[reflect.Type]*openapi3.SchemaRef{})
}

func schemaRefForType(t reflect.Type, cache map[reflect.Type]*openapi3.SchemaRef) (*openapi3.SchemaRef, error) {
	for t.Kind() == reflect.Pointer {
		ref, err := schemaRefForType(t.Elem(), cache)
		if err != nil {
			return nil, err
		}
		clone := *ref.Value
		clone.WithNullable()
		return &openapi3.SchemaRef{Value: &clone}, nil
	}

	if cached, ok := cache[t]; ok {
		return cached, nil
	}

	if t == reflect.TypeOf(time.Time{}) {
		return &openapi3.SchemaRef{Value: openapi3.NewDateTimeSchema()}, nil
	}

	if t.PkgPath() == "core/packages/money" && t.Name() == "Money" {
		schema := openapi3.NewObjectSchema().
			WithProperty("amount_minor", openapi3.NewInt64Schema()).
			WithProperty("currency", openapi3.NewStringSchema()).
			WithRequired([]string{"amount_minor", "currency"})
		return &openapi3.SchemaRef{Value: schema}, nil
	}

	switch t.Kind() {
	case reflect.Bool:
		return &openapi3.SchemaRef{Value: openapi3.NewBoolSchema()}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32:
		schema := openapi3.NewIntegerSchema()
		schema.Format = "int32"
		return &openapi3.SchemaRef{Value: schema}, nil
	case reflect.Int64:
		schema := openapi3.NewIntegerSchema()
		schema.Format = "int64"
		return &openapi3.SchemaRef{Value: schema}, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32:
		schema := openapi3.NewIntegerSchema()
		schema.Format = "int32"
		return &openapi3.SchemaRef{Value: schema}, nil
	case reflect.Uint64:
		schema := openapi3.NewIntegerSchema()
		schema.Format = "int64"
		return &openapi3.SchemaRef{Value: schema}, nil
	case reflect.Float32:
		schema := openapi3.NewFloat64Schema()
		schema.Format = "float"
		return &openapi3.SchemaRef{Value: schema}, nil
	case reflect.Float64:
		return &openapi3.SchemaRef{Value: openapi3.NewFloat64Schema()}, nil
	case reflect.String:
		return &openapi3.SchemaRef{Value: openapi3.NewStringSchema()}, nil
	case reflect.Interface:
		return &openapi3.SchemaRef{Value: openapi3.NewObjectSchema().WithAnyAdditionalProperties()}, nil
	case reflect.Slice, reflect.Array:
		itemSchema, err := schemaRefForType(t.Elem(), cache)
		if err != nil {
			return nil, err
		}
		schema := openapi3.NewArraySchema().WithItems(itemSchema.Value)
		return &openapi3.SchemaRef{Value: schema}, nil
	case reflect.Map:
		if t.Key().Kind() != reflect.String {
			return nil, fmt.Errorf("unsupported map key type %s", t.Key())
		}
		schema := openapi3.NewObjectSchema()
		elemSchema, err := schemaRefForType(t.Elem(), cache)
		if err != nil {
			return nil, err
		}
		if isAnyType(t.Elem()) {
			schema.WithAnyAdditionalProperties()
		} else {
			schema.WithAdditionalProperties(elemSchema.Value)
		}
		return &openapi3.SchemaRef{Value: schema}, nil
	case reflect.Struct:
		schema := openapi3.NewObjectSchema()
		ref := &openapi3.SchemaRef{Value: schema}
		cache[t] = ref

		required := make([]string, 0, t.NumField())
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			if !field.IsExported() {
				continue
			}
			name, omitEmpty, ok := jsonFieldName(field)
			if !ok {
				continue
			}
			fieldRef, err := schemaRefForType(field.Type, cache)
			if err != nil {
				return nil, err
			}
			schema.WithProperty(name, fieldRef.Value)
			if !omitEmpty && field.Type.Kind() != reflect.Pointer {
				required = append(required, name)
			}
		}
		sort.Strings(required)
		schema.WithRequired(required)
		return ref, nil
	default:
		return nil, fmt.Errorf("unsupported schema type %s", t.String())
	}
}

func jsonFieldName(field reflect.StructField) (name string, omitempty bool, ok bool) {
	tag := field.Tag.Get("json")
	if tag == "-" {
		return "", false, false
	}

	if tag == "" {
		return strings.ToLower(field.Name[:1]) + field.Name[1:], false, true
	}

	parts := strings.Split(tag, ",")
	if parts[0] == "" {
		return strings.ToLower(field.Name[:1]) + field.Name[1:], strings.Contains(tag, "omitempty"), true
	}

	return parts[0], strings.Contains(tag, "omitempty"), true
}

func isAnyType(t reflect.Type) bool {
	return t.Kind() == reflect.Interface && t.NumMethod() == 0
}

func hasParameter(parameters []ParameterSpec, name, in string) bool {
	for _, parameter := range parameters {
		if parameter.Name == name && parameter.In == in {
			return true
		}
	}
	return false
}

func localeParam() ParameterSpec {
	return ParameterSpec{
		Name:        "locale",
		In:          "query",
		Required:    false,
		Description: "Override locale resolution",
		Schema:      "",
	}
}

func toOpenAPITags(tags []openapi3.Tag) openapi3.Tags {
	result := make(openapi3.Tags, 0, len(tags))
	for _, tag := range tags {
		tagCopy := tag
		result = append(result, &tagCopy)
	}
	return result
}
