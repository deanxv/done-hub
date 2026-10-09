package epay

import (
	"crypto/md5"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
)

type Client struct {
	PayDomain string `json:"pay_domain"`
	PartnerID string `json:"partner_id"`
	Key       string `json:"key"`
}

// FormPay 表单支付
func (c *Client) FormPay(args *PayArgs) (string, map[string]string, error) {
	formPayArgs := map[string]string{
		"pid":          c.PartnerID,
		"out_trade_no": args.OutTradeNo,
		"notify_url":   args.NotifyUrl,
		"return_url":   args.ReturnUrl,
		"name":         args.Name,
		"money":        args.Money,
	}

	if args.Type != "" {
		formPayArgs["type"] = string(args.Type)
	}

	formPayArgs["sign"] = c.Sign(formPayArgs)
	formPayArgs["sign_type"] = FormArgsSignType

	domain := strings.TrimSuffix(c.PayDomain, "/")

	return domain + FormSubmitUrl, formPayArgs, nil

}

// notifyForbiddenParams 只会出现在下单请求里，真实回调不会携带。
// 回调里带上它们说明请求是拿下单签名拼出来的，直接拒绝。
// 这里不用白名单：各家易支付分支会多带字段且一并签名，白名单会误杀已付款的回调。
var notifyForbiddenParams = []string{"notify_url", "return_url"}

func validateNotifyParams(params map[string]string) error {
	for _, key := range notifyForbiddenParams {
		if _, ok := params[key]; ok {
			return fmt.Errorf("unexpected callback parameter: %s", key)
		}
	}

	// 拒绝任何参数值含 & 的回调。
	// 易支付签名串是把所有参数值直接拼接的，值里含 & 会让边界混淆，
	// 使得下单参数可以通过某个字段的值伪装成顶层回调参数。
	// 真实回调的值（trade_no、money、name 等）不会含 &。
	for key, value := range params {
		if strings.Contains(value, "&") {
			return fmt.Errorf("parameter %q contains invalid character '&'", key)
		}
	}

	return nil
}

func (c *Client) verifySignature(params map[string]string) error {
	sign := params["sign"]
	if sign == "" {
		return errors.New("missing signature")
	}

	if subtle.ConstantTimeCompare([]byte(sign), []byte(c.Sign(params))) != 1 {
		return errors.New("invalid signature")
	}

	return nil
}

// Sign 签名
func (c *Client) Sign(args map[string]string) string {
	keys := make([]string, 0, len(args))
	for k := range args {
		if k != "sign" && k != "sign_type" && args[k] != "" {
			keys = append(keys, k)
		}
	}

	sort.Strings(keys)

	signStrs := make([]string, len(keys))
	for i, k := range keys {
		signStrs[i] = k + "=" + args[k]
	}

	signStr := strings.Join(signStrs, "&") + c.Key

	h := md5.New()
	h.Write([]byte(signStr))

	return hex.EncodeToString(h.Sum(nil))
}
