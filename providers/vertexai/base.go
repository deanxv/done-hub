package vertexai

import (
	"bytes"
	"context"
	"crypto/rand"
	"done-hub/common"
	"done-hub/common/cache"
	"done-hub/common/logger"
	"done-hub/common/requester"
	"done-hub/common/utils"
	"done-hub/model"
	"done-hub/providers/base"
	"done-hub/providers/vertexai/category"
	"done-hub/types"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	credentials "cloud.google.com/go/iam/credentials/apiv1"
	"cloud.google.com/go/iam/credentials/apiv1/credentialspb"
	"golang.org/x/net/proxy"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
)

const TokenCacheKey = "api_token:vertexai"
const defaultScope = "https://www.googleapis.com/auth/cloud-platform"

type VertexAIProviderFactory struct{}

// 创建 VertexAIProvider
func (f VertexAIProviderFactory) Create(channel *model.Channel) base.ProviderInterface {
	proxyAddr := channel.GetProxy()

	vertexAIProvider := &VertexAIProvider{
		BaseProvider: base.BaseProvider{
			Config:    getConfig(),
			Channel:   channel,
			Requester: requester.NewHTTPRequester(proxyAddr, nil),
		},
	}

	getKeyConfig(vertexAIProvider)
	return vertexAIProvider
}

type VertexAIProvider struct {
	base.BaseProvider
	Region    string
	ProjectID string
	Category  *category.Category
}

func getConfig() base.ProviderConfig {
	return base.ProviderConfig{
		BaseURL:           "https://%saiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/google/models/%s:%s",
		ChatCompletions:   "/",
		ImagesGenerations: "/predict",
	}
}

func getKeyConfig(vertexAI *VertexAIProvider) {
	keys := strings.Split(vertexAI.Channel.Other, "|")
	if len(keys) < 2 {
		return
	}

	vertexAI.ProjectID = keys[len(keys)-1]

	regions := keys[:len(keys)-1]
	if len(regions) == 0 {
		return
	}

	randomIndex, err := rand.Int(rand.Reader, big.NewInt(int64(len(regions))))
	if err != nil {
		// 如果随机数生成失败，使用第一个region作为fallback
		logger.SysError("Failed to generate random number for region selection: " + err.Error())
		vertexAI.Region = regions[0]
		return
	}

	vertexAI.Region = regions[randomIndex.Int64()]
}

func (p *VertexAIProvider) GetFullRequestURL(modelName string, other string) string {
	if p.Region == "global" {
		return fmt.Sprintf(p.GetBaseURL(), "", p.ProjectID, p.Region, modelName, other)
	}
	return fmt.Sprintf(p.GetBaseURL(), p.Region+"-", p.ProjectID, p.Region, modelName, other)
}

func (p *VertexAIProvider) GetRequestHeaders() (headers map[string]string) {
	headers, _ = p.getRequestHeadersInternal()
	return headers
}

// getRequestHeadersInternal 内部方法，返回请求头和错误信息
func (p *VertexAIProvider) getRequestHeadersInternal() (headers map[string]string, err error) {
	headers = make(map[string]string)
	p.CommonRequestHeaders(headers)

	token, err := p.GetToken()
	if err != nil {
		logger.SysError("Failed to get token: " + err.Error())
		return nil, err
	}

	headers["Authorization"] = "Bearer " + token
	return headers, nil
}

// handleTokenError 处理token获取失败的错误，检查是否匹配禁用通道关键词
func (p *VertexAIProvider) handleTokenError(err error) *types.OpenAIErrorWithStatusCode {
	errMsg := err.Error()

	// 检查是否匹配禁用通道关键词
	if common.DisableChannelKeywordsInstance.IsContains(errMsg) {
		// 匹配关键词，返回非LocalError，允许重试
		return common.StringErrorWrapper(errMsg, "vertexai_token_error", http.StatusInternalServerError)
	} else {
		// 不匹配关键词，返回LocalError，保持原有行为
		return common.StringErrorWrapperLocal(errMsg, "vertexai_token_error", http.StatusInternalServerError)
	}
}

