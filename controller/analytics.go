package controller

import (
	"done-hub/model"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// getCurrencySymbol 获取货币符号
func getCurrencySymbol(currency string) string {
	switch currency {
	case "CNY":
		return "¥"
	case "USD":
		return "$"
	default:
		return currency + ": "
	}
}

type StatisticsByPeriod struct {
	UserStatistics       []*model.UserStatisticsByPeriod    `json:"user_statistics"`
	ChannelStatistics    []*model.LogStatisticGroupChannel  `json:"channel_statistics"`
	RedemptionStatistics []*model.RedemptionStatisticsGroup `json:"redemption_statistics"`
	OrderStatistics      []*model.OrderStatisticsGroup      `json:"order_statistics"`
}

func GetStatisticsByPeriod(c *gin.Context) {
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	groupType := c.Query("group_type")
	userID, _ := strconv.Atoi(c.Query("user_id"))
	modelName := c.Query("model_name")
	channelID, _ := strconv.Atoi(c.Query("channel_id"))

	statisticsByPeriod := &StatisticsByPeriod{}

	userStatistics, err := model.GetUserStatisticsByPeriod(startTimestamp, endTimestamp)
	if err == nil {
		statisticsByPeriod.UserStatistics = userStatistics
	}

	startTime := time.Unix(startTimestamp, 0)
	endTime := time.Unix(endTimestamp, 0)
	startDate := startTime.Format("2006-01-02")
	endDate := endTime.Format("2006-01-02")
	channelStatistics, err := model.GetChannelExpensesStatisticsByPeriod(startDate, endDate, groupType, userID, modelName, channelID)

	if err == nil {
		statisticsByPeriod.ChannelStatistics = channelStatistics
	}

	redemptionStatistics, err := model.GetStatisticsRedemptionByPeriod(startTimestamp, endTimestamp)
	if err == nil {
		statisticsByPeriod.RedemptionStatistics = redemptionStatistics
	}

	orderStatistics, err := model.GetStatisticsOrderByPeriod(startTimestamp, endTimestamp)
	if err == nil {
		statisticsByPeriod.OrderStatistics = orderStatistics
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    statisticsByPeriod,
	})
}

type StatisticsDetail struct {
	UserStatistics      *model.StatisticsUser         `json:"user_statistics"`
	ChannelStatistics   []*model.ChannelStatistics    `json:"channel_statistics"`
	RedemptionStatistic []*model.RedemptionStatistics `json:"redemption_statistic"`
	OrderStatistics     []*model.OrderStatistics      `json:"order_statistics"`
	RpmTpmStatistics    *RpmTpmStatistics             `json:"rpm_tpm_statistics"`
}

type RpmTpmStatistics struct {
	RPM int64   `json:"rpm"`
	TPM int64   `json:"tpm"`
	CPM float64 `json:"cpm"` // Cost Per Minute (美元)
	PPM float64 `json:"ppm"` // Profit Per Minute (美元)
}

func GetStatisticsDetail(c *gin.Context) {

	statisticsDetail := &StatisticsDetail{}
	userStatistics, err := model.GetStatisticsUser()
	if err == nil {
		statisticsDetail.UserStatistics = userStatistics
	}

	channelStatistics, err := model.GetStatisticsChannel()
	if err == nil {
		statisticsDetail.ChannelStatistics = channelStatistics
	}

	redemptionStatistics, err := model.GetStatisticsRedemption()
	if err == nil {
		statisticsDetail.RedemptionStatistic = redemptionStatistics
	}

	orderStatistics, err := model.GetStatisticsOrder()
	if err == nil {
		statisticsDetail.OrderStatistics = orderStatistics
	}

	// 获取最近60秒的RPM和TPM统计
	rpmTpmStats, err := model.GetRpmTpmStatistics()
	if err == nil {
		statisticsDetail.RpmTpmStatistics = &RpmTpmStatistics{
			RPM: rpmTpmStats.RPM,
			TPM: rpmTpmStats.TPM,
			CPM: rpmTpmStats.CPM,
			PPM: rpmTpmStats.PPM,
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    statisticsDetail,
	})
}

// RechargeStatistics 充值统计响应结构
type RechargeStatistics struct {
	Total             int64  `json:"total"`               // 总充值金额（quota）
	RedemptionAmount  int64  `json:"redemption_amount"`   // 兑换码充值金额
	OrderAmount       int64  `json:"order_amount"`        // 订单充值金额
	OrderCurrencyInfo string `json:"order_currency_info"` // 订单货币信息
}

