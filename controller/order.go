package controller

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"done-hub/common"
	"done-hub/common/config"
	"done-hub/common/logger"
	"done-hub/common/utils"
	"done-hub/model"
	"done-hub/payment"
	"done-hub/payment/types"

	"github.com/gin-gonic/gin"
)

type OrderRequest struct {
	UUID   string `json:"uuid" binding:"required"`
	Amount int    `json:"amount" binding:"required"`
}

type OrderResponse struct {
	TradeNo string `json:"trade_no"`
	*types.PayRequest
}

// CreateOrder
func CreateOrder(c *gin.Context) {
	var orderReq OrderRequest
	if err := c.ShouldBindJSON(&orderReq); err != nil {
		common.APIRespondWithError(c, http.StatusOK, errors.New("invalid request"))

		return
	}

	if orderReq.Amount <= 0 || orderReq.Amount < config.PaymentMinAmount {
		common.APIRespondWithError(c, http.StatusOK, fmt.Errorf("金额必须大于等于 %d", config.PaymentMinAmount))

		return
	}

	userId := c.GetInt("id")
	user, err := model.GetUserById(userId, false)
	if err != nil {
		common.APIRespondWithError(c, http.StatusOK, errors.New("用户不存在"))
		return
	}

	// 关闭用户未完成的订单
	go model.CloseUnfinishedOrder()

	paymentService, err := payment.NewPaymentService(orderReq.UUID)
	if err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}
	// 获取手续费和支付金额
	discount, fee, payMoney := calculateOrderAmount(paymentService.Payment, orderReq.Amount)
	// 开始支付
	tradeNo := utils.GenerateTradeNo()
	payRequest, err := paymentService.Pay(tradeNo, payMoney, user)
	if err != nil {
		common.APIRespondWithError(c, http.StatusOK, errors.New("创建支付失败，请稍后再试"))
		return
	}

	// 创建订单
	order := &model.Order{
		UserId:        userId,
		GatewayId:     paymentService.Payment.ID,
		TradeNo:       tradeNo,
		Amount:        orderReq.Amount,
		OrderAmount:   payMoney,
		OrderCurrency: paymentService.Payment.Currency,
		Fee:           fee,
		Discount:      discount,
		Status:        model.OrderStatusPending,
		Quota:         orderReq.Amount * int(config.QuotaPerUnit),
	}

	err = order.Insert()
	if err != nil {
		common.APIRespondWithError(c, http.StatusOK, errors.New("创建订单失败，请稍后再试"))
		return
	}

	orderResp := &OrderResponse{
		TradeNo:    tradeNo,
		PayRequest: payRequest,
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    orderResp,
	})
}

// orderLockEntry 订单锁条目，带时间戳用于清理
type orderLockEntry struct {
	lock     *sync.Mutex
	lastUsed time.Time
}

var orderLocks sync.Map
var createLock sync.Mutex

func init() {
	go cleanupOrderLocks()
}

// cleanupOrderLocks 定期清理长时间未使用的订单锁
func cleanupOrderLocks() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		orderLocks.Range(func(key, value interface{}) bool {
			if entry := value.(*orderLockEntry); now.Sub(entry.lastUsed) > 30*time.Minute {
				orderLocks.Delete(key)
			}
			return true
		})
	}
}

// LockOrder 尝试对给定订单号加锁
func LockOrder(tradeNo string) {
	var entry *orderLockEntry
	val, ok := orderLocks.Load(tradeNo)
	if !ok {
		createLock.Lock()
		val, ok = orderLocks.Load(tradeNo)
		if !ok {
			entry = &orderLockEntry{
				lock:     new(sync.Mutex),
				lastUsed: time.Now(),
			}
			orderLocks.Store(tradeNo, entry)
		} else {
			entry = val.(*orderLockEntry)
		}
		createLock.Unlock()
	} else {
		entry = val.(*orderLockEntry)
	}
	entry.lastUsed = time.Now()
	entry.lock.Lock()
}

// UnlockOrder 释放给定订单号的锁
func UnlockOrder(tradeNo string) {
	val, ok := orderLocks.Load(tradeNo)
	if ok {
		entry := val.(*orderLockEntry)
		entry.lastUsed = time.Now()
		entry.lock.Unlock()
	}
}

func PaymentCallback(c *gin.Context) {
	uuid := c.Param("uuid")
	paymentService, err := payment.NewPaymentService(uuid)
	if err != nil {
		common.APIRespondWithError(c, http.StatusOK, errors.New("payment not found"))
		return
	}

	payNotify, err := paymentService.HandleCallback(c, paymentService.Payment.Config)
	if err != nil {
		return
	}

	LockOrder(payNotify.TradeNo)
	defer UnlockOrder(payNotify.TradeNo)

	order, err := model.GetOrderByTradeNo(payNotify.TradeNo)
	if err != nil {
		logger.SysError(fmt.Sprintf("gateway callback failed to find order, trade_no: %s,", payNotify.TradeNo))
		return
	}

	completed, err := model.CompleteOrder(payNotify.TradeNo, payNotify.GatewayNo, order.UserId, order.Quota)
	if err != nil {
		logger.SysError(fmt.Sprintf("gateway callback failed to update order, trade_no: %s,", payNotify.TradeNo))
		return
	}
	if !completed {
		return
	}

	// Try to upgrade user group based on cumulative recharge amount
	err = model.CheckAndUpgradeUserGroup(order.UserId, order.Quota)
	if err != nil {
		logger.SysError(fmt.Sprintf("failed to check and upgrade user group, trade_no: %s, error: %s", payNotify.TradeNo, err.Error()))
	}

	model.RecordQuotaLog(order.UserId, model.LogTypeTopup, order.Quota, c.ClientIP(), fmt.Sprintf("在线充值成功，充值积分: %d，支付金额：%.2f %s", order.Quota, order.OrderAmount, order.OrderCurrency))

	// 处理邀请人充值返利
	err = model.ProcessInviterReward(order.UserId, order.Quota, c.ClientIP())
	if err != nil {
		logger.SysError(fmt.Sprintf("failed to process inviter reward, trade_no: %s, error: %s", payNotify.TradeNo, err.Error()))
	}
}