func (p *VertexAIProvider) GetToken() (string, error) {
	cacheKey := fmt.Sprintf("%s:%s", TokenCacheKey, p.ProjectID)
	token, err := cache.GetCache[string](cacheKey)
	if err != nil {
		logger.SysError("Failed to get token from cache: " + err.Error())
	}

	if token != "" {
		return token, nil
	}

	creds := &Credentials{}
	if err := json.Unmarshal([]byte(p.Channel.Key), creds); err != nil {
		return "", fmt.Errorf("failed to unmarshal credentials: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	proxyAddr := p.Channel.GetProxy()

	client, err := credentials.NewIamCredentialsClient(ctx, option.WithCredentialsJSON([]byte(p.Channel.Key)), option.WithGRPCDialOption(grpc.WithContextDialer(customDialer(proxyAddr))))
	if err != nil {
		return "", fmt.Errorf("failed to create IAM credentials client: %w", err)
	}
	defer client.Close()

	req := &credentialspb.GenerateAccessTokenRequest{
		Name:  fmt.Sprintf("projects/-/serviceAccounts/%s", creds.ClientEmail),
		Scope: []string{defaultScope},
	}

	resp, err := client.GenerateAccessToken(ctx, req)
	if err != nil {
		return "", fmt.Errorf("failed to generate access token: %w", err)
	}

	duration := time.Until(resp.ExpireTime.AsTime()) - 5*time.Minute
	if duration <= 0 {
		duration = 30 * time.Second
	}
	cache.SetCache(cacheKey, resp.AccessToken, duration)

	return resp.AccessToken, nil
}

func RequestErrorHandle(otherErr requester.HttpErrorHandler) requester.HttpErrorHandler {

	return func(resp *http.Response) *types.OpenAIError {
		requestBody, _ := io.ReadAll(resp.Body)
		resp.Body = io.NopCloser(bytes.NewBuffer(requestBody))

		if otherErr != nil {
			err := otherErr(resp)
			if err != nil {
				return err
			}
		}
		vertexaiErrors := &VertexaiErrors{}
		if err := json.Unmarshal(requestBody, vertexaiErrors); err == nil {
			if vertexaiError := vertexaiErrors.Error(); vertexaiError != nil {
				return errorHandle(vertexaiError)
			}
		} else {
			vertexaiError := &VertexaiError{}
			if err := json.Unmarshal(requestBody, vertexaiError); err == nil {
				return errorHandle(vertexaiError)
			}
		}

		return nil
	}
}

func errorHandle(vertexaiError *VertexaiError) *types.OpenAIError {
	if vertexaiError.Error.Message == "" {
		return nil
	}

	logger.SysError(fmt.Sprintf("VertexAI error: %s", utils.TruncateBase64InMessage(vertexaiError.Error.Message)))

	return &types.OpenAIError{
		Message: "VertexAI错误",
		Type:    "gemini_error",
		Param:   vertexaiError.Error.Status,
		Code:    vertexaiError.Error.Code,
	}
}

func customDialer(proxyAddr string) func(context.Context, string) (net.Conn, error) {

	return func(ctx context.Context, addr string) (net.Conn, error) {
		// 创建统一的dialer配置
		dialer := &net.Dialer{
			Timeout:   20 * time.Second,
			KeepAlive: 30 * time.Second,
		}

		// 无代理直接连接
		if proxyAddr == "" {
			return dialer.DialContext(ctx, "tcp", addr)
		}

		proxyURL, err := url.Parse(proxyAddr)
		if err != nil {
			return nil, fmt.Errorf("error parsing proxy address: %w", err)
		}

		// 根据代理类型选择连接方式
		switch proxyURL.Scheme {
		case "http":
			return connectViaHTTPProxy(ctx, proxyURL, addr)
		case "https":
			logger.SysError("Warning: HTTPS proxy not compatible with gRPC, using direct connection")
			return dialer.DialContext(ctx, "tcp", addr)
		case "socks5", "socks5h":
			return connectViaSOCKS5Proxy(ctx, dialer, proxyURL, addr)
		default:
			return nil, fmt.Errorf("unsupported proxy scheme: %s", proxyURL.Scheme)
		}
	}
}

// connectViaHTTPProxy 通过HTTP代理建立连接
func connectViaHTTPProxy(ctx context.Context, proxyURL *url.URL, targetAddr string) (net.Conn, error) {
	dialer := &net.Dialer{
		Timeout:   20 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	proxyConn, err := dialer.DialContext(ctx, "tcp", proxyURL.Host)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to HTTP proxy: %w", err)
	}

	// 发送HTTP CONNECT请求
	connectReq := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", targetAddr, targetAddr)
	if _, err = proxyConn.Write([]byte(connectReq)); err != nil {
		proxyConn.Close()
		return nil, fmt.Errorf("failed to send CONNECT request: %w", err)
	}

	// 读取代理响应
	response := make([]byte, 1024)
	n, err := proxyConn.Read(response)
	if err != nil {
		proxyConn.Close()
		return nil, fmt.Errorf("failed to read proxy response: %w", err)
	}

	responseStr := string(response[:n])
	if !strings.Contains(responseStr, "200 Connection established") && !strings.Contains(responseStr, "200 OK") {
		proxyConn.Close()
		return nil, fmt.Errorf("HTTP proxy CONNECT failed: %s", responseStr)
	}

	return proxyConn, nil
}

// connectViaSOCKS5Proxy 通过SOCKS5代理建立连接
func connectViaSOCKS5Proxy(ctx context.Context, dialer *net.Dialer, proxyURL *url.URL, addr string) (net.Conn, error) {
	dialerProxy, err := proxy.FromURL(proxyURL, dialer)
	if err != nil {
		return nil, fmt.Errorf("failed to create SOCKS5 proxy dialer: %v", err)
	}

	if contextDialer, ok := dialerProxy.(proxy.ContextDialer); ok {
		return contextDialer.DialContext(ctx, "tcp", addr)
	}

	return dialerProxy.Dial("tcp", addr)
}
