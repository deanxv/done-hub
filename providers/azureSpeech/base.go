package azureSpeech

import (
	"done-hub/common/requester"
	"done-hub/model"
	"done-hub/providers/base"
	"fmt"
	"strings"
)

// 定义供应商工厂
type AzureSpeechProviderFactory struct{}

// 创建 AzureSpeechProvider
func (f AzureSpeechProviderFactory) Create(channel *model.Channel) base.ProviderInterface {
	return &AzureSpeechProvider{
		BaseProvider: base.BaseProvider{
			Config: base.ProviderConfig{
				AudioSpeech: "/cognitiveservices/v1",
			},
			Channel:   channel,
			Requester: requester.NewHTTPRequester(channel.GetProxy(), nil),
		},
	}
}

type AzureSpeechProvider struct {
	base.BaseProvider
}

func (p *AzureSpeechProvider) GetFullRequestURL(requestURL string) string {
	baseURL := ""
	if p.Channel.Other != "" {
		baseURL = fmt.Sprintf("https://%s.tts.speech.microsoft.com", p.Channel.Other)
	} else {
		baseURL = strings.TrimSuffix(p.GetBaseURL(), "/")
	}

	return fmt.Sprintf("%s%s", baseURL, requestURL)
}

// 获取请求头
func (p *AzureSpeechProvider) GetRequestHeaders() (headers map[string]string) {
	headers = make(map[string]string)
	headers["Ocp-Apim-Subscription-Key"] = p.Channel.Key
	headers["Content-Type"] = "application/ssml+xml"
	headers["User-Agent"] = "OneAPI"
	// headers["X-Microsoft-OutputFormat"] = "audio-16khz-128kbitrate-mono-mp3"

	return headers
}
