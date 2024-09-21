/*
 * Copyright 2024 CloudWeGo Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 * Copyright 2020 Google LLC. All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *    http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 * This file may have been modified by CloudWeGo authors. All CloudWeGo
 * Modifications are Copyright 2024 CloudWeGo Authors.
 */

package generator

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/cloudwego/hertz/cmd/hz/util/logs"
	"github.com/hertz-contrib/swagger-generate/common/consts"
	common "github.com/hertz-contrib/swagger-generate/common/utils"
	"github.com/hertz-contrib/swagger-generate/idl/protobuf/api"
	"github.com/hertz-contrib/swagger-generate/idl/protobuf/openapi"
	wk "github.com/hertz-contrib/swagger-generate/protoc-gen-rpc-swagger/generator/wellknown"
	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	any_pb "google.golang.org/protobuf/types/known/anypb"
)

type Configuration struct {
	Version        *string
	Title          *string
	Description    *string
	Naming         *string
	FQSchemaNaming *bool
	EnumType       *string
	OutputMode     *string
}

// In order to dynamically add google.rpc.Status responses we need
// to know the message descriptors for google.rpc.Status as well
// as google.protobuf.Any.
var (
	anyProtoDesc = (&any_pb.Any{}).ProtoReflect().Descriptor()
)

// OpenAPIGenerator holds internal state needed to generate an OpenAPIv3 document for a transcoded Protocol Buffer service.
type OpenAPIGenerator struct {
	conf              Configuration
	plugin            *protogen.Plugin
	inputFiles        []*protogen.File
	reflect           *OpenAPIReflector
	generatedSchemas  []string // Names of schemas that have already been generated.
	linterRulePattern *regexp.Regexp
}

// NewOpenAPIGenerator creates a new generator for a protoc plugin invocation.
func NewOpenAPIGenerator(plugin *protogen.Plugin, conf Configuration, inputFiles []*protogen.File) *OpenAPIGenerator {
	return &OpenAPIGenerator{
		conf:              conf,
		plugin:            plugin,
		inputFiles:        inputFiles,
		reflect:           NewOpenAPIReflector(conf),
		generatedSchemas:  make([]string, 0),
		linterRulePattern: regexp.MustCompile(`\(-- .* --\)`),
	}
}

// Run runs the generator.
func (g *OpenAPIGenerator) Run(outputFile *protogen.GeneratedFile) error {
	d := g.buildDocument()
	bytes, err := d.YAMLValue("Generated with " + consts.PluginNameProtocRpcSwagger + "\n" + consts.InfoURL + consts.PluginNameProtocRpcSwagger)
	if err != nil {
		return fmt.Errorf("failed to marshal yaml: %s", err.Error())
	}
	if _, err = outputFile.Write(bytes); err != nil {
		return fmt.Errorf("failed to write yaml: %s", err.Error())
	}
	return nil
}

