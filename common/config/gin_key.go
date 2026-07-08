package config

const (
	GinRequestBodyKey          = "cached_request_body"
	GinProcessedBodyKey        = "processed_request_body"
	GinProcessedBodyIsVertexAI = "processed_body_is_vertexai"
	GinRawMapBodyKey           = "raw_map_body"

	GinProcessedBytesKey        = "processed_request_bytes"
	GinProcessedBytesIsVertexAI = "processed_bytes_is_vertexai"

	// Bedrock 响应指纹保真：provider 侧把上游原始响应暂存到 gin.Context，
	// 由 relay 层的响应写入函数按需取用，从而只影响 Bedrock 渠道。
	GinBedrockRawResponseBodyKey = "bedrock_raw_response_body"   // 非流式：上游原始响应字节
	GinBedrockPassThroughHeaders = "bedrock_passthrough_headers" // 流式+非流式：上游 x-amzn-* 等响应头

	// GinRawResponseBodyKey 通用的非流式响应字节透传：provider 在确认无需改写响应
	// （尤其是无模型映射需要改 model）时，把上游原始响应字节暂存到此 key，
	// 由 responseJsonClient 直接写回客户端，保留字段顺序 / 未知字段 / model 原名。
	// 与 Bedrock 专用 key 的区别：不附带 x-amzn-* 头透传，供 Claude 官方等渠道复用。
	GinRawResponseBodyKey = "raw_response_body"
)
