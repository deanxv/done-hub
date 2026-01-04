package providers

import (
	"done-hub/common/config"
	"done-hub/model"
	"done-hub/providers/ali"
	"done-hub/providers/antigravity"
	"done-hub/providers/azure"
	azurespeech "done-hub/providers/azureSpeech"
	"done-hub/providers/azure_v1"
	"done-hub/providers/azuredatabricks"
	"done-hub/providers/baichuan"
	"done-hub/providers/baidu"
	"done-hub/providers/base"
	"done-hub/providers/bedrock"
	"done-hub/providers/claude"
	"done-hub/providers/claudecode"
	"done-hub/providers/cloudflareAI"
	"done-hub/providers/codex"
	"done-hub/providers/cohere"
	"done-hub/providers/coze"
	"done-hub/providers/deepseek"
	"done-hub/providers/gemini"
	"done-hub/providers/geminicli"
	"done-hub/providers/github"
	"done-hub/providers/groq"
	"done-hub/providers/hunyuan"
	"done-hub/providers/jina"
	"done-hub/providers/lingyi"
	"done-hub/providers/midjourney"
	"done-hub/providers/minimax"
	"done-hub/providers/mistral"
	"done-hub/providers/moonshot"
	"done-hub/providers/ollama"
	"done-hub/providers/openai"
	"done-hub/providers/openrouter"
	"done-hub/providers/palm"
	"done-hub/providers/recraftAI"
	"done-hub/providers/replicate"
	"done-hub/providers/siliconflow"
	"done-hub/providers/stabilityAI"
	"done-hub/providers/suno"
	"done-hub/providers/tencent"
	"done-hub/providers/vertexai"
	vertexai_express "done-hub/providers/vertexai_express"
	"done-hub/providers/xAI"
	"done-hub/providers/xunfei"
	"done-hub/providers/zhipu"

	"github.com/gin-gonic/gin"
)

// 定义供应商工厂接口
type ProviderFactory interface {
	Create(Channel *model.Channel) base.ProviderInterface
}

// 创建全局的供应商工厂映射
var providerFactories = make(map[int]ProviderFactory)

// 在程序启动时，添加所有的供应商工厂
func init() {
	providerFactories = map[int]ProviderFactory{
		config.ChannelTypeOpenAI:          openai.OpenAIProviderFactory{},
		config.ChannelTypeAzure:           azure.AzureProviderFactory{},
		config.ChannelTypeAli:             ali.AliProviderFactory{},
		config.ChannelTypeTencent:         tencent.TencentProviderFactory{},
		config.ChannelTypeBaidu:           baidu.BaiduProviderFactory{},
		config.ChannelTypeAnthropic:       claude.ClaudeProviderFactory{},
		config.ChannelTypePaLM:            palm.PalmProviderFactory{},
		config.ChannelTypeZhipu:           zhipu.ZhipuProviderFactory{},
		config.ChannelTypeXunfei:          xunfei.XunfeiProviderFactory{},
		config.ChannelTypeAzureSpeech:     azurespeech.AzureSpeechProviderFactory{},
		config.ChannelTypeGemini:          gemini.GeminiProviderFactory{},
		config.ChannelTypeBaichuan:        baichuan.BaichuanProviderFactory{},
		config.ChannelTypeMiniMax:         minimax.MiniMaxProviderFactory{},
		config.ChannelTypeDeepseek:        deepseek.DeepseekProviderFactory{},
		config.ChannelTypeMistral:         mistral.MistralProviderFactory{},
		config.ChannelTypeGroq:            groq.GroqProviderFactory{},
		config.ChannelTypeBedrock:         bedrock.BedrockProviderFactory{},
		config.ChannelTypeMidjourney:      midjourney.MidjourneyProviderFactory{},
		config.ChannelTypeCloudflareAI:    cloudflareAI.CloudflareAIProviderFactory{},
		config.ChannelTypeCohere:          cohere.CohereProviderFactory{},
		config.ChannelTypeStabilityAI:     stabilityAI.StabilityAIProviderFactory{},
		config.ChannelTypeCoze:            coze.CozeProviderFactory{},
		config.ChannelTypeOllama:          ollama.OllamaProviderFactory{},
		config.ChannelTypeMoonshot:        moonshot.MoonshotProviderFactory{},
		config.ChannelTypeLingyi:          lingyi.LingyiProviderFactory{},
		config.ChannelTypeHunyuan:         hunyuan.HunyuanProviderFactory{},
		config.ChannelTypeSuno:            suno.SunoProviderFactory{},
		config.ChannelTypeVertexAI:        vertexai.VertexAIProviderFactory{},
		config.ChannelTypeSiliconflow:     siliconflow.SiliconflowProviderFactory{},
		config.ChannelTypeJina:            jina.JinaProviderFactory{},
		config.ChannelTypeGithub:          github.GithubProviderFactory{},
		config.ChannelTypeRecraft:         recraftAI.RecraftProviderFactory{},
		config.ChannelTypeReplicate:       replicate.ReplicateProviderFactory{},
		config.ChannelTypeOpenRouter:      openrouter.OpenRouterProviderFactory{},
		config.ChannelTypeAzureDatabricks: azuredatabricks.AzureDatabricksProviderFactory{},
		config.ChannelTypeAzureV1:         azure_v1.AzureV1ProviderFactory{},
		config.ChannelTypeXAI:             xAI.XAIProviderFactory{},
		config.ChannelTypeGeminiCli:       geminicli.GeminiCliProviderFactory{},
		config.ChannelTypeClaudeCode:      claudecode.ClaudeCodeProviderFactory{},
		config.ChannelTypeCodex:           codex.CodexProviderFactory{},
		config.ChannelTypeAntigravity:     antigravity.AntigravityProviderFactory{},
		config.ChannelTypeVertexAIExpress: vertexai_express.VertexAIExpressProviderFactory{},
	}
}

// 获取供应商
func GetProvider(channel *model.Channel, c *gin.Context) base.ProviderInterface {
	factory, ok := providerFactories[channel.Type]
	var provider base.ProviderInterface
	if !ok {
		// 处理未找到的供应商工厂
		baseURL := channel.GetBaseURL()
		if baseURL == "" {
			return nil
		}

		provider = openai.CreateOpenAIProvider(channel, baseURL)
	} else {
		provider = factory.Create(channel)
	}
	provider.SetContext(c)

	return provider
}
