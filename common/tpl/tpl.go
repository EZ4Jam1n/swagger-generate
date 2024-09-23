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

package tpl

const ServerTemplateHttp = `package swagger

import (
	"context"
	_ "embed"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/hertz-contrib/cors"
	"github.com/hertz-contrib/swagger"
	swaggerFiles "github.com/swaggo/files"
)

//go:embed openapi.yaml
var openapiYAML []byte

func BindSwagger(h *server.Hertz) {
	h.Use(cors.Default())

	h.GET("/swagger/*any", swagger.WrapHandler(
		swaggerFiles.Handler,
		swagger.URL("/openapi.yaml"),
	))

	h.GET("/openapi.yaml", func(c context.Context, ctx *app.RequestContext) {
		ctx.Header("Content-Type", "application/x-yaml")
		ctx.Write(openapiYAML)
	})
}
`

const ServerTemplateRpc = `package swagger

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bytedance/gopkg/cloud/metainfo"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/cloudwego/hertz/pkg/network"
	"github.com/cloudwego/hertz/pkg/route"
	"github.com/cloudwego/kitex/client"
	"github.com/cloudwego/kitex/client/genericclient"
	"github.com/cloudwego/kitex/pkg/endpoint"
	"github.com/cloudwego/kitex/pkg/generic"
	"github.com/cloudwego/kitex/pkg/klog"
	"github.com/cloudwego/kitex/pkg/remote"
	"github.com/cloudwego/kitex/pkg/remote/trans/detection"
	"github.com/cloudwego/kitex/pkg/remote/trans/netpoll"
	"github.com/cloudwego/kitex/pkg/remote/trans/nphttp2"
	"github.com/cloudwego/kitex/pkg/transmeta"
	"github.com/cloudwego/kitex/transport"
	"github.com/hertz-contrib/cors"
	"github.com/hertz-contrib/swagger"
	swaggerFiles "github.com/swaggo/files"
)

var (
	//go:embed openapi.yaml
	openapiYAML []byte
	hertzEngine *route.Engine
	httpReg     = regexp.MustCompile("^(?:GET |POST|PUT|DELE|HEAD|OPTI|CONN|TRAC|PATC)$")
)

const (
	kitexAddr = "{{.KitexAddr}}"
	idlFile   = "{{.IdlPath}}"
)

type MixTransHandlerFactory struct {
	OriginFactory remote.ServerTransHandlerFactory
}

type transHandler struct {
	remote.ServerTransHandler
}

func (t *transHandler) SetInvokeHandleFunc(inkHdlFunc endpoint.Endpoint) {
	t.ServerTransHandler.(remote.InvokeHandleFuncSetter).SetInvokeHandleFunc(inkHdlFunc)
}

func (m MixTransHandlerFactory) NewTransHandler(opt *remote.ServerOption) (remote.ServerTransHandler, error) {

	if hertzEngine == nil {
		StartServer()
	}

	var kitexOrigin remote.ServerTransHandler
	var err error

	if m.OriginFactory != nil {
		kitexOrigin, err = m.OriginFactory.NewTransHandler(opt)
	} else {
		kitexOrigin, err = detection.NewSvrTransHandlerFactory(netpoll.NewSvrTransHandlerFactory(), nphttp2.NewSvrTransHandlerFactory()).NewTransHandler(opt)
	}
	if err != nil {
		return nil, err
	}
	return &transHandler{ServerTransHandler: kitexOrigin}, nil
}

func (t *transHandler) OnRead(ctx context.Context, conn net.Conn) error {
	c, ok := conn.(network.Conn)
	if ok {
		pre, _ := c.Peek(4)
		if httpReg.Match(pre) {
			klog.Info("using Hertz to process request")
			err := hertzEngine.Serve(ctx, c)
			if err != nil {
				err = errors.New(fmt.Sprintf("HERTZ: %s", err.Error()))
			}
			return err
		}
	}

	return t.ServerTransHandler.OnRead(ctx, conn)
}

func StartServer() {
	h := server.Default()
	h.Use(cors.Default())

	cli := initializeGenericClient()
	setupSwaggerRoutes(h)
	setupProxyRoutes(h, cli)

	hlog.Info("Swagger UI is available at: http://" + kitexAddr + "/swagger/index.html")
	err := h.Engine.Init()
	if err != nil {
		panic(err)
	}

	hertzEngine = h.Engine
}

func findThriftFile(fileName string) (string, error) {
	workingDir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	foundPath := ""
	relativePath := fileName

	err = filepath.Walk(workingDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() {
			relative, err := filepath.Rel(workingDir, path)
			if err != nil {
				return err
			}

			if relative == relativePath {
				foundPath = path
				return filepath.SkipDir
			}
		}
		return nil
	})

	if err == nil && foundPath != "" {
		return foundPath, nil
	}

	parentDir := filepath.Dir(workingDir)
	for parentDir != "/" && parentDir != "." && parentDir != workingDir {
		filePath := filepath.Join(parentDir, fileName)
		if _, err := os.Stat(filePath); err == nil {
			return filePath, nil
		}
		workingDir = parentDir
		parentDir = filepath.Dir(parentDir)
	}

	return "", errors.New("thrift file not found: " + fileName)
}

func initializeGenericClient() genericclient.Client {
	thriftFile, err := findThriftFile(idlFile)
	if err != nil {
		hlog.Fatal("Failed to locate Thrift file:", err)
	}

	p, err := generic.NewThriftFileProviderWithDynamicGo(thriftFile)
	if err != nil {
		hlog.Fatal("Failed to create ThriftFileProvider:", err)
	}

	g, err := generic.JSONThriftGeneric(p)
	if err != nil {
		hlog.Fatal("Failed to create JsonThriftGeneric:", err)
	}
	var opts []client.Option
	opts = append(opts, client.WithTransportProtocol(transport.TTHeader))
	opts = append(opts, client.WithMetaHandler(transmeta.ClientTTHeaderHandler))
	opts = append(opts, client.WithHostPorts(kitexAddr))
	cli, err := genericclient.NewClient("swagger", g, opts...)
	if err != nil {
		hlog.Fatal("Failed to create generic client:", err)
	}

	return cli
}

func setupSwaggerRoutes(h *server.Hertz) {
	h.GET("swagger/*any", swagger.WrapHandler(swaggerFiles.Handler, swagger.URL("/openapi.yaml")))

	h.GET("/openapi.yaml", func(c context.Context, ctx *app.RequestContext) {
		ctx.Header("Content-Type", "application/x-yaml")
		ctx.Write(openapiYAML)
	})
}

func setupProxyRoutes(h *server.Hertz, cli genericclient.Client) {
	h.Any("/*ServiceMethod", func(c context.Context, ctx *app.RequestContext) {
		serviceMethod := ctx.Param("ServiceMethod")
		if serviceMethod == "" {
			handleError(ctx, "ServiceMethod not provided", http.StatusBadRequest)
			return
		}

		bodyBytes := ctx.Request.Body()

		queryMap := formatQueryParams(ctx)

		for k, v := range queryMap {
			if strings.HasPrefix(k, "p_") {
				c = metainfo.WithPersistentValue(c, k, v)
			} else {
				c = metainfo.WithValue(c, k, v)
			}
		}

		c = metainfo.WithBackwardValues(c)

		jReq := string(bodyBytes)

		jRsp, err := cli.GenericCall(c, serviceMethod, jReq)
		if err != nil {
			hlog.Errorf("GenericCall error: %v", err)
			ctx.JSON(500, map[string]interface{}{
				"error": err.Error(),
			})
			return
		}

		result := make(map[string]interface{})
		if err := json.Unmarshal([]byte(jRsp.(string)), &result); err != nil {
			hlog.Errorf("Failed to unmarshal response body: %v", err)
			ctx.JSON(500, map[string]interface{}{
				"error": "Failed to unmarshal response body",
			})
			return
		}

		m := metainfo.RecvAllBackwardValues(c)

		for key, value := range m {
			result[key] = value
		}

		respBody, err := json.Marshal(result)
		if err != nil {
			hlog.Errorf("Failed to marshal response body: %v", err)
			ctx.JSON(500, map[string]interface{}{
				"error": "Failed to marshal response body",
			})
			return
		}

		ctx.Data(http.StatusOK, "application/json", respBody)

	})
}

func formatQueryParams(ctx *app.RequestContext) map[string]string {
	var QueryParams = make(map[string]string)
	ctx.Request.URI().QueryArgs().VisitAll(func(key, value []byte) {
		QueryParams[string(key)] = string(value)
	})
	return QueryParams
}

func handleError(ctx *app.RequestContext, errMsg string, statusCode int) {
	hlog.Errorf("Error: %s", errMsg)
	ctx.JSON(statusCode, map[string]interface{}{
		"error": errMsg,
	})
}
`

