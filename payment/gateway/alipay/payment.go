package alipay

import (
	"encoding/json"
	"errors"
	"fmt"
	"go-template/model"
	"go-template/payment/types"

	"github.com/gin-gonic/gin"
	"github.com/smartwalle/alipay/v3"
)

type Alipay struct{}

type AlipayConfig struct {
	AppID      string  `json:"app_id"`
	PrivateKey string  `json:"private_key"`
	PublicKey  string  `json:"public_key"`
	PayType    PayType `json:"pay_type"`
}

const isProduction bool = true

func (a *Alipay) Name() string {
	return "支付宝"
}

// createClient 创建支付宝客户端，每次调用都创建新实例
func (a *Alipay) createClient(config *AlipayConfig) (*alipay.Client, error) {
	client, err := alipay.New(config.AppID, config.PrivateKey, isProduction)
	if err != nil {
		return nil, err
	}
	err = client.LoadAliPayPublicKey(config.PublicKey)
	if err != nil {
		return nil, err
	}
	return client, nil
}

func (a *Alipay) Pay(config *types.PayConfig, gatewayConfig string) (*types.PayRequest, error) {
	alipayConfig, err := getAlipayConfig(gatewayConfig)
	if err != nil {
		return nil, err
	}

	client, err := a.createClient(alipayConfig)
	if err != nil {
		return nil, err
	}

	switch alipayConfig.PayType {
	case PagePay:
		return a.handlePagePay(config, client)
	case WapPay:
		return a.handleWapPay(config, client)
	default:
		return a.handleTradePreCreate(config, client)
	}
}

func (a *Alipay) HandleCallback(c *gin.Context, gatewayConfig string) (*types.PayNotify, error) {
	alipayConfig, err := getAlipayConfig(gatewayConfig)
	if err != nil {
		c.Writer.Write([]byte("failure"))
		return nil, err
	}

	client, err := a.createClient(alipayConfig)
	if err != nil {
		c.Writer.Write([]byte("failure"))
		return nil, err
	}

	// 获取通知参数
	params := c.Request.URL.Query()
	if err := c.Request.ParseForm(); err != nil {
		c.Writer.Write([]byte("failure"))
		return nil, fmt.Errorf("Alipay params failed: %v", err)
	}
	for k, v := range c.Request.PostForm {
		params[k] = v
	}
	// 验证通知签名
	if err := client.VerifySign(params); err != nil {
		c.Writer.Write([]byte("failure"))
		return nil, fmt.Errorf("Alipay Signature verification failed: %v", err)
	}
	//解析通知内容
	noti, err := client.DecodeNotification(params)
	if err != nil {
		c.Writer.Write([]byte("failure"))
		return nil, fmt.Errorf("Alipay Error decoding notification: %v", err)
	}

	if noti.TradeStatus == alipay.TradeStatusSuccess {
		payNotify := &types.PayNotify{
			TradeNo:   noti.OutTradeNo,
			GatewayNo: noti.TradeNo,
		}
		alipay.ACKNotification(c.Writer)
		return payNotify, nil
	}
	c.Writer.Write([]byte("failure"))
	return nil, fmt.Errorf("trade status not success")
}

func getAlipayConfig(gatewayConfig string) (*AlipayConfig, error) {
	var alipayConfig AlipayConfig
	if err := json.Unmarshal([]byte(gatewayConfig), &alipayConfig); err != nil {
		return nil, errors.New("config error")
	}

	return &alipayConfig, nil
}

func (a *Alipay) CreatedPay(_ string, _ *model.Payment) error {
	return nil
}
