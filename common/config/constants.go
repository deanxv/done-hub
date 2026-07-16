package config

import (
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

var StartTime = time.Now().Unix() // unit: second
var Version = "v0.0.0"            // this hard coding will be replaced automatically when building, no need to manually change
var Commit = "unknown"
var BuildTime = "unknown"
var SystemName = "Done Hub"
var ServerAddress = "http://localhost:3000"
var PaymentCallbackAddress = ""
var Debug = false

var OldTokenMaxId = 0

var Language = ""
var Footer = ""
var Logo = ""
var TopUpLink = ""
var ChatLink = ""
var ChatLinks = ""
var AnalyticsCode = ""
var QuotaPerUnit = 500 * 1000.0 // $0.002 / 1K tokens
var DisplayInCurrencyEnabled = true
var DisplayTokenStatEnabled = true

// 是否开启用户月账单功能
var UserInvoiceMonth = false

// Any options with "Secret", "Token" in its key won't be return by GetOptions

var SessionSecret = uuid.New().String()

var ItemsPerPage = 10
var MaxRecentItems = 100

var PasswordLoginEnabled = true
var PasswordRegisterEnabled = true
var EmailVerificationEnabled = false
var GitHubOAuthEnabled = false
var WeChatAuthEnabled = false
var LarkAuthEnabled = false
var TurnstileCheckEnabled = false
var RegisterEnabled = true
var InviteCodeRegisterEnabled = false
var UserAgreementEnabled = false
var PrivacyPolicyEnabled = false
var OIDCAuthEnabled = false
var LinuxDoOAuthEnabled = false
var LinuxDoOAuthTrustLevelEnabled = false
var LinuxDoOAuthDynamicTrustLevel = true // 动态限制已注册用户的信任等级，关闭后已注册用户不受新等级限制影响

// 是否开启内容审查
var EnableSafe = false

// 默认使用系统自带关键词审查工具
var SafeToolName = "Keyword"

// 系统自带关键词审查默认字典
var SafeKeyWords = []string{
	"fuck",
	"shit",
	"bitch",
	"pussy",
	"cunt",
	"dick",
	"asshole",
	"bastard",
	"slut",
	"whore",
	"nigger",
	"nigga",
	"nazi",
	"gay",
	"lesbian",
	"transgender",
	"queer",
	"homosexual",
	"incest",
	"rape",
	"rapist",
	"raped",
	"raping",
	"raped",
	"raping",
	"rapist",
	"rape",
	"sex",
	"sexual",
	"sexually",
	"sexualize",
	"sexualized",
	"sexualizes",
	"sexualizing",
	"sexually",
	"sex",
	"porn",
	"pornography",
	"prostitute",
	"prostitution",
	"masturbate",
	"masturbation",
	"pedophile",
	"pedophilia",
	"hentai",
	"explicit",
	"obscene",
	"obscenity",
	"erotic",
	"erotica",
	"fetish",
	"NSFW",
	"nude",
	"nudity",
	"harassment",
	"abuse",
	"violent",
	"violence",
	"suicide",
	"racist",
	"racism",
	"discrimination",
	"hate",
	"terrorism",
	"terrorist",
	"drugs",
	"cocaine",
	"heroin",
	"methamphetamine",
}

// mj
var MjNotifyEnabled = false

// 内置聊天功能开关
var BuiltinChatEnabled = true

var EmailDomainRestrictionEnabled = false
var EmailDomainWhitelist = []string{
	"gmail.com",
	"163.com",
	"126.com",
	"qq.com",
	"outlook.com",
	"hotmail.com",
	"icloud.com",
	"yahoo.com",
	"foxmail.com",
}

var MemoryCacheEnabled = false

var LogConsumeEnabled = true

var LogAutoDeleteEnabled = false // 是否启用消费日志自动清理
var LogAutoDeleteDays = 30       // 保留天数，默认30天

var SMTPServer = ""
var SMTPPort = 587
var SMTPAccount = ""
var SMTPFrom = ""
var SMTPToken = ""

var ChatImageRequestProxy = ""

var GitHubProxy = ""
var GitHubClientId = ""
var GitHubClientSecret = ""
var GitHubOldIdCloseEnabled = false

var LarkClientId = ""
var LarkClientSecret = ""

var WeChatServerAddress = ""
var WeChatServerToken = ""
var WeChatAccountQRCodeImageURL = ""

var TurnstileSiteKey = ""
var TurnstileSecretKey = ""

var OIDCClientId = ""
var OIDCClientSecret = ""
var OIDCIssuer = ""
var OIDCScopes = ""
var OIDCUsernameClaims = ""

var LinuxDoClientId = ""
var LinuxDoClientSecret = ""
var LinuxDoOAuthLowestTrustLevel = 1

var QuotaForNewUser = 0
var QuotaForInviter = 0
var QuotaForInvitee = 0
var InviterRewardType = "fixed" // "fixed" 或 "percentage"
var InviterRewardValue = 0
var ChannelDisableThreshold = 5.0
var AutomaticDisableChannelEnabled = false
var AutomaticEnableChannelEnabled = false
var AutomaticDisableChannelNotifyEnabled = true
var QuotaRemindEnabled = true
var QuotaRemindThreshold = 500000
var PreConsumedQuota = 500
var ApproximateTokenEnabled = false
var EmptyResponseBillingEnabled = true
var DisableTokenEncoders = false
var RetryTimes = 0
var RetryTimeOut = 10

// ChannelFailErrorWrapEnabled 是否启用"渠道失败统一封装"。
// 开启（默认）：FilterOpenAIErr 把所有非 400 上游错误坍缩为 503 + ChannelFailErrorMessage，
//
//	对客户端隐藏上游身份、key 状态等内部信息。
//
// 关闭：跳过坍缩，上游错误原样透传（仍走 request id 拼接 / Type 隐藏等轻度规整）。
//
//	给运维一个"临时关掉看上游真实错误"的口子，便于调试。
var ChannelFailErrorWrapEnabled = true

// ChannelFailErrorMessage 返回给客户端的统一上游错误文案。
// 仅在 ChannelFailErrorWrapEnabled 为 true 时生效；留空时回退到 DefaultChannelFailErrorMessage。
// 通过 GetChannelFailErrorMessage() 取值。
const DefaultChannelFailErrorMessage = "当前分组上游负载已饱和，请稍后再试"

var ChannelFailErrorMessage = DefaultChannelFailErrorMessage

// GetChannelFailErrorMessage 返回当前配置的统一错误文案；
// 运维在管理后台清空（空串或纯空白）时回退到默认值，避免客户端收到空 message。
func GetChannelFailErrorMessage() string {
	if strings.TrimSpace(ChannelFailErrorMessage) == "" {
		return DefaultChannelFailErrorMessage
	}
	return ChannelFailErrorMessage
}

// 统一请求响应模型（响应中显示用户请求的原始模型名称）
var UnifiedRequestResponseModelEnabled = false

// FingerprintPassThroughEnabled 让原生 Claude 格式渠道（Bedrock / Claude 官方）尽量保留
// 上游的响应指纹：非流式原始字节透传、Bedrock 额外透传 x-amzn-* 响应头、流式跳过 model 改写。
// 默认开启；关闭后回退到与其它渠道一致的结构体序列化行为。
var FingerprintPassThroughEnabled = true

// 模型名称大小写不敏感匹配
var ModelNameCaseInsensitiveEnabled = false

var DefaultChannelWeight = uint(1)
var RetryCooldownSeconds = 5

// RetryCooldownPerStatus stores the JSON source of per-status cooldown overrides,
// e.g. {"503":120,"502":60}. The parsed map is held in retryCooldownPerStatusMap
// and accessed via GetRetryCooldownForStatus.
var RetryCooldownPerStatus = ""

var (
	retryCooldownPerStatusMap  = map[int]int{}
	retryCooldownPerStatusLock sync.RWMutex
)

// SetRetryCooldownPerStatusMap replaces the in-memory map. Called by the option
// setter after parsing the JSON payload.
func SetRetryCooldownPerStatusMap(m map[int]int) {
	retryCooldownPerStatusLock.Lock()
	defer retryCooldownPerStatusLock.Unlock()
	retryCooldownPerStatusMap = m
}

// GetRetryCooldownForStatus returns (seconds, configured). configured=false means
// the caller should fall through to RetryCooldownSeconds (or skip cooldown entirely
// depending on the caller's policy).
func GetRetryCooldownForStatus(statusCode int) (int, bool) {
	retryCooldownPerStatusLock.RLock()
	defer retryCooldownPerStatusLock.RUnlock()
	v, ok := retryCooldownPerStatusMap[statusCode]
	return v, ok
}

var CFWorkerImageUrl = ""
var CFWorkerImageKey = ""

var RootUserEmail = ""

var IsMasterNode = true

// RelayOnly 纯 relay 网关模式：仅暴露转发接口(/v1、/claude、/gemini、/mj 等)与 /health，
// 前端页面、/api 管理接口、dashboard 一律返回 404，避免从节点泄露主域名等信息。
var RelayOnly = false

var RequestInterval time.Duration

var BatchUpdateEnabled = false
var BatchUpdateInterval = 5

var MCP_ENABLE = false

var UPTIMEKUMA_ENABLE = false
var UPTIMEKUMA_DOMAIN = ""
var UPTIMEKUMA_STATUS_PAGE_NAME = ""

// Gemini
var GeminiAPIEnabled = true

// Claude
var ClaudeAPIEnabled = true

const (
	RoleGuestUser    = 0
	RoleCommonUser   = 1
	RoleReliableUser = 3 // 可信的内部员工
	RoleAdminUser    = 10
	RoleRootUser     = 100
)

var RateLimitKeyExpirationDuration = 20 * time.Minute

const (
	UserStatusEnabled  = 1 // don't use 0, 0 is the default value!
	UserStatusDisabled = 2 // also don't use 0
)

const (
	TokenStatusEnabled   = 1 // don't use 0, 0 is the default value!
	TokenStatusDisabled  = 2 // also don't use 0
	TokenStatusExpired   = 3
	TokenStatusExhausted = 4
)

const (
	RedemptionCodeStatusEnabled  = 1 // don't use 0, 0 is the default value!
	RedemptionCodeStatusDisabled = 2 // also don't use 0
	RedemptionCodeStatusUsed     = 3 // also don't use 0
)

const (
	ChannelStatusUnknown          = 0
	ChannelStatusEnabled          = 1 // don't use 0, 0 is the default value!
	ChannelStatusManuallyDisabled = 2 // also don't use 0
	ChannelStatusAutoDisabled     = 3
)

const (
	ChannelTypeUnknown = 0
	ChannelTypeOpenAI  = 1
	// ChannelTypeAPI2D          = 2
	ChannelTypeAzure = 3
	// ChannelTypeCloseAI = 4
	// ChannelTypeOpenAISB       = 5
	// ChannelTypeOpenAIMax      = 6
	// ChannelTypeOhMyGPT        = 7
	ChannelTypeCustom = 8
	// ChannelTypeAILS           = 9
	// ChannelTypeAIProxy        = 10
	ChannelTypePaLM = 11
	// ChannelTypeAPI2GPT        = 12
	// ChannelTypeAIGC2D         = 13
	ChannelTypeAnthropic  = 14
	ChannelTypeBaidu      = 15
	ChannelTypeZhipu      = 16
	ChannelTypeAli        = 17
	ChannelTypeXunfei     = 18
	ChannelType360        = 19
	ChannelTypeOpenRouter = 20
	// ChannelTypeAIProxyLibrary = 21
	// ChannelTypeFastGPT        = 22
	ChannelTypeTencent         = 23
	ChannelTypeAzureSpeech     = 24
	ChannelTypeGemini          = 25
	ChannelTypeBaichuan        = 26
	ChannelTypeMiniMax         = 27
	ChannelTypeDeepseek        = 28
	ChannelTypeMoonshot        = 29
	ChannelTypeMistral         = 30
	ChannelTypeGroq            = 31
	ChannelTypeBedrock         = 32
	ChannelTypeLingyi          = 33
	ChannelTypeMidjourney      = 34
	ChannelTypeCloudflareAI    = 35
	ChannelTypeCohere          = 36
	ChannelTypeStabilityAI     = 37
	ChannelTypeCoze            = 38
	ChannelTypeOllama          = 39
	ChannelTypeHunyuan         = 40
	ChannelTypeSuno            = 41
	ChannelTypeVertexAI        = 42
	ChannelTypeLLAMA           = 43
	ChannelTypeIdeogram        = 44
	ChannelTypeSiliconflow     = 45
	ChannelTypeFlux            = 46
	ChannelTypeJina            = 47
	ChannelTypeRerank          = 48
	ChannelTypeGithub          = 49
	ChannelTypeRecraft         = 51
	ChannelTypeReplicate       = 52
	ChannelTypeKling           = 53
	ChannelTypeAzureDatabricks = 54
	ChannelTypeAzureV1         = 55
	ChannelTypeXAI             = 56
	ChannelTypeGeminiCli       = 57
	ChannelTypeClaudeCode      = 58
	ChannelTypeCodex           = 59
	ChannelTypeAntigravity     = 60
	ChannelTypeVertexAIExpress = 61
)

const (
	RelayModeUnknown = iota
	RelayModeChatCompletions
	RelayModeCompletions
	RelayModeEmbeddings
	RelayModeModerations
	RelayModeImagesGenerations
	RelayModeImagesEdits
	RelayModeImagesVariations
	RelayModeEdits
	RelayModeAudioSpeech
	RelayModeAudioTranscription
	RelayModeAudioTranslation
	RelayModeSuno
	RelayModeRerank
	RelayModeChatRealtime
	RelayModeKling
	RelayModeResponses
)

type ContextKey string

// linux do 用户信任等级
const (
	Basic   = 1 // 基础用户
	Member  = 2 // 会员
	Regular = 3 // 活跃用户
	Leader  = 4 // 领导者
)
