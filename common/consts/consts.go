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
 */

package consts

const (
	HttpMethodGet     = "GET"
	HttpMethodPost    = "POST"
	HttpMethodPut     = "PUT"
	HttpMethodPatch   = "PATCH"
	HttpMethodDelete  = "DELETE"
	HttpMethodOptions = "OPTIONS"
	HttpMethodHead    = "HEAD"
)

const (
	ApiGet           = "api.get"
	ApiPost          = "api.post"
	ApiPut           = "api.put"
	ApiPatch         = "api.patch"
	ApiDelete        = "api.delete"
	ApiOptions       = "api.options"
	ApiHEAD          = "api.head"
	ApiAny           = "api.any"
	ApiQuery         = "api.query"
	ApiForm          = "api.form"
	ApiPath          = "api.path"
	ApiHeader        = "api.header"
	ApiCookie        = "api.cookie"
	ApiBody          = "api.body"
	ApiRawBody       = "api.raw_body"
	ApiBaseDomain    = "api.base_domain"
	ApiBaseURL       = "api.baseurl"
	OpenapiOperation = "openapi.operation"
	OpenapiProperty  = "openapi.property"
	OpenapiSchema    = "openapi.schema"
	OpenapiParameter = "openapi.parameter"
	OpenapiDocument  = "openapi.document"
)

const (
	CodeGenerationCommentPbHttp     = "// Code generated by protoc-gen-http-swagger."
	CodeGenerationCommentPbRpc      = "// Code generated by protoc-gen-rpc-swagger."
	CodeGenerationCommentThriftHttp = "// Code generated by thrift-gen-http-swagger."
	CodeGenerationCommentThriftRpc  = "// Code generated by thrift-gen-rpc-swagger."
)

const (
	PluginNameProtocHttpSwagger = "protoc-gen-http-swagger"
	PluginNameProtocRpcSwagger  = "protoc-gen-rpc-swagger"
	PluginNameThriftHttpSwagger = "thrift-gen-http-swagger"
	PluginNameThriftRpcSwagger  = "thrift-gen-rpc-swagger"
)

const (
	OpenAPIVersion        = "3.0.3"
	InfoURL               = "https://github.com/hertz-contrib/swagger-generate/"
	URLDefaultPrefixHTTP  = "http://"
	URLDefaultPrefixHTTPS = "https://"
	DefaultInfoTitle      = "API generated by "
	DefaultInfoDesc       = "API description"
	DefaultInfoVersion    = "0.0.1"

	DocumentOptionServiceType = "service"
	DocumentOptionStructType  = "struct"

	DefaultResponseDesc          = "Successful response"
	DefaultExceptionDesc         = "Exception response"
	StatusOK                     = "200"
	StatusBadRequest             = "400"
	SchemaObjectType             = "object"
	ComponentSchemaPrefix        = "#/components/schemas/"
	ComponentSchemaSuffixBody    = "Body"
	ComponentSchemaSuffixForm    = "Form"
	ComponentSchemaSuffixRawBody = "RawBody"

	ContentTypeJSON           = "application/json"
	ContentTypeFormMultipart  = "multipart/form-data"
	ContentTypeFormURLEncoded = "application/x-www-form-urlencoded"
	ContentTypeRawBody        = "text/plain"

	ParameterInQuery  = "query"
	ParameterInHeader = "header"
	ParameterInPath   = "path"
	ParameterInCookie = "cookie"

	DefaultOutputDir         = "swagger"
	DefaultOutputYamlFile    = "openapi.yaml"
	DefaultOutputSwaggerFile = "swagger.go"

	DefaultServerURL = "http://127.0.0.1:8888"
	DefaultKitexAddr = "127.0.0.1:8888"

	ParameterNameTTHeader = "ttheader"
	ParameterDescription  = "metainfo for request"

	CommentPatternRegexp    = `//\s*(.*)|/\*([\s\S]*?)\*/`
	LinterRulePatternRegexp = `\(-- .* --\)`

	ProtobufValueName = "GoogleProtobufValue"
	ProtobufAnyName   = "GoogleProtobufAny"
)