// buildDocument builds an OpenAPIv3 document for a plugin request.
func (g *OpenAPIGenerator) buildDocument() *openapi.Document {
	d := &openapi.Document{}

	d.Openapi = consts.OpenAPIVersion
	d.Info = &openapi.Info{
		Version:     *g.conf.Version,
		Title:       *g.conf.Title,
		Description: *g.conf.Description,
	}

	d.Paths = &openapi.Paths{}
	d.Components = &openapi.Components{
		Schemas: &openapi.SchemasOrReferences{
			AdditionalProperties: []*openapi.NamedSchemaOrReference{},
		},
	}

	// Go through the files and add the services to the documents, keeping
	// track of which schemas are referenced in the response so we can
	// add them later.
	for _, file := range g.inputFiles {
		if file.Generate {
			// Merge any `Document` annotations with the current
			extDocument := proto.GetExtension(file.Desc.Options(), openapi.E_Document)
			if extDocument != nil {
				if doc, ok := extDocument.(*openapi.Document); ok {
					proto.Merge(d, doc)
				} else {
					logs.Errorf("unexpected type for Document: %T", extDocument)
				}
			}
			g.addPathsToDocument(d, file.Services)
		}
	}

	// While we have required schemas left to generate, go through the files again
	// looking for the related message and adding them to the document if required.
	for len(g.reflect.requiredSchemas) > 0 {
		count := len(g.reflect.requiredSchemas)
		for _, file := range g.plugin.Files {
			g.addSchemasForMessagesToDocument(d, file.Messages)
		}
		g.reflect.requiredSchemas = g.reflect.requiredSchemas[count:len(g.reflect.requiredSchemas)]
	}

	// If there is only 1 service, then use it's title for the
	// document, if the document is missing it.
	if len(d.Tags) == 1 {
		if d.Info.Title == "" && d.Tags[0].Name != "" {
			d.Info.Title = d.Tags[0].Name + " API"
		}
		if d.Info.Description == "" {
			d.Info.Description = d.Tags[0].Description
		}
		d.Tags[0].Description = ""
	}

	var allServers []string

	// If paths methods has servers, but they're all the same, then move servers to path level
	for _, path := range d.Paths.Path {
		var servers []string
		if path.Value.Post != nil && len(path.Value.Post.Servers) == 1 {
			servers = common.AppendUnique(servers, path.Value.Post.Servers[0].Url)
			allServers = common.AppendUnique(allServers, path.Value.Post.Servers[0].Url)
		}

		if len(servers) == 1 {
			path.Value.Servers = []*openapi.Server{{Url: servers[0]}}
			if path.Value.Post != nil {
				path.Value.Post.Servers = nil
			}
		}
	}

	// Set all servers on API level
	if len(allServers) > 0 {
		d.Servers = []*openapi.Server{}
		for _, server := range allServers {
			d.Servers = append(d.Servers, &openapi.Server{Url: server})
		}
	}

	// If there is only 1 server, we can safely remove all path level servers
	if len(allServers) == 1 {
		for _, path := range d.Paths.Path {
			path.Value.Servers = nil
		}
	}

	// If there are no servers, add a default one
	if len(allServers) == 0 {
		d.Servers = []*openapi.Server{
			{Url: consts.DefaultServerURL},
		}
	}

	// Sort the tags.
	{
		pairs := d.Tags
		sort.Slice(pairs, func(i, j int) bool {
			return pairs[i].Name < pairs[j].Name
		})
		d.Tags = pairs
	}
	// Sort the paths.
	{
		pairs := d.Paths.Path
		sort.Slice(pairs, func(i, j int) bool {
			return pairs[i].Name < pairs[j].Name
		})
		d.Paths.Path = pairs
	}
	// Sort the schemas.
	{
		pairs := d.Components.Schemas.AdditionalProperties
		sort.Slice(pairs, func(i, j int) bool {
			return pairs[i].Name < pairs[j].Name
		})
		d.Components.Schemas.AdditionalProperties = pairs
	}
	return d
}

// filterCommentString removes linter rules from comments.
func (g *OpenAPIGenerator) filterCommentString(c protogen.Comments) string {
	comment := g.linterRulePattern.ReplaceAllString(string(c), "")
	return strings.TrimSpace(comment)
}

