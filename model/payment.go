package model

import (
	"go-template/common/utils"

	"gorm.io/gorm"
)

type CurrencyType string

const (
	CurrencyTypeUSD CurrencyType = "USD"
	CurrencyTypeCNY CurrencyType = "CNY"
)

// Payment 支付网关配置
type Payment struct {
	// ID 主键自增ID
	ID int `json:"id" gorm:"comment:主键ID"`
	// Type 网关类型（如 stripe/alipay/wxpay/epay 等）
	Type string `json:"type" form:"type" gorm:"type:varchar(16);comment:网关类型"`
	// UUID 网关唯一标识（用于回调识别），系统生成
	UUID string `json:"uuid" form:"uuid" gorm:"type:char(32);uniqueIndex;comment:网关唯一标识"`
	// Name 网关名称（展示使用）
	Name string `json:"name" form:"name" gorm:"type:varchar(255);not null;comment:网关名称"`
	// Icon 图标URL（前端展示）
	Icon string `json:"icon" form:"icon" gorm:"type:varchar(300);comment:图标URL"`
	// NotifyDomain 回调域名（如需外网回调，配置对外可访问域名）
	NotifyDomain string `json:"notify_domain" form:"notify_domain" gorm:"type:varchar(300);comment:回调域名"`
	// FixedFee 固定手续费
	FixedFee float64 `json:"fixed_fee" form:"fixed_fee" gorm:"type:decimal(10,2);default:0.00;comment:固定手续费"`
	// PercentFee 百分比手续费（0~1，示例：0.05=5%）
	PercentFee float64 `json:"percent_fee" form:"percent_fee" gorm:"type:decimal(10,2);default:0.00;comment:百分比手续费"`
	// Currency 币种（USD/CNY）
	Currency CurrencyType `json:"currency" form:"currency" gorm:"type:varchar(5);comment:币种"`
	// Config 网关配置（JSON 文本，按网关类型定义）
	Config string `json:"config" form:"config" gorm:"type:text;comment:网关配置JSON"`
	// Sort 排序（数字越小越靠前）
	Sort int `json:"sort" form:"sort" gorm:"default:1;comment:排序"`
	// Enable 是否启用
	Enable *bool `json:"enable" form:"enable" gorm:"default:true;comment:是否启用"`
	// CreatedAt 创建时间（Unix 秒）
	CreatedAt int64 `json:"created_at" gorm:"bigint;comment:创建时间(Unix秒)"`
	// UpdatedAt 更新时间（Unix 秒）
	UpdatedAt int64 `json:"-" gorm:"bigint;comment:更新时间(Unix秒)"`
	// DeletedAt 软删除（索引）
	DeletedAt gorm.DeletedAt `json:"-" gorm:"index;comment:软删除时间"`
}

func GetPaymentByID(id int) (*Payment, error) {
	var payment Payment
	err := DB.First(&payment, id).Error
	return &payment, err
}

func GetPaymentByUUID(uuid string) (*Payment, error) {
	var payment Payment
	err := DB.Where("uuid = ? AND enable = ?", uuid, true).First(&payment).Error
	return &payment, err
}

var allowedPaymentOrderFields = map[string]bool{
	"id":         true,
	"uuid":       true,
	"name":       true,
	"type":       true,
	"sort":       true,
	"enable":     true,
	"created_at": true,
}

type SearchPaymentParams struct {
	Payment
	PaginationParams
}

func GetPanymentList(params *SearchPaymentParams) (*DataResult[Payment], error) {
	var payments []*Payment

	db := DB.Omit("key")

	if params.Type != "" {
		db = db.Where("type = ?", params.Type)
	}

	if params.Name != "" {
		db = db.Where("name LIKE ?", params.Name+"%")
	}

	if params.UUID != "" {
		db = db.Where("uuid = ?", params.UUID)
	}

	if params.Currency != "" {
		db = db.Where("currency = ?", params.Currency)
	}

	return PaginateAndOrder(db, &params.PaginationParams, &payments, allowedPaymentOrderFields)
}

func GetUserPaymentList() ([]*Payment, error) {
	var payments []*Payment
	err := DB.Model(payments).Select("uuid, name, icon, fixed_fee, percent_fee, currency, sort").Where("enable = ?", true).Find(&payments).Error
	return payments, err
}

func (p *Payment) Insert() error {
	p.UUID = utils.GetUUID()
	return DB.Create(p).Error
}

func (p *Payment) Update(overwrite bool) error {
	var err error

	if overwrite {
		err = DB.Model(p).Select("*").Updates(p).Error
	} else {
		err = DB.Model(p).Updates(p).Error
	}

	return err
}

func (p *Payment) Delete() error {
	return DB.Delete(p).Error
}
