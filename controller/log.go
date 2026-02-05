package controller

import (
	"done-hub/common"
	"done-hub/model"
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// 日志查询最大时间范围（天）
const MaxLogQueryDays = 180

// validateLogTimeRange 验证日志查询的时间范围
func validateLogTimeRange(startTimestamp, endTimestamp int64) error {
	if startTimestamp == 0 && endTimestamp == 0 {
		return nil
	}

	now := time.Now()
	maxDuration := time.Duration(MaxLogQueryDays) * 24 * time.Hour
	minAllowedTime := now.Add(-maxDuration)

	if startTimestamp > 0 {
		if time.Unix(startTimestamp, 0).Before(minAllowedTime) {
			return fmt.Errorf("开始时间不能早于 %d 天前", MaxLogQueryDays)
		}
	}

	if endTimestamp > 0 {
		if time.Unix(endTimestamp, 0).Before(minAllowedTime) {
			return fmt.Errorf("结束时间不能早于 %d 天前", MaxLogQueryDays)
		}
	}

	if startTimestamp > 0 && endTimestamp > 0 {
		if endTimestamp < startTimestamp {
			return fmt.Errorf("结束时间不能早于开始时间")
		}
		if time.Unix(endTimestamp, 0).Sub(time.Unix(startTimestamp, 0)) > maxDuration {
			return fmt.Errorf("查询时间跨度不能超过 %d 天", MaxLogQueryDays)
		}
	}

	return nil
}

// checkLogTimeRange 校验时间范围，失败时写入错误响应并返回 true
func checkLogTimeRange(c *gin.Context, startTimestamp, endTimestamp int64) bool {
	if err := validateLogTimeRange(startTimestamp, endTimestamp); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return true
	}
	return false
}

func GetLogsList(c *gin.Context) {
	var params model.LogsListParams
	if err := c.ShouldBindQuery(&params); err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}

	if checkLogTimeRange(c, params.StartTimestamp, params.EndTimestamp) {
		return
	}

	logs, err := model.GetLogsList(&params)
	if err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    logs,
	})
}

func GetUserLogsList(c *gin.Context) {
	userId := c.GetInt("id")

	var params model.LogsListParams
	if err := c.ShouldBindQuery(&params); err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}

	if checkLogTimeRange(c, params.StartTimestamp, params.EndTimestamp) {
		return
	}

	logs, err := model.GetUserLogsList(userId, &params)
	if err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    logs,
	})
}

func GetLogsStat(c *gin.Context) {
	// logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)

	if checkLogTimeRange(c, startTimestamp, endTimestamp) {
		return
	}

	tokenName := c.Query("token_name")
	username := c.Query("username")
	modelName := c.Query("model_name")
	channel, _ := strconv.Atoi(c.Query("channel"))
	quotaNum := model.SumUsedQuota(startTimestamp, endTimestamp, modelName, username, tokenName, channel)
	//tokenNum := model.SumUsedToken(logType, startTimestamp, endTimestamp, modelName, username, "")
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"quota": quotaNum,
			//"token": tokenNum,
		},
	})
}

func GetLogsSelfStat(c *gin.Context) {
	username := c.GetString("username")
	// logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)

	if checkLogTimeRange(c, startTimestamp, endTimestamp) {
		return
	}

	tokenName := c.Query("token_name")
	modelName := c.Query("model_name")
	channel, _ := strconv.Atoi(c.Query("channel"))
	quotaNum := model.SumUsedQuota(startTimestamp, endTimestamp, modelName, username, tokenName, channel)
	//tokenNum := model.SumUsedToken(logType, startTimestamp, endTimestamp, modelName, username, tokenName)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"quota": quotaNum,
			//"token": tokenNum,
		},
	})
}

func DeleteHistoryLogs(c *gin.Context) {
	targetTimestamp, _ := strconv.ParseInt(c.Query("target_timestamp"), 10, 64)
	if targetTimestamp == 0 {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "target timestamp is required",
		})
		return
	}
	count, err := model.DeleteOldLog(targetTimestamp)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    count,
	})
}