func (g *OpenAPIGenerator) getSchemaByOption(inputMessage *protogen.Message) *openapi.Schema {
	// Build an array holding the fields of the message.
	definitionProperties := &openapi.Properties{
		AdditionalProperties: make([]*openapi.NamedSchemaOrReference, 0),
	}
	// Merge any `Schema` annotations with the current
	extSchema := proto.GetExtension(inputMessage.Desc.Options(), openapi.E_Schema)
	var allRequired []string
	if extSchema != nil {
		if schema, ok := extSchema.(*openapi.Schema); ok && schema != nil {
			if len(schema.Required) != 0 {
				allRequired = schema.Required
			}
		}
	}
	var required []string
	for _, field := range inputMessage.Fields {
		extName := g.reflect.formatFieldName(field.Desc)
		if common.Contains(allRequired, extName) {
			required = append(required, extName)
		}
		// Get the field description from the comments.
		description := g.filterCommentString(field.Comments.Leading)
		// Check the field annotations to see if this is a readonly or writeonly field.
		inputOnly := false
		outputOnly := false
		extension := proto.GetExtension(field.Desc.Options(), annotations.E_FieldBehavior)
		if extension != nil {
			switch v := extension.(type) {
			case []annotations.FieldBehavior:
				for _, vv := range v {
					switch vv {
					case annotations.FieldBehavior_OUTPUT_ONLY:
						outputOnly = true
					case annotations.FieldBehavior_INPUT_ONLY:
						inputOnly = true
					case annotations.FieldBehavior_REQUIRED:
						required = append(required, g.reflect.formatFieldName(field.Desc))
					}
				}
			default:
				logs.Errorf("unsupported extension type %T", extension)
			}
		}

		// The field is either described by a reference or a schema.
		fieldSchema := g.reflect.schemaOrReferenceForField(field.Desc)
		if fieldSchema == nil {
			continue
		}

		// If this field has siblings and is a $ref now, create a new schema use `allOf` to wrap it
		wrapperNeeded := inputOnly || outputOnly || description != ""
		if wrapperNeeded {
			if _, ok := fieldSchema.Oneof.(*openapi.SchemaOrReference_Reference); ok {
				fieldSchema = &openapi.SchemaOrReference{Oneof: &openapi.SchemaOrReference_Schema{Schema: &openapi.Schema{
					AllOf: []*openapi.SchemaOrReference{fieldSchema},
				}}}
			}
		}

		if schema, ok := fieldSchema.Oneof.(*openapi.SchemaOrReference_Schema); ok {
			schema.Schema.Description = description
			schema.Schema.ReadOnly = outputOnly
			schema.Schema.WriteOnly = inputOnly

			// Merge any `Property` annotations with the current
			extProperty := proto.GetExtension(field.Desc.Options(), openapi.E_Property)
			if extProperty != nil {
				proto.Merge(schema.Schema, extProperty.(*openapi.Schema))
			}
		}

		definitionProperties.AdditionalProperties = append(
			definitionProperties.AdditionalProperties,
			&openapi.NamedSchemaOrReference{
				Name:  extName,
				Value: fieldSchema,
			},
		)
	}

	schema := &openapi.Schema{
		Type:       consts.SchemaObjectType,
		Properties: definitionProperties,
	}

	// Merge any `Schema` annotations with the current
	extSchema = proto.GetExtension(inputMessage.Desc.Options(), openapi.E_Schema)
	if extSchema != nil {
		proto.Merge(schema, extSchema.(*openapi.Schema))
	}

	schema.Required = required
	return schema
}

