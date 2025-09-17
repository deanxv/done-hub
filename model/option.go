package model

import (
	"done-hub/common/config"
	"done-hub/common/logger"
	"strings"
	"time"
)

// Option 配置项表（键值对，后台可通过接口修改并写入内存）
type Option struct {
	// Key 配置项键（主键）
	Key string `json:"key" gorm:"primaryKey;comment:配置项Key"`
	// Value 配置项值（字符串存储，复杂结构以 JSON 存储）
	Value string `json:"value" gorm:"comment:配置项值(字符串/JSON)"`
}

func AllOption() ([]*Option, error) {
	var options []*Option
	err := DB.Find(&options).Error
	return options, err
}

func GetOption(key string) (option Option, err error) {
	err = DB.First(&option, Option{Key: key}).Error
	return
}

func InitOptionMap() {
	// 登录注册与邮箱
	config.GlobalOption.RegisterBool("PasswordLoginEnabled", &config.PasswordLoginEnabled)
	config.GlobalOption.RegisterBool("PasswordRegisterEnabled", &config.PasswordRegisterEnabled)
	config.GlobalOption.RegisterBool("EmailVerificationEnabled", &config.EmailVerificationEnabled)
	config.GlobalOption.RegisterBool("EmailDomainRestrictionEnabled", &config.EmailDomainRestrictionEnabled)
	config.GlobalOption.RegisterBool("RegisterEnabled", &config.RegisterEnabled)
	config.GlobalOption.RegisterCustom("EmailDomainWhitelist", func() string {
		return strings.Join(config.EmailDomainWhitelist, ",")
	}, func(value string) error {
		config.EmailDomainWhitelist = strings.Split(value, ",")
		return nil
	}, "")

	// SMTP
	config.GlobalOption.RegisterString("SMTPServer", &config.SMTPServer)
	config.GlobalOption.RegisterString("SMTPFrom", &config.SMTPFrom)
	config.GlobalOption.RegisterInt("SMTPPort", &config.SMTPPort)
	config.GlobalOption.RegisterString("SMTPAccount", &config.SMTPAccount)
	config.GlobalOption.RegisterString("SMTPToken", &config.SMTPToken)

	// OIDC
	config.GlobalOption.RegisterBool("OIDCAuthEnabled", &config.OIDCAuthEnabled)
	config.GlobalOption.RegisterString("OIDCClientId", &config.OIDCClientId)
	config.GlobalOption.RegisterString("OIDCClientSecret", &config.OIDCClientSecret)
	config.GlobalOption.RegisterString("OIDCIssuer", &config.OIDCIssuer)
	config.GlobalOption.RegisterString("OIDCScopes", &config.OIDCScopes)
	config.GlobalOption.RegisterString("OIDCUsernameClaims", &config.OIDCUsernameClaims)

	// 页面与系统信息
	config.GlobalOption.RegisterValue("Notice")
	config.GlobalOption.RegisterValue("About")
	config.GlobalOption.RegisterValue("HomePageContent")
	config.GlobalOption.RegisterString("Footer", &config.Footer)
	config.GlobalOption.RegisterString("SystemName", &config.SystemName)
	config.GlobalOption.RegisterString("Logo", &config.Logo)
	config.GlobalOption.RegisterString("ServerAddress", &config.ServerAddress)

	// 支付基础配置
	config.GlobalOption.RegisterFloat("PaymentUSDRate", &config.PaymentUSDRate)
	config.GlobalOption.RegisterInt("PaymentMinAmount", &config.PaymentMinAmount)

	loadOptionsFromDatabase()
}

func loadOptionsFromDatabase() {
	options, _ := AllOption()
	for _, option := range options {
		err := config.GlobalOption.Set(option.Key, option.Value)
		if err != nil {
			logger.SysError("failed to update option map: " + err.Error())
		}
	}
}

func SyncOptions(frequency int) {
	for {
		time.Sleep(time.Duration(frequency) * time.Second)
		logger.SysLog("syncing options from database")
		loadOptionsFromDatabase()
	}
}

func UpdateOption(key string, value string) error {
	// Save to database first
	option := Option{
		Key: key,
	}
	// https://gorm.io/docs/update.html#Save-All-Fields
	DB.FirstOrCreate(&option, Option{Key: key})
	option.Value = value
	// Save is a combination function.
	// If save value does not contain primary key, it will execute Create,
	// otherwise it will execute Update (with all fields).
	DB.Save(&option)
	// Update OptionMap
	return config.GlobalOption.Set(key, value)
}
