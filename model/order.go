package model

import (
	"time"

	"gorm.io/gorm"
)

type OrderStatus string

const (
	OrderStatusPending OrderStatus = "pending"
	OrderStatusSuccess OrderStatus = "success"
	OrderStatusFailed  OrderStatus = "failed"
	OrderStatusClosed  OrderStatus = "closed"
)

// Order 订单表（充值/支付用，不耦合配额发放）
type Order struct {
	// ID 主键自增ID
	ID int `json:"id" gorm:"comment:主键ID"`
	// UserId 下单用户ID
	UserId int `json:"user_id" gorm:"comment:下单用户ID"`
	// GatewayId 支付网关ID（关联 Payment.ID）
	GatewayId int `json:"gateway_id" gorm:"comment:支付网关ID"`
	// TradeNo 站内订单号（唯一）
	TradeNo string `json:"trade_no" gorm:"type:varchar(50);uniqueIndex;comment:站内订单号"`
	// GatewayNo 第三方支付渠道返回的订单号/流水号
	GatewayNo string `json:"gateway_no" gorm:"type:varchar(100);comment:第三方订单号"`
	// Amount 充值面额（站内单位，整数）
	Amount int `json:"amount" gorm:"default:0;comment:充值面额(站内单位)"`
	// OrderAmount 实付金额（单位与 OrderCurrency 对应）
	OrderAmount float64 `json:"order_amount" gorm:"type:decimal(10,2);default:0;comment:实付金额"`
	// OrderCurrency 订单币种（USD/CNY）
	OrderCurrency CurrencyType `json:"order_currency" gorm:"type:varchar(16);comment:订单币种"`
	// Quota 折算内部额度（当前版本仅记录，不做发放）
	Quota int `json:"quota" gorm:"type:int;default:0;comment:折算内部额度(仅记录)"`
	// Fee 手续费（按配置计算）
	Fee float64 `json:"fee" gorm:"type:decimal(10,2);default:0;comment:手续费"`
	// Discount 优惠金额（按配置计算）
	Discount float64 `json:"discount" gorm:"type:decimal(10,2);default:0;comment:优惠金额"`
	// Status 订单状态（pending/success/failed/closed）
	Status OrderStatus `json:"status" gorm:"type:varchar(32);comment:订单状态"`
	// CreatedAt 创建时间（Unix 秒）
	CreatedAt int `json:"created_at" gorm:"comment:创建时间(Unix秒)"`
	// UpdatedAt 更新时间（保留字段）
	UpdatedAt int `json:"-" gorm:"comment:更新时间保留字段"`
	// DeletedAt 软删除（索引）
	DeletedAt gorm.DeletedAt `json:"-" gorm:"index;comment:软删除时间"`
}

// 查询并关闭未完成的订单
func CloseUnfinishedOrder() error {
	// 关闭超过 3 小时未支付的订单
	unixTime := time.Now().Unix() - 3*3600
	return DB.Model(&Order{}).Where("status = ? AND created_at < ?", OrderStatusPending, unixTime).Update("status", OrderStatusClosed).Error
}

func GetOrderByTradeNo(tradeNo string) (*Order, error) {
	var order Order
	err := DB.Where("trade_no = ?", tradeNo).First(&order).Error
	return &order, err
}

func GetUserOrder(userId int, tradeNo string) (*Order, error) {
	var order Order
	err := DB.Where("user_id = ? AND trade_no = ?", userId, tradeNo).First(&order).Error
	return &order, err
}

func (o *Order) Insert() error {
	return DB.Create(o).Error
}

func (o *Order) Update() error {
	return DB.Save(o).Error
}

var allowedOrderFields = map[string]bool{
	"id":         true,
	"gateway_id": true,
	"user_id":    true,
	"status":     true,
	"created_at": true,
}

type SearchOrderParams struct {
	UserId         int    `form:"user_id"`
	GatewayId      int    `form:"gateway_id"`
	TradeNo        string `form:"trade_no"`
	GatewayNo      string `form:"gateway_no"`
	Status         string `form:"status"`
	StartTimestamp int64  `form:"start_timestamp"`
	EndTimestamp   int64  `form:"end_timestamp"`
	PaginationParams
}

func GetOrderList(params *SearchOrderParams) (*DataResult[Order], error) {
	var orders []*Order

	db := DB.Omit("key")
	if params.GatewayId != 0 {
		db = db.Where("gateway_id = ?", params.GatewayId)
	}
	if params.UserId != 0 {
		db = db.Where("user_id = ?", params.UserId)
	}

	if params.TradeNo != "" {
		db = db.Where("trade_no = ?", params.TradeNo)
	}

	if params.GatewayNo != "" {
		db = db.Where("gateway_no = ?", params.GatewayNo)
	}

	if params.Status != "" {
		db = db.Where("status = ?", params.Status)
	}

	if params.StartTimestamp != 0 {
		db = db.Where("created_at >= ?", params.StartTimestamp)
	}
	if params.EndTimestamp != 0 {
		db = db.Where("created_at <= ?", params.EndTimestamp)
	}

	return PaginateAndOrder(db, &params.PaginationParams, &orders, allowedOrderFields)
}

type OrderStatistics struct {
	Quota         int64   `json:"quota"`
	Money         float64 `json:"money"`
	OrderCurrency string  `json:"order_currency"`
}

func GetStatisticsOrder() (orderStatistics []*OrderStatistics, err error) {
	err = DB.Model(&Order{}).Select("sum(quota) as quota, sum(order_amount) as money, order_currency").Where("status = ?", OrderStatusSuccess).Group("order_currency").Scan(&orderStatistics).Error
	return orderStatistics, err
}

type OrderStatisticsGroup struct {
	Date          string  `json:"date"`
	Quota         int64   `json:"quota"`
	Money         float64 `json:"money"`
	OrderCurrency string  `json:"order_currency"`
}

func GetStatisticsOrderByPeriod(startTimestamp, endTimestamp int64) (orderStatistics []*OrderStatisticsGroup, err error) {
	groupSelect := getTimestampGroupsSelect("created_at", "day", "date")

	err = DB.Raw(`
		SELECT `+groupSelect+`,
		sum(quota) as quota,
		sum(order_amount) as money,
		order_currency
		FROM orders
		WHERE status= ?
		AND created_at BETWEEN ? AND ?
		GROUP BY date, order_currency
		ORDER BY date, order_currency
	`, OrderStatusSuccess, startTimestamp, endTimestamp).Scan(&orderStatistics).Error

	return orderStatistics, err
}