const ServerTemplateRpcPb = `package swagger

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bytedance/gopkg/cloud/metainfo"
	"github.com/cloudwego/dynamicgo/proto"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/cloudwego/hertz/pkg/network"
	"github.com/cloudwego/hertz/pkg/route"
	"github.com/cloudwego/kitex/client"
	"github.com/cloudwego/kitex/client/genericclient"
	"github.com/cloudwego/kitex/pkg/endpoint"
	"github.com/cloudwego/kitex/pkg/generic"
	"github.com/cloudwego/kitex/pkg/klog"
	"github.com/cloudwego/kitex/pkg/remote"
	"github.com/cloudwego/kitex/pkg/remote/trans/detection"
	"github.com/cloudwego/kitex/pkg/remote/trans/netpoll"
	"github.com/cloudwego/kitex/pkg/remote/trans/nphttp2"
	"github.com/cloudwego/kitex/pkg/transmeta"
	"github.com/cloudwego/kitex/transport"
	"github.com/hertz-contrib/cors"
	"github.com/hertz-contrib/swagger"
	swaggerFiles "github.com/swaggo/files"
)

var (
	//go:embed openapi.yaml
	openapiYAML []byte
	hertzEngine *route.Engine
	httpReg     = regexp.MustCompile("^(?:GET |POST|PUT|DELE|HEAD|OPTI|CONN|TRAC|PATC)$")
)

const (
	kitexAddr = "{{.KitexAddr}}"
	idlFile   = "{{.IdlPath}}"
)

type MixTransHandlerFactory struct {
	OriginFactory remote.ServerTransHandlerFactory
}

type transHandler struct {
	remote.ServerTransHandler
}

func (t *transHandler) SetInvokeHandleFunc(inkHdlFunc endpoint.Endpoint) {
	t.ServerTransHandler.(remote.InvokeHandleFuncSetter).SetInvokeHandleFunc(inkHdlFunc)
}

func (m MixTransHandlerFactory) NewTransHandler(opt *remote.ServerOption) (remote.ServerTransHandler, error) {

	if hertzEngine == nil {
		StartServer()
	}

	var kitexOrigin remote.ServerTransHandler
	var err error

	if m.OriginFactory != nil {
		kitexOrigin, err = m.OriginFactory.NewTransHandler(opt)
	} else {
		kitexOrigin, err = detection.NewSvrTransHandlerFactory(netpoll.NewSvrTransHandlerFactory(), nphttp2.NewSvrTransHandlerFactory()).NewTransHandler(opt)
	}
	if err != nil {
		return nil, err
	}
	return &transHandler{ServerTransHandler: kitexOrigin}, nil
}

func (t *transHandler) OnRead(ctx context.Context, conn net.Conn) error {
	c, ok := conn.(network.Conn)
	if ok {
		pre, _ := c.Peek(4)
		if httpReg.Match(pre) {
			klog.Info("using Hertz to process request")
			err := hertzEngine.Serve(ctx, c)
			if err != nil {
				err = errors.New(fmt.Sprintf("HERTZ: %s", err.Error()))
			}
			return err
		}
	}

	return t.ServerTransHandler.OnRead(ctx, conn)
}

func StartServer() {
	h := server.Default()
	h.Use(cors.Default())

	cli := initializeGenericClient()
	setupSwaggerRoutes(h)
	setupProxyRoutes(h, cli)

	hlog.Info("Swagger UI is available at: http://" + kitexAddr + "/swagger/index.html")
	err := h.Engine.Init()
	if err != nil {
		panic(err)
	}

	hertzEngine = h.Engine
}

func findPbFile(fileName string) (string, error) {
	workingDir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	foundPath := ""
	relativePath := fileName

	err = filepath.Walk(workingDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() {
			relative, err := filepath.Rel(workingDir, path)
			if err != nil {
				return err
			}

			if relative == relativePath {
				foundPath = path
				return filepath.SkipDir
			}
		}
		return nil
	})

	if err == nil && foundPath != "" {
		return foundPath, nil
	}

	parentDir := filepath.Dir(workingDir)
	for parentDir != "/" && parentDir != "." && parentDir != workingDir {
		filePath := filepath.Join(parentDir, fileName)
		if _, err := os.Stat(filePath); err == nil {
			return filePath, nil
		}
		workingDir = parentDir
		parentDir = filepath.Dir(parentDir)
	}

	return "", errors.New("proto file not found: " + fileName)
}

func initializeGenericClient() genericclient.Client {
	pbFile, err := findPbFile(idlFile)
	if err != nil {
		hlog.Fatal("Failed to locate Proto file:", err)
	}

	dOpts := proto.Options{}
	p, err := generic.NewPbFileProviderWithDynamicGo(pbFile, context.Background(), dOpts)
	if err != nil {
		hlog.Fatal("Failed to create PbFileProvider:", err)
	}

	g, err := generic.JSONPbGeneric(p)
	if err != nil {
		hlog.Fatal("Failed to create JsonPbGeneric:", err)
	}
	var opts []client.Option
	opts = append(opts, client.WithTransportProtocol(transport.TTHeader))
	opts = append(opts, client.WithMetaHandler(transmeta.ClientTTHeaderHandler))
	opts = append(opts, client.WithHostPorts(kitexAddr))
	cli, err := genericclient.NewClient("swagger", g, opts...)
	if err != nil {
		hlog.Fatal("Failed to create generic client:", err)
	}

	return cli
}

func setupSwaggerRoutes(h *server.Hertz) {
	h.GET("swagger/*any", swagger.WrapHandler(swaggerFiles.Handler, swagger.URL("/openapi.yaml")))

	h.GET("/openapi.yaml", func(c context.Context, ctx *app.RequestContext) {
		ctx.Header("Content-Type", "application/x-yaml")
		ctx.Write(openapiYAML)
	})
}

func setupProxyRoutes(h *server.Hertz, cli genericclient.Client) {
	h.Any("/*ServiceMethod", func(c context.Context, ctx *app.RequestContext) {
		serviceMethod := ctx.Param("ServiceMethod")
		if serviceMethod == "" {
			handleError(ctx, "ServiceMethod not provided", http.StatusBadRequest)
			return
		}

		bodyBytes := ctx.Request.Body()

		queryMap := formatQueryParams(ctx)

		for k, v := range queryMap {
			if strings.HasPrefix(k, "p_") {
				c = metainfo.WithPersistentValue(c, k, v)
			} else {
				c = metainfo.WithValue(c, k, v)
			}
		}

		c = metainfo.WithBackwardValues(c)

		jReq := string(bodyBytes)

		jRsp, err := cli.GenericCall(c, serviceMethod, jReq)
		if err != nil {
			hlog.Errorf("GenericCall error: %v", err)
			ctx.JSON(500, map[string]interface{}{
				"error": err.Error(),
			})
			return
		}

		result := make(map[string]interface{})
		if err := json.Unmarshal([]byte(jRsp.(string)), &result); err != nil {
			hlog.Errorf("Failed to unmarshal response body: %v", err)
			ctx.JSON(500, map[string]interface{}{
				"error": "Failed to unmarshal response body",
			})
			return
		}

		m := metainfo.RecvAllBackwardValues(c)

		for key, value := range m {
			result[key] = value
		}

		respBody, err := json.Marshal(result)
		if err != nil {
			hlog.Errorf("Failed to marshal response body: %v", err)
			ctx.JSON(500, map[string]interface{}{
				"error": "Failed to marshal response body",
			})
			return
		}

		ctx.Data(http.StatusOK, "application/json", respBody)

	})
}

func formatQueryParams(ctx *app.RequestContext) map[string]string {
	var QueryParams = make(map[string]string)
	ctx.Request.URI().QueryArgs().VisitAll(func(key, value []byte) {
		QueryParams[string(key)] = string(value)
	})
	return QueryParams
}

func handleError(ctx *app.RequestContext, errMsg string, statusCode int) {
	hlog.Errorf("Error: %s", errMsg)
	ctx.JSON(statusCode, map[string]interface{}{
		"error": errMsg,
	})
}
`
