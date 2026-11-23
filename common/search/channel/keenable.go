package channel

import (
	"done-hub/common/requester"
	"done-hub/common/search/search_type"
	"done-hub/types"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

const keenableDefaultBaseURL = "https://api.keenable.ai"

// Keenable searches Keenable's own web index through its keyed API.
// API reference: https://docs.keenable.ai/api-reference/search
type Keenable struct {
	apiKey  string
	baseURL string
}

type KeenableRequest struct {
	Query string `json:"query"`
}

type KeenableResponse struct {
	Results []*KeenableResult `json:"results"`
}

type KeenableResult struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"`
	Snippet     string `json:"snippet"`
}

type KeenableErr struct {
	Detail any    `json:"detail"`
	Error  string `json:"error"`
}

func NewKeenable(apiKey, baseURL string) *Keenable {
	if baseURL == "" {
		baseURL = keenableDefaultBaseURL
	}
	return &Keenable{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
	}
}

func (k *Keenable) Name() string {
	return "Keenable"
}

func (k *Keenable) Query(query string) (*search_type.SearchResponses, error) {
	client := requester.NewHTTPRequester("", keenableErrFunc)
	client.IsOpenAI = false

	headers := requester.GetJsonHeaders()
	headers["X-API-Key"] = k.apiKey

	req, err := client.NewRequest(http.MethodPost, k.baseURL+"/v1/search", client.WithHeader(headers), client.WithBody(&KeenableRequest{Query: query}))
	if err != nil {
		return nil, err
	}

	var resp KeenableResponse
	_, opErr := client.SendRequest(req, &resp, false)
	if opErr != nil {
		return nil, opErr
	}

	responses := &search_type.SearchResponses{}
	for _, result := range resp.Results {
		if result == nil || result.URL == "" {
			continue
		}
		content := result.Description
		if content == "" {
			content = result.Snippet
		}
		responses.Results = append(responses.Results, search_type.SearchResult{
			Title:   result.Title,
			Content: content,
			Url:     result.URL,
		})
	}

	return responses, nil
}

func keenableErrFunc(resp *http.Response) *types.OpenAIError {
	respMsg := &KeenableErr{}

	err := json.NewDecoder(resp.Body).Decode(respMsg)
	if err != nil {
		return nil
	}

	msg := respMsg.Error
	if msg == "" && respMsg.Detail != nil {
		msg = fmt.Sprint(respMsg.Detail)
	}
	if msg == "" {
		return nil
	}

	return &types.OpenAIError{
		Message: fmt.Sprintf("query keenable err. err msg: %s", msg),
		Type:    "keenable_error",
	}
}
