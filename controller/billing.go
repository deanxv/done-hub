package controller

import (
	"done-hub/common"
	"done-hub/common/config"
	"done-hub/model"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

// getUserAccountQuota 读取账户级的剩余/已用额度，供 DisplayTokenStatEnabled 关闭时按账户口径返回。
func getUserAccountQuota(c *gin.Context) (remainQuota int, usedQuota int, err error) {
	userData, err := model.GetUserFields(c.GetInt("id"), []string{"quota", "used_quota"})
	if err != nil {
		return 0, 0, err
	}
	return userData["quota"].(int), userData["used_quota"].(int), nil
}

func GetSubscription(c *gin.Context) {
	var remainQuota int
	var usedQuota int
	var err error
	var expiredTime int64

	if config.DisplayTokenStatEnabled {
		var token *model.Token
		token, err = model.GetTokenById(c.GetInt("token_id"))
		if err != nil {
			common.APIRespondWithError(c, http.StatusOK, fmt.Errorf("获取信息失败: %v", err))
			return
		}
		if token.UnlimitedQuota {
			remainQuota, usedQuota, err = getUserAccountQuota(c)
		} else {
			expiredTime = token.ExpiredTime
			remainQuota = token.RemainQuota
			usedQuota = token.UsedQuota
		}
	} else {
		remainQuota, usedQuota, err = getUserAccountQuota(c)
	}
	if err != nil {
		common.APIRespondWithError(c, http.StatusOK, fmt.Errorf("获取用户信息失败: %v", err))
		return
	}

	if expiredTime <= 0 {
		expiredTime = 0
	}

	quota := remainQuota + usedQuota
	amount := float64(quota)
	if config.DisplayInCurrencyEnabled {
		amount /= config.QuotaPerUnit
	}

	subscription := OpenAISubscriptionResponse{
		Object:             "billing_subscription",
		HasPaymentMethod:   true,
		SoftLimitUSD:       amount,
		HardLimitUSD:       amount,
		SystemHardLimitUSD: amount,
		AccessUntil:        expiredTime,
	}
	c.JSON(200, subscription)
}

func GetUsage(c *gin.Context) {
	var quota int
	var err error

	if config.DisplayTokenStatEnabled {
		var token *model.Token
		token, err = model.GetTokenById(c.GetInt("token_id"))
		if err != nil {
			common.APIRespondWithError(c, http.StatusOK, fmt.Errorf("获取信息失败: %v", err))
			return
		}
		if token.UnlimitedQuota {
			_, quota, err = getUserAccountQuota(c)
		} else {
			quota = token.UsedQuota
		}
	} else {
		_, quota, err = getUserAccountQuota(c)
	}
	if err != nil {
		common.APIRespondWithError(c, http.StatusOK, fmt.Errorf("获取用户信息失败: %v", err))
		return
	}

	amount := float64(quota)
	if config.DisplayInCurrencyEnabled {
		amount /= config.QuotaPerUnit
	}
	usage := OpenAIUsageResponse{
		Object:     "list",
		TotalUsage: amount * 100,
	}
	c.JSON(200, usage)
}
