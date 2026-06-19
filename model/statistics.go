package model

import (
	"done-hub/common"
	"fmt"
	"os"
	"strings"
	"time"
)

type Statistics struct {
	Date             time.Time `gorm:"primary_key;type:date" json:"date"`
	UserId           int       `json:"user_id" gorm:"primary_key"`
	ChannelId        int       `json:"channel_id" gorm:"primary_key"`
	ModelName        string    `json:"model_name" gorm:"primary_key;type:varchar(255)"`
	RequestCount     int       `json:"request_count"`
	Quota            int       `json:"quota"`
	CostQuota        int       `json:"cost_quota"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	RequestTime      int       `json:"request_time"`
}

func GetUserModelStatisticsByPeriod(userId int, startTime, endTime string) (LogStatistic []*LogStatisticGroupModel, err error) {
	dateStr := "date"
	if common.UsingPostgreSQL {
		dateStr = "TO_CHAR(date, 'YYYY-MM-DD') as date"
	} else if common.UsingSQLite {
		dateStr = "strftime('%Y-%m-%d', date) as date"
	} else {
		// MySQL/TiDB - 显式格式化日期以确保兼容性
		dateStr = "DATE_FORMAT(date, '%Y-%m-%d') as date"
	}

	err = DB.Raw(`
		SELECT `+dateStr+`,
		model_name, 
		sum(request_count) as request_count,
		sum(quota) as quota,
		sum(prompt_tokens) as prompt_tokens,
		sum(completion_tokens) as completion_tokens,
		sum(request_time) as request_time
		FROM statistics
		WHERE user_id= ?
		AND date BETWEEN ? AND ?
		GROUP BY date, model_name
		ORDER BY date, model_name
	`, userId, startTime, endTime).Scan(&LogStatistic).Error
	return
}

// UserGroupedStatistic 按用户分组的统计数据(不按模型分组)
type UserGroupedStatistic struct {
	Username         string `gorm:"column:username" json:"username"`
	RequestCount     int64  `gorm:"column:request_count" json:"request_count"`
	Quota            int64  `gorm:"column:quota" json:"quota"`
	PromptTokens     int64  `gorm:"column:prompt_tokens" json:"prompt_tokens"`
	CompletionTokens int64  `gorm:"column:completion_tokens" json:"completion_tokens"`
	RequestTime      int64  `gorm:"column:request_time" json:"request_time"`
}

// ModelUsageByUser 按用户和模型分组的使用统计
type ModelUsageByUser struct {
	Username     string `gorm:"column:username" json:"username"`
	ModelName    string `gorm:"column:model_name" json:"model_name"`
	RequestCount int64  `gorm:"column:request_count" json:"request_count"`
}

// GetUserGroupedStatisticsByPeriod 获取按用户分组的统计数据(不按模型分组)
func GetUserGroupedStatisticsByPeriod(usernames []string, startTime, endTime string) ([]*UserGroupedStatistic, error) {
	if len(usernames) == 0 {
		return nil, fmt.Errorf("usernames cannot be empty")
	}

	var statistics []*UserGroupedStatistic

	// Build SQL query - group by username only
	query := `
		SELECT 
			users.username,
			SUM(statistics.request_count) as request_count,
			SUM(statistics.quota) as quota,
			SUM(statistics.prompt_tokens) as prompt_tokens,
			SUM(statistics.completion_tokens) as completion_tokens,
			SUM(statistics.request_time) as request_time
		FROM statistics
		INNER JOIN users ON statistics.user_id = users.id
		WHERE users.username IN (?)
		AND statistics.date BETWEEN ? AND ?
		GROUP BY users.username
		ORDER BY users.username
	`

	err := DB.Raw(query, usernames, startTime, endTime).Scan(&statistics).Error
	if err != nil {
		return nil, err
	}

	if statistics == nil {
		statistics = []*UserGroupedStatistic{}
	}
	return statistics, nil
}

// GetModelUsageByUser 获取每个用户使用不同模型的调用次数
func GetModelUsageByUser(usernames []string, startTime, endTime string) ([]*ModelUsageByUser, error) {
	if len(usernames) == 0 {
		return nil, fmt.Errorf("usernames cannot be empty")
	}

	var usage []*ModelUsageByUser

	query := `
		SELECT 
			users.username,
			statistics.model_name,
			SUM(statistics.request_count) as request_count
		FROM statistics
		INNER JOIN users ON statistics.user_id = users.id
		WHERE users.username IN (?)
		AND statistics.date BETWEEN ? AND ?
		GROUP BY users.username, statistics.model_name
		ORDER BY users.username, request_count DESC
	`

	err := DB.Raw(query, usernames, startTime, endTime).Scan(&usage).Error
	if err != nil {
		return nil, err
	}

	if usage == nil {
		usage = []*ModelUsageByUser{}
	}
	return usage, nil
}

func GetChannelExpensesStatisticsByPeriod(startTime, endTime, groupType string, userID int, modelName string, channelID int) (LogStatistics []*LogStatisticGroupChannel, err error) {

	var whereClause strings.Builder
	whereClause.WriteString("WHERE date BETWEEN ? AND ?")
	args := []interface{}{startTime, endTime}

	if userID > 0 {
		whereClause.WriteString(" AND user_id = ?")
		args = append(args, userID)
	}

	if channelID > 0 {
		whereClause.WriteString(" AND channel_id = ?")
		args = append(args, channelID)
	}

	if modelName != "" {
		whereClause.WriteString(" AND model_name = ?")
		args = append(args, modelName)
	}

	dateStr := "date"
	if common.UsingPostgreSQL {
		dateStr = "TO_CHAR(date, 'YYYY-MM-DD') as date"
	} else if common.UsingSQLite {
		dateStr = "strftime('%%Y-%%m-%%d', date) as date"
	} else {
		// MySQL/TiDB - 显式格式化日期以确保兼容性
		dateStr = "DATE_FORMAT(date, '%%Y-%%m-%%d') as date"
	}

	baseSelect := `
        SELECT ` + dateStr + `,
        sum(request_count) as request_count,
        sum(quota) as quota,
        sum(cost_quota) as cost_quota,
        sum(prompt_tokens) as prompt_tokens,
        sum(completion_tokens) as completion_tokens,
        sum(request_time) as request_time,`

	var sql string
	if groupType == "model" {
		sql = baseSelect + `
            model_name as channel
            FROM statistics
            %s
            GROUP BY date, model_name
            ORDER BY date, model_name`
	} else if groupType == "model_type" {
		sql = baseSelect + `
            model_owned_by.name as channel
            FROM statistics
            JOIN prices ON statistics.model_name = prices.model
			JOIN model_owned_by ON prices.channel_type = model_owned_by.id
            %s
            GROUP BY date, model_owned_by.name
            ORDER BY date, model_owned_by.name`

	} else {
		sql = baseSelect + `
            MAX(channels.name) as channel
            FROM statistics
            JOIN channels ON statistics.channel_id = channels.id
            %s
            GROUP BY date, channel_id
            ORDER BY date, channel_id`
	}

	sql = fmt.Sprintf(sql, whereClause.String())
	err = DB.Raw(sql, args...).Scan(&LogStatistics).Error
	if err != nil {
		return nil, err
	}

	return LogStatistics, nil
}

type StatisticsUpdateType int

const (
	StatisticsUpdateTypeToDay     StatisticsUpdateType = 1
	StatisticsUpdateTypeYesterday StatisticsUpdateType = 2
	StatisticsUpdateTypeALL       StatisticsUpdateType = 3
)

func UpdateStatistics(updateType StatisticsUpdateType) error {
	sql := `
	%s statistics (date, user_id, channel_id, model_name, request_count, quota, cost_quota, prompt_tokens, completion_tokens, request_time)
	SELECT
		%s as date,
		user_id,
		channel_id,
		model_name,
		count(1) as request_count,
		sum(quota) as quota,
		sum(cost_quota) as cost_quota,
		sum(prompt_tokens) as prompt_tokens,
		sum(completion_tokens) as completion_tokens,
		sum(request_time) as request_time
	FROM logs
	WHERE
		type = 2
		%s
	GROUP BY date, channel_id, user_id, model_name
	ORDER BY date, model_name
	%s
	`

	sqlPrefix := ""
	sqlWhere := ""
	sqlDate := ""
	sqlSuffix := ""

	// 统一获取时区信息
	location := time.Local
	if tzEnv := os.Getenv("TZ"); tzEnv != "" {
		if loc, err := time.LoadLocation(tzEnv); err == nil {
			location = loc
		}
	}
	now := time.Now().In(location)
	_, offsetSeconds := now.Zone()

	// SQLite 需要特殊格式的偏移字符串
	getSqliteOffset := func() string {
		hours := offsetSeconds / 3600
		minutes := (offsetSeconds % 3600) / 60
		if hours >= 0 {
			offset := fmt.Sprintf("+%d hours", hours)
			if minutes != 0 {
				offset += fmt.Sprintf(" %d minutes", minutes)
			}
			return offset
		}
		offset := fmt.Sprintf("%d hours", hours)
		if minutes != 0 {
			offset += fmt.Sprintf(" %d minutes", -minutes)
		}
		return offset
	}

	if common.UsingSQLite {
		sqlPrefix = "INSERT OR REPLACE INTO"
		sqlDate = fmt.Sprintf("strftime('%%Y-%%m-%%d', datetime(created_at, 'unixepoch', '%s'))", getSqliteOffset())
		sqlSuffix = ""
	} else if common.UsingPostgreSQL {
		sqlPrefix = "INSERT INTO"
		tzName := "UTC"
		if tzEnv := os.Getenv("TZ"); tzEnv != "" {
			tzName = tzEnv
		}
		sqlDate = fmt.Sprintf("DATE_TRUNC('day', TO_TIMESTAMP(created_at) AT TIME ZONE '%s')::DATE", tzName)
		sqlSuffix = `ON CONFLICT (date, user_id, channel_id, model_name) DO UPDATE SET
		request_count = EXCLUDED.request_count,
		quota = EXCLUDED.quota,
		cost_quota = EXCLUDED.cost_quota,
		prompt_tokens = EXCLUDED.prompt_tokens,
		completion_tokens = EXCLUDED.completion_tokens,
		request_time = EXCLUDED.request_time`
	} else {
		sqlPrefix = "INSERT INTO"
		// MySQL: 检测 MySQL 时区，决定是否需要转换
		if isMySQLUsingUTC() {
			// MySQL 是 UTC，需要转换为本地时区
			hours := offsetSeconds / 3600
			minutes := (offsetSeconds % 3600) / 60
			var tzOffset string
			if hours >= 0 {
				tzOffset = fmt.Sprintf("+%02d:%02d", hours, minutes)
			} else {
				tzOffset = fmt.Sprintf("-%02d:%02d", -hours, -minutes)
			}
			sqlDate = fmt.Sprintf("DATE(CONVERT_TZ(FROM_UNIXTIME(created_at), '+00:00', '%s'))", tzOffset)
		} else {
			// MySQL 是本地时区（SYSTEM 或 +08:00 等），直接使用
			sqlDate = "DATE(FROM_UNIXTIME(created_at))"
		}
		sqlSuffix = `ON DUPLICATE KEY UPDATE
		request_count = VALUES(request_count),
		quota = VALUES(quota),
		cost_quota = VALUES(cost_quota),
		prompt_tokens = VALUES(prompt_tokens),
		completion_tokens = VALUES(completion_tokens),
		request_time = VALUES(request_time)`
	}

	todayTimestamp := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location).Unix()

	switch updateType {
	case StatisticsUpdateTypeToDay:
		sqlWhere = fmt.Sprintf("AND created_at >= %d", todayTimestamp)
	case StatisticsUpdateTypeYesterday:
		yesterdayTimestamp := todayTimestamp - 86400
		sqlWhere = fmt.Sprintf("AND created_at >= %d AND created_at < %d", yesterdayTimestamp, todayTimestamp)
	}

	err := DB.Exec(fmt.Sprintf(sql, sqlPrefix, sqlDate, sqlWhere, sqlSuffix)).Error
	return err
}