// GetRechargeStatisticsByTimeRange 获取指定时间范围的充值统计
// 支持的时间范围: all, year, month, week, day
func GetRechargeStatisticsByTimeRange(c *gin.Context) {
	timeRange := c.Query("time_range") // all, year, month, week, day
	if timeRange == "" {
		timeRange = "month" // 默认本月
	}

	var startTimestamp, endTimestamp int64

	// 后端计算时间范围
	if timeRange != "all" {
		// 优先使用系统本地时区（Docker中通过TZ环境变量设置）
		// 如果Docker设置了TZ=Asia/Shanghai，time.Local会自动使用该时区
		location := time.Local

		// 也可以通过环境变量TZ覆盖，如果有需要的话
		if tzEnv := os.Getenv("TZ"); tzEnv != "" {
			if loc, err := time.LoadLocation(tzEnv); err == nil {
				location = loc
			}
		}

		now := time.Now().In(location)

		switch timeRange {
		case "year":
			startTimestamp = time.Date(now.Year(), 1, 1, 0, 0, 0, 0, location).Unix()
			endTimestamp = time.Date(now.Year(), 12, 31, 23, 59, 59, 0, location).Unix()
		case "month":
			startTimestamp = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, location).Unix()
			endTimestamp = time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, location).Add(-time.Second).Unix()
		case "week":
			// 计算本周开始（周一）
			weekday := now.Weekday()
			if weekday == 0 { // 周日
				weekday = 7
			}
			startOfWeek := now.AddDate(0, 0, -int(weekday-1))
			startTimestamp = time.Date(startOfWeek.Year(), startOfWeek.Month(), startOfWeek.Day(), 0, 0, 0, 0, location).Unix()
			endTimestamp = time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 0, location).Unix()
		case "day":
			startTimestamp = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location).Unix()
			endTimestamp = time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 0, location).Unix()
		default:
			// 无效的时间范围，返回错误
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": fmt.Sprintf("无效的时间范围: %s", timeRange),
			})
			return
		}
	}

	// 初始化结果结构，确保所有字段都有默认值
	rechargeStats := &RechargeStatistics{
		Total:             0,
		RedemptionAmount:  0,
		OrderAmount:       0,
		OrderCurrencyInfo: "",
	}

	// 获取充值统计数据
	if timeRange == "all" {
		// 全部数据
		redemptionStats, err := model.GetStatisticsRedemption()
		if err != nil {
			// 记录错误但不中断，继续处理其他数据
			fmt.Printf("获取兑换码统计失败: %v\n", err)
		} else {
			// 安全遍历，即使slice为空也不会出错
			for _, stat := range redemptionStats {
				if stat != nil && stat.Status == 3 { // 已使用的兑换码
					rechargeStats.RedemptionAmount += stat.Quota
				}
			}
		}

		orderStats, err := model.GetStatisticsOrder()
		if err != nil {
			// 记录错误但不中断
			fmt.Printf("获取订单统计失败: %v\n", err)
		} else {
			currencies := make(map[string]float64)
			// 安全遍历，即使slice为空也不会出错
			for _, stat := range orderStats {
				if stat != nil {
					rechargeStats.OrderAmount += stat.Quota
					currencies[stat.OrderCurrency] = currencies[stat.OrderCurrency] + stat.Money
				}
			}

			// 构建货币信息字符串 - 安全处理空map
			if len(currencies) > 0 {
				var currencyInfo []string
				for currency, amount := range currencies {
					currencyInfo = append(currencyInfo, fmt.Sprintf("%s%.2f", getCurrencySymbol(currency), amount))
				}
				rechargeStats.OrderCurrencyInfo = strings.Join(currencyInfo, " ")
			}
		}
	} else {
		// 指定时间范围数据
		redemptionStats, err := model.GetStatisticsRedemptionByPeriod(startTimestamp, endTimestamp)
		if err != nil {
			// 记录错误但不中断
			fmt.Printf("获取时间范围兑换码统计失败: %v\n", err)
		} else {
			// 安全遍历
			for _, stat := range redemptionStats {
				if stat != nil {
					rechargeStats.RedemptionAmount += stat.Quota
				}
			}
		}

		orderStats, err := model.GetStatisticsOrderByPeriod(startTimestamp, endTimestamp)
		if err != nil {
			// 记录错误但不中断
			fmt.Printf("获取时间范围订单统计失败: %v\n", err)
		} else {
			currencies := make(map[string]float64)
			// 安全遍历
			for _, stat := range orderStats {
				if stat != nil {
					rechargeStats.OrderAmount += stat.Quota
					currencies[stat.OrderCurrency] = currencies[stat.OrderCurrency] + stat.Money
				}
			}

			// 构建货币信息字符串 - 安全处理空map
			if len(currencies) > 0 {
				var currencyInfo []string
				for currency, amount := range currencies {
					currencyInfo = append(currencyInfo, fmt.Sprintf("%s%.2f", getCurrencySymbol(currency), amount))
				}
				rechargeStats.OrderCurrencyInfo = strings.Join(currencyInfo, " ")
			}
		}
	}

	// 计算总计
	rechargeStats.Total = rechargeStats.RedemptionAmount + rechargeStats.OrderAmount

	// 确保始终返回成功响应，即使某些查询失败
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    rechargeStats,
	})
}