func ExportLogsList(c *gin.Context) {
	var params model.LogsListParams
	if err := c.ShouldBindQuery(&params); err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}

	if checkLogTimeRange(c, params.StartTimestamp, params.EndTimestamp) {
		return
	}

	// Get all matching records without pagination
	logs, err := model.GetAllLogsList(&params)
	if err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}

	// Set response headers for CSV download
	filename := fmt.Sprintf("logs_export_%s.csv", time.Now().Format("20060102_150405"))
	c.Header("Content-Type", "text/csv")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))

	// Create CSV writer
	writer := csv.NewWriter(c.Writer)
	defer writer.Flush()

	// Write CSV headers
	headers := []string{
		"时间", "渠道", "用户", "分组", "令牌", "类型", "模型",
		"耗时(秒)", "输入Token", "输出Token", "额度", "来源IP", "详情",
	}
	if err := writer.Write(headers); err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}

	// Write data rows
	for _, log := range logs {
		row := formatLogToCSVRow(log, true) // true for admin
		if err := writer.Write(row); err != nil {
			common.APIRespondWithError(c, http.StatusOK, err)
			return
		}
	}
}

func ExportUserLogsList(c *gin.Context) {
	userId := c.GetInt("id")

	var params model.LogsListParams
	if err := c.ShouldBindQuery(&params); err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}

	if checkLogTimeRange(c, params.StartTimestamp, params.EndTimestamp) {
		return
	}

	// Get all matching records without pagination
	logs, err := model.GetAllUserLogsList(userId, &params)
	if err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}

	// Set response headers for CSV download
	filename := fmt.Sprintf("logs_export_%s.csv", time.Now().Format("20060102_150405"))
	c.Header("Content-Type", "text/csv")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))

	// Create CSV writer
	writer := csv.NewWriter(c.Writer)
	defer writer.Flush()

	// Write CSV headers (without admin-only columns)
	headers := []string{
		"时间", "分组", "令牌", "类型", "模型",
		"耗时(秒)", "输入Token", "输出Token", "额度", "来源IP", "详情",
	}
	if err := writer.Write(headers); err != nil {
		common.APIRespondWithError(c, http.StatusOK, err)
		return
	}

	// Write data rows
	for _, log := range logs {
		row := formatLogToCSVRow(log, false) // false for regular user
		if err := writer.Write(row); err != nil {
			common.APIRespondWithError(c, http.StatusOK, err)
			return
		}
	}
}

func formatLogToCSVRow(log *model.Log, isAdmin bool) []string {
	// Format timestamp
	timeStr := time.Unix(log.CreatedAt, 0).Format("2006-01-02 15:04:05")

	// Format log type
	typeStr := getLogTypeText(log.Type)

	// Format channel info
	channelStr := ""
	if log.Channel != nil {
		channelStr = fmt.Sprintf("%d (%s)", log.ChannelId, log.Channel.Name)
	} else if log.ChannelId != 0 {
		channelStr = strconv.Itoa(log.ChannelId)
	}

	// Format duration
	durationStr := ""
	if log.RequestTime > 0 {
		durationStr = fmt.Sprintf("%.2f", float64(log.RequestTime)/1000.0)
	}

	// Format quota
	quotaStr := ""
	if log.Quota > 0 {
		quotaStr = fmt.Sprintf("%.6f", float64(log.Quota)/500000.0)
	} else {
		quotaStr = "0"
	}

	// Format tokens
	inputTokensStr := ""
	if log.PromptTokens > 0 {
		inputTokensStr = strconv.Itoa(log.PromptTokens)
	}

	outputTokensStr := ""
	if log.CompletionTokens > 0 {
		outputTokensStr = strconv.Itoa(log.CompletionTokens)
	}

	// Get group name from metadata
	groupStr := ""
	if metadataData := log.Metadata.Data(); metadataData != nil {
		if groupName, ok := metadataData["group_name"]; ok {
			if groupNameStr, ok := groupName.(string); ok {
				groupStr = groupNameStr
			}
		}
	}

	// Format detail content
	detailStr := formatLogDetail(log)

	if isAdmin {
		// Admin view includes channel and user columns
		return []string{
			timeStr,
			channelStr,
			log.Username,
			groupStr,
			log.TokenName,
			typeStr,
			log.ModelName,
			durationStr,
			inputTokensStr,
			outputTokensStr,
			quotaStr,
			log.SourceIp,
			detailStr,
		}
	} else {
		// User view excludes admin-only columns
		return []string{
			timeStr,
			groupStr,
			log.TokenName,
			typeStr,
			log.ModelName,
			durationStr,
			inputTokensStr,
			outputTokensStr,
			quotaStr,
			log.SourceIp,
			detailStr,
		}
	}
}

