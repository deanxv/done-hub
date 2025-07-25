package config

import (
	"strings"
	"time"

	"done-hub/common/utils"

	"github.com/spf13/viper"
)

func InitConf() {
	defaultConfig()
	setEnv()
	Language = viper.GetString("language")
	IsMasterNode = viper.GetString("node_type") != "slave"
	RequestInterval = time.Duration(viper.GetInt("polling_interval")) * time.Second
	SessionSecret = utils.GetOrDefault("session_secret", SessionSecret)
	UserInvoiceMonth = viper.GetBool("user_invoice_month")
	GitHubProxy = viper.GetString("github_proxy")
	MCP_ENABLE = viper.GetBool("mcp.enable") != false
	UPTIMEKUMA_ENABLE = viper.GetBool("uptime_kuma.enable") != false
	UPTIMEKUMA_DOMAIN = viper.GetString("uptime_kuma.domain")
	UPTIMEKUMA_STATUS_PAGE_NAME = viper.GetString("uptime_kuma.status_page_name")
}

func setEnv() {
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
}

func defaultConfig() {
	viper.SetDefault("port", "3000")
	viper.SetDefault("gin_mode", "release")
	viper.SetDefault("log_dir", "./logs")
	viper.SetDefault("sqlite_path", "done-hub.db")
	viper.SetDefault("sqlite_busy_timeout", 3000)
	viper.SetDefault("sync_frequency", 600)
	viper.SetDefault("batch_update_interval", 5)
	viper.SetDefault("global.api_rate_limit", 300)
	viper.SetDefault("global.web_rate_limit", 300)
	viper.SetDefault("connect_timeout", 5)
	viper.SetDefault("auto_price_updates", false)
	viper.SetDefault("auto_price_updates_mode", "system")
	viper.SetDefault("auto_price_updates_interval", 1440)
	viper.SetDefault("update_price_service", "https://raw.githubusercontent.com/MartialBE/done-hub/prices/prices.json")
	viper.SetDefault("language", "zh_CN")
	viper.SetDefault("favicon", "")
	viper.SetDefault("user_invoice_month", false)
	viper.SetDefault("mcp.enable", false)
	viper.SetDefault("uptime_kuma.enable", false)
	viper.SetDefault("uptime_kuma.domain", "")
	viper.SetDefault("uptime_kuma.status_page_name", "")
}