func (g *OpenAPIGenerator) buildOperation(
	d *openapi.Document,
	operationID string,
	tagName string,
	description string,
	defaultHost string,
	path string,
	inputMessage *protogen.Message,
	outputMessage *protogen.Message,
) (*openapi.Operation, string) {
	// Parameters array to hold all parameter objects
	var parameters []*openapi.ParameterOrReference

	fieldSchema := &openapi.SchemaOrReference{
		Oneof: &openapi.SchemaOrReference_Schema{
			Schema: &openapi.Schema{
				Type: consts.SchemaObjectType,
			},
		},
	}
	parameter := &openapi.Parameter{
		Name:        consts.ParameterNameTTHeader,
		In:          consts.ParameterInQuery,
		Description: consts.ParameterDescription,
		Required:    false,
		Schema:      fieldSchema,
	}

	parameters = append(parameters, &openapi.ParameterOrReference{
		Oneof: &openapi.ParameterOrReference_Parameter{
			Parameter: parameter,
		},
	})

	var RequestBody *openapi.RequestBodyOrReference
	var additionalProperties []*openapi.NamedMediaType

	bodySchema := g.getSchemaByOption(inputMessage)

	if len(bodySchema.Properties.AdditionalProperties) > 0 {
		refSchema := &openapi.NamedSchemaOrReference{
			Name:  g.reflect.formatMessageName(inputMessage.Desc),
			Value: &openapi.SchemaOrReference{Oneof: &openapi.SchemaOrReference_Schema{Schema: bodySchema}},
		}

		ref := consts.ComponentSchemaPrefix + g.reflect.formatMessageName(inputMessage.Desc)

		g.addSchemaToDocument(d, refSchema)

		additionalProperties = append(additionalProperties, &openapi.NamedMediaType{
			Name: consts.ContentTypeJSON,
			Value: &openapi.MediaType{
				Schema: &openapi.SchemaOrReference{
					Oneof: &openapi.SchemaOrReference_Reference{
						Reference: &openapi.Reference{XRef: ref},
					},
				},
			},
		})
	}

	if len(additionalProperties) > 0 {
		RequestBody = &openapi.RequestBodyOrReference{
			Oneof: &openapi.RequestBodyOrReference_RequestBody{
				RequestBody: &openapi.RequestBody{
					Description: g.filterCommentString(inputMessage.Comments.Leading),
					Content: &openapi.MediaTypes{
						AdditionalProperties: additionalProperties,
					},
				},
			},
		}
	}

	name, content := g.getResponseForMessage(d, outputMessage)

	desc := g.filterCommentString(outputMessage.Comments.Leading)
	if desc == "" {
		desc = consts.DefaultResponseDesc
	}

	var contentOrEmpty *openapi.MediaTypes
	if len(content.AdditionalProperties) != 0 {
		contentOrEmpty = content
	}
	var responses *openapi.Responses
	if contentOrEmpty != nil {
		responses = &openapi.Responses{
			ResponseOrReference: []*openapi.NamedResponseOrReference{
				{
					Name: name,
					Value: &openapi.ResponseOrReference{
						Oneof: &openapi.ResponseOrReference_Response{
							Response: &openapi.Response{
								Description: desc,
								Content:     contentOrEmpty,
							},
						},
					},
				},
			},
		}
	}

	re := regexp.MustCompile(`:(\w+)`)
	path = re.ReplaceAllString(path, `{$1}`)

	op := &openapi.Operation{
		Tags:        []string{tagName},
		Description: description,
		OperationId: operationID,
		Parameters:  parameters,
		Responses:   responses,
		RequestBody: RequestBody,
	}
	if defaultHost != "" {
		if !strings.HasPrefix(defaultHost, consts.URLDefaultPrefixHTTP) && !strings.HasPrefix(defaultHost, consts.URLDefaultPrefixHTTPS) {
			defaultHost = consts.URLDefaultPrefixHTTP + defaultHost
		}
		op.Servers = append(op.Servers, &openapi.Server{Url: defaultHost})
	}

	return op, path
}

func (g *OpenAPIGenerator) getResponseForMessage(d *openapi.Document, message *protogen.Message) (string, *openapi.MediaTypes) {
	bodySchema := g.getSchemaByOption(message)

	var additionalProperties []*openapi.NamedMediaType

	if len(bodySchema.Properties.AdditionalProperties) > 0 {
		refSchema := &openapi.NamedSchemaOrReference{
			Name:  g.reflect.formatMessageName(message.Desc),
			Value: &openapi.SchemaOrReference{Oneof: &openapi.SchemaOrReference_Schema{Schema: bodySchema}},
		}
		ref := consts.ComponentSchemaPrefix + g.reflect.formatMessageName(message.Desc)
		g.addSchemaToDocument(d, refSchema)
		additionalProperties = append(additionalProperties, &openapi.NamedMediaType{
			Name: consts.ContentTypeJSON,
			Value: &openapi.MediaType{
				Schema: &openapi.SchemaOrReference{
					Oneof: &openapi.SchemaOrReference_Reference{
						Reference: &openapi.Reference{XRef: ref},
					},
				},
			},
		})
	}

	content := &openapi.MediaTypes{
		AdditionalProperties: additionalProperties,
	}

	return consts.StatusOK, content
}