func getLogTypeText(logType int) string {
	switch logType {
	case model.LogTypeTopup:
		return "充值"
	case model.LogTypeConsume:
		return "消费"
	case model.LogTypeManage:
		return "管理"
	case model.LogTypeSystem:
		return "系统"
	default:
		return "未知"
	}
}

func formatLogDetail(log *model.Log) string {
	if metadataData := log.Metadata.Data(); metadataData != nil {
		// Check if we have price ratio information
		if inputRatio, hasInputRatio := metadataData["input_ratio"]; hasInputRatio {
			inputRatioFloat, ok := inputRatio.(float64)
			if !ok {
				// Try to convert from other numeric types
				if inputRatioInt, ok := inputRatio.(int); ok {
					inputRatioFloat = float64(inputRatioInt)
				} else {
					return log.Content
				}
			}

			// Get group discount ratio
			groupRatio := 1.0
			if groupRatioVal, ok := metadataData["group_ratio"]; ok {
				if groupRatioFloat, ok := groupRatioVal.(float64); ok {
					groupRatio = groupRatioFloat
				}
			}

			// Get price type
			priceType := ""
			if priceTypeVal, ok := metadataData["price_type"]; ok {
				if priceTypeStr, ok := priceTypeVal.(string); ok {
					priceType = priceTypeStr
				}
			}

			if priceType == "times" {
				// Calculate price for 'times' type
				price := calculatePrice(inputRatioFloat, groupRatio, true)
				return fmt.Sprintf("$%s / 次", price)
			} else {
				// Calculate prices for standard type
				inputPrice := calculatePrice(inputRatioFloat, groupRatio, false)

				// Get output ratio
				outputRatioFloat := 0.0
				if outputRatio, hasOutputRatio := metadataData["output_ratio"]; hasOutputRatio {
					if outputRatioVal, ok := outputRatio.(float64); ok {
						outputRatioFloat = outputRatioVal
					} else if outputRatioInt, ok := outputRatio.(int); ok {
						outputRatioFloat = float64(outputRatioInt)
					}
				}

				outputPrice := calculatePrice(outputRatioFloat, groupRatio, false)
				return fmt.Sprintf("输入: $%s /M\n输出: $%s /M", inputPrice, outputPrice)
			}
		}

		// Check for free consumption
		if log.Quota == 0 && log.Type == model.LogTypeConsume {
			return "免费"
		}
	}

	// Fallback to original content
	return log.Content
}

func calculatePrice(ratio, groupDiscount float64, isTimes bool) string {
	if ratio == 0 {
		ratio = 0
	}
	if groupDiscount == 0 {
		groupDiscount = 0
	}

	discount := ratio * groupDiscount

	if !isTimes {
		discount = discount * 1000
	}

	// Calculate the price: discount * 0.002
	price := discount * 0.002

	// Format with 6 decimal places and trim trailing zeros
	priceStr := fmt.Sprintf("%.6f", price)

	// Remove trailing zeros
	priceStr = strings.TrimRight(priceStr, "0")
	priceStr = strings.TrimRight(priceStr, ".")

	return priceStr
}
