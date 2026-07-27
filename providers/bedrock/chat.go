package bedrock

import (
	"bytes"
	"done-hub/common"
	"done-hub/common/requester"
	"done-hub/providers/bedrock/category"
	"done-hub/types"
	"encoding/json"
	"io"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
)

func (p *BedrockProvider) CreateChatCompletion(request *types.ChatCompletionRequest) (*types.ChatCompletionResponse, *types.OpenAIErrorWithStatusCode) {
	request.OneOtherArg = p.GetOtherArg()

	body, modelID, errWithCode := p.buildChatBody(request)
	if errWithCode != nil {
		return nil, errWithCode
	}

	client, err := p.getAwsClient()
	if err != nil {
		return nil, common.ErrorWrapper(err, "aws_client_error", http.StatusInternalServerError)
	}

	out, err := client.InvokeModel(p.invokeContext(), &bedrockruntime.InvokeModelInput{
		ModelId:     aws.String(modelID),
		Accept:      aws.String(awsAccept),
		ContentType: aws.String(awsContentType),
		Body:        body,
	})
	if err != nil {
		return nil, awsErrorToOpenAI(err)
	}

	// Category.ResponseChatComplete 只读 response.Body，用上游原始字节合成一个最小 *http.Response 复用之。
	response := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(out.Body)),
	}
	defer response.Body.Close()

	return p.Category.ResponseChatComplete(p, response, request)
}

func (p *BedrockProvider) CreateChatCompletionStream(request *types.ChatCompletionRequest) (requester.StreamReaderInterface[string], *types.OpenAIErrorWithStatusCode) {
	request.OneOtherArg = p.GetOtherArg()

	body, modelID, errWithCode := p.buildChatBody(request)
	if errWithCode != nil {
		return nil, errWithCode
	}

	client, err := p.getAwsClient()
	if err != nil {
		return nil, common.ErrorWrapper(err, "aws_client_error", http.StatusInternalServerError)
	}

	out, err := client.InvokeModelWithResponseStream(p.invokeContext(), &bedrockruntime.InvokeModelWithResponseStreamInput{
		ModelId:     aws.String(modelID),
		Accept:      aws.String(awsAccept),
		ContentType: aws.String(awsContentType),
		Body:        body,
	})
	if err != nil {
		return nil, awsErrorToOpenAI(err)
	}

	return newAWSStreamReader(out.GetStream(), p.Category.ResponseChatCompleteStrem(p, request)), nil
}

// buildChatBody 构造 OpenAI 形态请求转 Bedrock 的请求体字节，并返回目标 AWS modelId。
func (p *BedrockProvider) buildChatBody(request *types.ChatCompletionRequest) ([]byte, string, *types.OpenAIErrorWithStatusCode) {
	var err error
	p.Category, err = category.GetCategory(request.Model, p.Region)
	if err != nil || p.Category == nil || p.Category.ChatComplete == nil || p.Category.ResponseChatComplete == nil {
		return nil, "", common.StringErrorWrapperLocal("bedrock provider not found", "bedrock_err", http.StatusInternalServerError)
	}

	bedrockRequest, errWithCode := p.Category.ChatComplete(request)
	if errWithCode != nil {
		return nil, "", errWithCode
	}

	body, err := json.Marshal(bedrockRequest)
	if err != nil {
		return nil, "", common.ErrorWrapper(err, "marshal_request_failed", http.StatusInternalServerError)
	}

	return body, p.Category.ModelName, nil
}
