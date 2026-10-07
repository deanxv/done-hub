package image

import (
	"context"
	"done-hub/common/config"
	"done-hub/common/utils"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

var ImageHttpClients = &http.Client{
	Transport: &http.Transport{
		DialContext: utils.NewDialer().DialContext,
	},
	Timeout: 15 * time.Second,
}

// imageProxyClients 按 ChatImageRequestProxy 隔离连接池；该配置可在运行时修改，旧代理 client 闲置后回收。
var imageProxyClients = utils.NewProxyClientPool(func() *http.Client { return ImageHttpClients }, 10*time.Minute)

var maxFileSize int64 = 20 * 1024 * 1024 // 20MB

type CFRequest struct {
	Action string `json:"action"`
	APIKey string `json:"api_key"`
	URL    string `json:"url"`
}

type CFResponse struct {
	Status   bool   `json:"status"`
	Message  string `json:"message,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

func RequestFile(url, action string) (*http.Response, error) {
	reqUrl := url
	method := http.MethodGet
	var requestBody any

	if config.CFWorkerImageUrl != "" {
		requestBody = &CFRequest{
			Action: action,
			APIKey: config.CFWorkerImageKey,
			URL:    url,
		}
		reqUrl = config.CFWorkerImageUrl
		method = http.MethodPost
	}

	res, err := utils.RequestBuilder(context.Background(), method, reqUrl, requestBody, nil)

	if err != nil {
		return nil, err
	}

	response, err := imageProxyClients.Get(config.ChatImageRequestProxy).Do(res)
	if err != nil {
		return nil, err
	}

	response.Body = http.MaxBytesReader(nil, response.Body, maxFileSize)

	if response.StatusCode != http.StatusOK && config.CFWorkerImageUrl != "" {
		var cfResp *CFResponse
		err = json.NewDecoder(response.Body).Decode(&cfResp)
		if err != nil {
			return nil, err
		}
		return nil, errors.New(cfResp.Message)
	}

	return response, err
}
