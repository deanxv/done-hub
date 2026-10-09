package epay

import (
	"done-hub/common/logger"
	"done-hub/model"
	"done-hub/payment/types"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

type Epay struct{}

type EpayConfig struct {
	PayType PayType `json:"pay_type"`
	Client
}

func (e *Epay) Name() string {
	return "易支付"
}

func (e *Epay) Pay(config *types.PayConfig, gatewayConfig string) (*types.PayRequest, error) {
	epayConfig, err := getEpayConfig(gatewayConfig)
	if err != nil {
		return nil, err
	}

	payArgs := &PayArgs{
		Type:       epayConfig.PayType,
		OutTradeNo: config.TradeNo,
		NotifyUrl:  config.NotifyURL,
		ReturnUrl:  config.ReturnURL,
		Name:       config.TradeNo,
		Money:      strconv.FormatFloat(config.Money, 'f', 2, 64),
	}

	formPayURL, formPayArgs, err := epayConfig.FormPay(payArgs)
	if err != nil {
		return nil, err
	}

	payRequest := &types.PayRequest{
		Type: 1,
		Data: types.PayRequestData{
			URL:    formPayURL,
			Params: &formPayArgs,
			Method: http.MethodPost,
		},
	}

	return payRequest, nil
}

func (e *Epay) HandleCallback(c *gin.Context, gatewayConfig string) (*types.PayNotify, error) {
	queryMap := make(map[string]string)
	for key, values := range c.Request.URL.Query() {
		if len(values) > 0 {
			queryMap[key] = values[0]
		}
	}

	if err := validateNotifyParams(queryMap); err != nil {
		logger.SysError(fmt.Sprintf("epay callback params invalid: %v, params: %+v", err, queryMap))
		c.Writer.Write([]byte("fail"))
		return nil, fmt.Errorf("invalid callback params: %w", err)
	}

	epayConfig, err := getEpayConfig(gatewayConfig)
	if err != nil {
		c.Writer.Write([]byte("fail"))
		return nil, fmt.Errorf("tradeNo: %s, err: %v", queryMap["out_trade_no"], err)
	}

	if err := epayConfig.verifySignature(queryMap); err != nil {
		logger.SysError(fmt.Sprintf("epay signature verification failed: %v, params: %+v", err, queryMap))
		c.Writer.Write([]byte("fail"))
		return nil, fmt.Errorf("tradeNo: %s, signature verification failed", queryMap["out_trade_no"])
	}

	if queryMap["trade_status"] != TradeStatusSuccess {
		c.Writer.Write([]byte("fail"))
		return nil, fmt.Errorf("tradeNo: %s, invalid trade status: %s", queryMap["out_trade_no"], queryMap["trade_status"])
	}

	c.Writer.Write([]byte("success"))
	return &types.PayNotify{
		TradeNo:   queryMap["out_trade_no"],
		GatewayNo: queryMap["trade_no"],
	}, nil
}

func getEpayConfig(gatewayConfig string) (*EpayConfig, error) {
	var epayConfig EpayConfig
	if err := json.Unmarshal([]byte(gatewayConfig), &epayConfig); err != nil {
		return nil, errors.New("config error")
	}

	return &epayConfig, nil
}
func (e *Epay) CreatedPay(_ string, _ *model.Payment) error {
	return nil
}
