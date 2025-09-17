package config

import (
	"time"

	"github.com/google/uuid"
)

var StartTime = time.Now().Unix() // unit: second
var Version = "v0.0.0"            // this hard coding will be replaced automatically when building, no need to manually change
var Commit = "unknown"
var BuildTime = "unknown"
var SystemName = "Go Template"
var ServerAddress = "http://localhost:3000"
var Debug = false

var Language = ""
var Footer = ""
var Logo = ""
var QuotaPerUnit = 500 * 1000.0 // $0.002 / 1K tokens

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
var TurnstileCheckEnabled = false
var RegisterEnabled = true
var InviteCodeRegisterEnabled = false
var OIDCAuthEnabled = false
var LinuxDoOAuthEnabled = false
var LinuxDoOAuthTrustLevelEnabled = false

// mj
var MjNotifyEnabled = false

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

var SMTPServer = ""
var SMTPPort = 587
var SMTPAccount = ""
var SMTPFrom = ""
var SMTPToken = ""

var GitHubProxy = ""
var GitHubClientId = ""
var GitHubClientSecret = ""

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

// Removed AI-specific toggles/constants in minimal edition

var RootUserEmail = ""

var IsMasterNode = true

var RequestInterval time.Duration

var BatchUpdateEnabled = false
var BatchUpdateInterval = 5

var MCP_ENABLE = false
var UPTIMEKUMA_ENABLE = false
var UPTIMEKUMA_DOMAIN = ""
var UPTIMEKUMA_STATUS_PAGE_NAME = ""

const (
	RoleGuestUser  = 0
	RoleCommonUser = 1
	RoleAdminUser  = 10
	RoleRootUser   = 100
)

var RateLimitKeyExpirationDuration = 20 * time.Minute

const (
	UserStatusEnabled  = 1 // don't use 0, 0 is the default value!
	UserStatusDisabled = 2 // also don't use 0
)

// AI channel and relay mode constants removed in minimal edition

type ContextKey string

// linux do 用户信任等级
const (
	Basic   = 1 // 基础用户
	Member  = 2 // 会员
	Regular = 3 // 活跃用户
	Leader  = 4 // 领导者
)