// addOperationToDocument adds an operation to the specified path/method.
func (g *OpenAPIGenerator) addOperationToDocument(d *openapi.Document, op *openapi.Operation, path string) {
	var selectedPathItem *openapi.NamedPathItem
	for _, namedPathItem := range d.Paths.Path {
		if namedPathItem.Name == path {
			selectedPathItem = namedPathItem
			break
		}
	}
	// If we get here, we need to create a path item.
	if selectedPathItem == nil {
		selectedPathItem = &openapi.NamedPathItem{Name: path, Value: &openapi.PathItem{}}
		d.Paths.Path = append(d.Paths.Path, selectedPathItem)
	}
	// Set the operation on the specified method.
	selectedPathItem.Value.Post = op
}

func (g *OpenAPIGenerator) addPathsToDocument(d *openapi.Document, services []*protogen.Service) {
	for _, service := range services {
		annotationsCount := 0

		for _, method := range service.Methods {
			comment := g.filterCommentString(method.Comments.Leading)
			inputMessage := method.Input
			outputMessage := method.Output
			operationID := string(service.Desc.Name()) + "_" + string(method.Desc.Name())
			path := "/" + string(method.Desc.Name())

			annotationsCount++
			var host string
			host = proto.GetExtension(method.Desc.Options(), api.E_Baseurl).(string)

			if host == "" {
				host = proto.GetExtension(service.Desc.Options(), api.E_BaseDomain).(string)
			}
			op, path2 := g.buildOperation(d, operationID, string(service.Desc.Name()), comment, host, path, inputMessage, outputMessage)
			// Merge any `Operation` annotations with the current
			extOperation := proto.GetExtension(method.Desc.Options(), openapi.E_Operation)

			if extOperation != nil {
				proto.Merge(op, extOperation.(*openapi.Operation))
			}
			g.addOperationToDocument(d, op, path2)
		}
		if annotationsCount > 0 {
			comment := g.filterCommentString(service.Comments.Leading)
			d.Tags = append(d.Tags, &openapi.Tag{Name: string(service.Desc.Name()), Description: comment})
		}
	}
}

// addSchemaToDocument adds the schema to the document if required
func (g *OpenAPIGenerator) addSchemaToDocument(d *openapi.Document, schema *openapi.NamedSchemaOrReference) {
	if common.Contains(g.generatedSchemas, schema.Name) {
		return
	}
	g.generatedSchemas = append(g.generatedSchemas, schema.Name)
	d.Components.Schemas.AdditionalProperties = append(d.Components.Schemas.AdditionalProperties, schema)
}

