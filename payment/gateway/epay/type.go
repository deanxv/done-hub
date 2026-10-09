package epay

type PayType string

var (
	EpayPay PayType = ""       // 网关/收银台
	Alipay  PayType = "alipay" // 支付宝
	Wechat  PayType = "wxpay"  // 微信
	QQ      PayType = "qqpay"  // QQ
	Bank    PayType = "bank"   // 银行
	JD      PayType = "jdpay"  // 京东
	PayPal  PayType = "paypal" // PayPal
	USDT    PayType = "usdt"   // USDT
)

const (
	FormArgsSignType   = "MD5"
	FormSubmitUrl      = "/submit.php"
	TradeStatusSuccess = "TRADE_SUCCESS"
)

type PayArgs struct {
	Type       PayType `json:"type,omitempty"`
	OutTradeNo string  `json:"out_trade_no"`
	NotifyUrl  string  `json:"notify_url"`
	ReturnUrl  string  `json:"return_url"`
	Name       string  `json:"name"`
	Money      string  `json:"money"`
}
