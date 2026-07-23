package bedrockmessages

// bedrockMantleService 是 "Claude in Amazon Bedrock" 原生 Messages 端点的 SigV4 service name。
// 取自官方 curl 的 --aws-sigv4 "aws:amz:{region}:bedrock-mantle" 第四段，
// 与 legacy InvokeModel 渠道的 "bedrock" 不同。
const bedrockMantleService = "bedrock-mantle"

// defaultAnthropicVersion 是缺省的 anthropic-version 头值（客户端未携带时使用）。
const defaultAnthropicVersion = "2023-06-01"
