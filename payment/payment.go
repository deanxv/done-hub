package payment

import (
	"go-template/model"
	"go-template/payment/gateway/alipay"
	"go-template/payment/gateway/epay"
	"go-template/payment/gateway/stripe"
	"go-template/payment/gateway/wxpay"
	"go-template/payment/types"

	"github.com/gin-gonic/gin"
)

type PaymentProcessor interface {
	Name() string
	Pay(config *types.PayConfig, gatewayConfig string) (*types.PayRequest, error)
	CreatedPay(notifyURL string, gatewayConfig *model.Payment) error
	HandleCallback(c *gin.Context, gatewayConfig string) (*types.PayNotify, error)
}

var Gateways = make(map[string]PaymentProcessor)

func init() {
	Gateways["epay"] = &epay.Epay{}
	Gateways["alipay"] = &alipay.Alipay{}
	Gateways["wxpay"] = &wxpay.WeChatPay{}
	Gateways["stripe"] = &stripe.Stripe{}
}