func CheckOrderStatus(c *gin.Context) {
	tradeNo := c.Query("trade_no")
	userId := c.GetInt("id")
	success := false

	if tradeNo != "" {
		order, err := model.GetUserOrder(userId, tradeNo)
		if err == nil {
			if order.Status == model.OrderStatusSuccess {
				success = true
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": success,
		"message": "",
	})
}

// discountMoney优惠金额 fee手续费，payMoney实付金额
func calculateOrderAmount(payment *model.Payment, amount int) (discountMoney, fee, payMoney float64) {
	// 获取折扣
	discount := common.GetRechargeDiscount(strconv.Itoa(amount))
	newMoney := float64(amount) * discount // 折后价值
	oldTotal := float64(amount)            //原价值
	if payment.PercentFee > 0 {
		//手续费=（原始价值*折扣*手续费率）
		fee = utils.Decimal(newMoney*payment.PercentFee, 2) //折后手续
		oldTotal = utils.Decimal(oldTotal*(1+payment.PercentFee), 2)
	} else if payment.FixedFee > 0 {
		//固定费率不计算折扣
		fee = payment.FixedFee
	}

	//实际费用=（折后价+折后手续费）*汇率*网关倍率
	total := utils.Decimal(newMoney+fee, 2)

	// 获取网关倍率，默认为1
	currencyRate := payment.CurrencyRate
	if currencyRate <= 0 {
		currencyRate = 1
	}

	if payment.Currency == model.CurrencyTypeUSD {
		oldTotal = utils.Decimal(oldTotal*currencyRate, 2)
		payMoney = utils.Decimal(total*currencyRate, 2)
	} else {
		oldTotal = utils.Decimal(oldTotal*config.PaymentUSDRate*currencyRate, 2)
		payMoney = utils.Decimal(total*config.PaymentUSDRate*currencyRate, 2)
	}
	discountMoney = oldTotal - payMoney //折扣金额 = 原价值-实际支付价值
	return
}

func GetOrderList(c *gin.Context) {
	var params model.SearchOrderParams
	if err := c.ShouldBindQuery(&params); err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}

	payments, err := model.GetOrderList(&params)
	if err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    payments,
	})
}

// EpayCallback 固定的易支付回调接口
func EpayCallback(c *gin.Context) {
	tradeNo := c.Query("out_trade_no")
	if tradeNo == "" {
		c.String(http.StatusOK, "fail")
		return
	}

	order, err := model.GetOrderByTradeNo(tradeNo)
	if err != nil {
		logger.SysError(fmt.Sprintf("epay callback failed to find order, trade_no: %s", tradeNo))
		c.String(http.StatusOK, "fail")
		return
	}

	gatewayPayment, err := model.GetPaymentByID(order.GatewayId)
	if err != nil {
		logger.SysError(fmt.Sprintf("epay callback failed to find payment, trade_no: %s, gateway_id: %d", tradeNo, order.GatewayId))
		c.String(http.StatusOK, "fail")
		return
	}

	paymentService, err := payment.NewPaymentService(gatewayPayment.UUID)
	if err != nil {
		logger.SysError(fmt.Sprintf("epay callback failed to create payment service, trade_no: %s", tradeNo))
		c.String(http.StatusOK, "fail")
		return
	}

	payNotify, err := paymentService.HandleCallback(c, paymentService.Payment.Config)
	if err != nil {
		return
	}

	LockOrder(payNotify.TradeNo)
	defer UnlockOrder(payNotify.TradeNo)

	completed, err := model.CompleteOrder(payNotify.TradeNo, payNotify.GatewayNo, order.UserId, order.Quota)
	if err != nil {
		logger.SysError(fmt.Sprintf("epay callback failed to update order, trade_no: %s", payNotify.TradeNo))
		return
	}
	if !completed {
		return
	}

	err = model.CheckAndUpgradeUserGroup(order.UserId, order.Quota)
	if err != nil {
		logger.SysError(fmt.Sprintf("epay callback failed to upgrade user group, trade_no: %s, error: %s", tradeNo, err.Error()))
	}

	model.RecordQuotaLog(order.UserId, model.LogTypeTopup, order.Quota, c.ClientIP(), fmt.Sprintf("在线充值成功，充值积分: %d，支付金额：%.2f %s", order.Quota, order.OrderAmount, order.OrderCurrency))

	err = model.ProcessInviterReward(order.UserId, order.Quota, c.ClientIP())
	if err != nil {
		logger.SysError(fmt.Sprintf("epay callback failed to process inviter reward, trade_no: %s, error: %s", tradeNo, err.Error()))
	}
}
