package gemini

import (
	"done-hub/types"
)

func StringErrorWrapper(err string, code string, statusCode int, localError bool) *GeminiErrorWithStatusCode {
	geminiError := GeminiErrorResponse{
		ErrorInfo: &GeminiError{
			Code:    500,
			Status:  code,
			Message: err,
		},
	}

	return &GeminiErrorWithStatusCode{
		LocalError:          localError,
		StatusCode:          statusCode,
		GeminiErrorResponse: geminiError,
	}
}

func OpenaiErrToGeminiErr(err *types.OpenAIErrorWithStatusCode) *GeminiErrorWithStatusCode {
	if err == nil {
		return nil
	}

	return &GeminiErrorWithStatusCode{
		LocalError: err.LocalError,
		StatusCode: err.StatusCode,
		GeminiErrorResponse: GeminiErrorResponse{
			ErrorInfo: &GeminiError{
				Code:    err.StatusCode,
				Status:  err.Type,
				Message: err.Message,
			},
		},
	}
}

func ErrorToGeminiErr(err error) *GeminiErrorResponse {
	if err == nil {
		return nil
	}
	return &GeminiErrorResponse{
		ErrorInfo: &GeminiError{
			Code:    500,
			Status:  "internal_error",
			Message: err.Error(),
		},
	}
}

func ConvertRole(roleName string) string {
	switch roleName {
	case types.ChatMessageRoleFunction, types.ChatMessageRoleTool:
		return types.ChatMessageRoleFunction
	case types.ChatMessageRoleAssistant:
		return "model"
	default:
		return types.ChatMessageRoleUser
	}
}

func ConvertFinishReason(reason string) string {
	switch reason {
	case "STOP":
		return types.FinishReasonStop
	case "MAX_TOKENS":
		return types.FinishReasonLength
	case "CONTINUATION":
		// Long Decode 单次 hop 到达输出上限、还有剩余可续写：对 OpenAI 协议而言就是被长度截断，
		// 落到 default 会被当成 content_filter 误导客户端。
		return types.FinishReasonLength
	default:
		return types.FinishReasonContentFilter
	}
}