// addSchemasForMessagesToDocument adds info from one file descriptor.
func (g *OpenAPIGenerator) addSchemasForMessagesToDocument(d *openapi.Document, messages []*protogen.Message) {
	// For each message, generate a definition.
	for _, message := range messages {
		if message.Messages != nil {
			g.addSchemasForMessagesToDocument(d, message.Messages)
		}

		schemaName := g.reflect.formatMessageName(message.Desc)

		// Only generate this if we need it and haven't already generated it.
		if !common.Contains(g.reflect.requiredSchemas, schemaName) ||
			common.Contains(g.generatedSchemas, schemaName) {
			continue
		}

		typeName := g.reflect.fullMessageTypeName(message.Desc)
		messageDescription := g.filterCommentString(message.Comments.Leading)

		// `google.protobuf.Value` and `google.protobuf.Any` have special JSON transcoding
		// so we can't just reflect on the message descriptor.
		if typeName == ".google.protobuf.Value" {
			g.addSchemaToDocument(d, wk.NewGoogleProtobufValueSchema(schemaName))
			continue
		} else if typeName == ".google.protobuf.Any" {
			g.addSchemaToDocument(d, wk.NewGoogleProtobufAnySchema(schemaName))
			continue
		} else if typeName == ".google.rpc.Status" {
			anySchemaName := g.reflect.formatMessageName(anyProtoDesc)
			g.addSchemaToDocument(d, wk.NewGoogleProtobufAnySchema(anySchemaName))
			g.addSchemaToDocument(d, wk.NewGoogleRpcStatusSchema(schemaName, anySchemaName))
			continue
		}

		// Build an array holding the fields of the message.
		definitionProperties := &openapi.Properties{
			AdditionalProperties: make([]*openapi.NamedSchemaOrReference, 0),
		}

		var required []string
		for _, field := range message.Fields {
			// Get the field description from the comments.
			description := g.filterCommentString(field.Comments.Leading)
			// Check the field annotations to see if this is a readonly or writeonly field.
			inputOnly := false
			outputOnly := false
			extension := proto.GetExtension(field.Desc.Options(), annotations.E_FieldBehavior)
			if extension != nil {
				switch v := extension.(type) {
				case []annotations.FieldBehavior:
					for _, vv := range v {
						switch vv {
						case annotations.FieldBehavior_OUTPUT_ONLY:
							outputOnly = true
						case annotations.FieldBehavior_INPUT_ONLY:
							inputOnly = true
						case annotations.FieldBehavior_REQUIRED:
							required = append(required, g.reflect.formatFieldName(field.Desc))
						}
					}
				default:
					logs.Errorf("unsupported extension type %T", extension)
				}
			}

			// The field is either described by a reference or a schema.
			fieldSchema := g.reflect.schemaOrReferenceForField(field.Desc)
			if fieldSchema == nil {
				continue
			}

			// If this field has siblings and is a $ref now, create a new schema use `allOf` to wrap it
			wrapperNeeded := inputOnly || outputOnly || description != ""
			if wrapperNeeded {
				if _, ok := fieldSchema.Oneof.(*openapi.SchemaOrReference_Reference); ok {
					fieldSchema = &openapi.SchemaOrReference{Oneof: &openapi.SchemaOrReference_Schema{Schema: &openapi.Schema{
						AllOf: []*openapi.SchemaOrReference{fieldSchema},
					}}}
				}
			}

			if schema, ok := fieldSchema.Oneof.(*openapi.SchemaOrReference_Schema); ok {
				schema.Schema.Description = description
				schema.Schema.ReadOnly = outputOnly
				schema.Schema.WriteOnly = inputOnly

				// Merge any `Property` annotations with the current
				extProperty := proto.GetExtension(field.Desc.Options(), openapi.E_Property)
				if extProperty != nil {
					proto.Merge(schema.Schema, extProperty.(*openapi.Schema))
				}
			}

			name := g.reflect.formatFieldName(field.Desc)

			definitionProperties.AdditionalProperties = append(
				definitionProperties.AdditionalProperties,
				&openapi.NamedSchemaOrReference{
					Name:  name,
					Value: fieldSchema,
				},
			)
		}

		schema := &openapi.Schema{
			Type:        consts.SchemaObjectType,
			Description: messageDescription,
			Properties:  definitionProperties,
			Required:    required,
		}

		// Merge any `Schema` annotations with the current
		extSchema := proto.GetExtension(message.Desc.Options(), openapi.E_Schema)
		if extSchema != nil {
			proto.Merge(schema, extSchema.(*openapi.Schema))
		}

		// Add the schema to the components.schema list.
		g.addSchemaToDocument(d, &openapi.NamedSchemaOrReference{
			Name: schemaName,
			Value: &openapi.SchemaOrReference{
				Oneof: &openapi.SchemaOrReference_Schema{
					Schema: schema,
				},
			},
		})
	}
}
