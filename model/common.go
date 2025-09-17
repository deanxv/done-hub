package model

import (
	"fmt"
	"go-template/common"
	"go-template/common/config"
	"os"
	"strings"
	"time"

	"gorm.io/gorm"
)

type modelable interface {
	any
}

type GenericParams struct {
	PaginationParams
	Keyword string `form:"keyword"`
}

type PaginationParams struct {
	Page  int    `form:"page"`
	Size  int    `form:"size"`
	Order string `form:"order"`
}

type DataResult[T modelable] struct {
	Data       *[]*T `json:"data"`
	Page       int   `json:"page"`
	Size       int   `json:"size"`
	TotalCount int64 `json:"total_count"`
}

func PaginateAndOrder[T modelable](db *gorm.DB, params *PaginationParams, result *[]*T, allowedOrderFields map[string]bool) (*DataResult[T], error) {
	// 获取总数
	var totalCount int64
	err := db.Model(result).Count(&totalCount).Error
	if err != nil {
		return nil, err
	}

	// 分页
	if params.Page < 1 {
		params.Page = 1
	}
	if params.Size < 1 {
		params.Size = config.ItemsPerPage
	}

	if params.Size > config.MaxRecentItems {
		return nil, fmt.Errorf("size 参数不能超过 %d", config.MaxRecentItems)
	}

	offset := (params.Page - 1) * params.Size
	db = db.Offset(offset).Limit(params.Size)

	// 排序
	if params.Order != "" {
		orderFields := strings.Split(params.Order, ",")
		for _, field := range orderFields {
			field = strings.TrimSpace(field)
			desc := strings.HasPrefix(field, "-")
			if desc {
				field = field[1:]
			}
			if !allowedOrderFields[field] {
				return nil, fmt.Errorf("不允许对字段 '%s' 进行排序", field)
			}
			if desc {
				field = field + " DESC"
			}
			db = db.Order(field)
		}
	} else {
		// 默认排序
		db = db.Order("id DESC")
	}

	// 查询
	err = db.Find(result).Error
	if err != nil {
		return nil, err
	}

	// 返回结果
	return &DataResult[T]{
		Data:       result,
		Page:       params.Page,
		Size:       params.Size,
		TotalCount: totalCount,
	}, nil
}

func getDateFormat(groupType string) string {
	var dateFormat string
	if groupType == "day" {
		dateFormat = "%Y-%m-%d"
		if common.UsingPostgreSQL {
			dateFormat = "YYYY-MM-DD"
		}
	} else {
		dateFormat = "%Y-%m"
		if common.UsingPostgreSQL {
			dateFormat = "YYYY-MM"
		}
	}
	return dateFormat
}

func getTimestampGroupsSelect(fieldName, groupType, alias string) string {
	dateFormat := getDateFormat(groupType)
	var groupSelect string

	// 获取系统时区偏移
	getTimezoneOffset := func() (string, string) {
		// 优先使用系统本地时区（Docker中通过TZ环境变量设置）
		location := time.Local

		// 也可以通过环境变量TZ覆盖
		if tzEnv := os.Getenv("TZ"); tzEnv != "" {
			if loc, err := time.LoadLocation(tzEnv); err == nil {
				location = loc
			}
		}

		// 获取当前时间在指定时区的偏移量
		now := time.Now().In(location)
		_, offset := now.Zone()

		// 计算小时偏移
		hours := offset / 3600
		minutes := (offset % 3600) / 60

		// 生成不同数据库需要的格式
		var sqliteOffset, mysqlOffset string
		if hours >= 0 {
			sqliteOffset = fmt.Sprintf("+%d hours", hours)
			if minutes != 0 {
				sqliteOffset += fmt.Sprintf(" %d minutes", minutes)
			}
			mysqlOffset = fmt.Sprintf("+%02d:%02d", hours, minutes)
		} else {
			sqliteOffset = fmt.Sprintf("%d hours", hours) // 负数自带减号
			if minutes != 0 {
				sqliteOffset += fmt.Sprintf(" %d minutes", -minutes) // 分钟也要是负数
			}
			mysqlOffset = fmt.Sprintf("-%02d:%02d", -hours, -minutes)
		}

		return sqliteOffset, mysqlOffset
	}

	if common.UsingPostgreSQL {
		// PostgreSQL: 使用系统时区或TZ环境变量
		tzName := "UTC"
		if tzEnv := os.Getenv("TZ"); tzEnv != "" {
			tzName = tzEnv
		} else {
			// 尝试获取系统时区名称
			now := time.Now()
			zone, _ := now.Zone()
			if zone != "" {
				tzName = zone
			}
		}
		groupSelect = fmt.Sprintf(`TO_CHAR(date_trunc('%s', to_timestamp(%s) AT TIME ZONE '%s'), '%s') as %s`, groupType, fieldName, tzName, dateFormat, alias)
	} else if common.UsingSQLite {
		// SQLite: 动态计算时区偏移
		sqliteOffset, _ := getTimezoneOffset()
		groupSelect = fmt.Sprintf(`strftime('%s', datetime(%s, 'unixepoch', '%s')) as %s`, dateFormat, fieldName, sqliteOffset, alias)
	} else {
		// MySQL: 动态计算时区偏移
		_, mysqlOffset := getTimezoneOffset()
		groupSelect = fmt.Sprintf(`DATE_FORMAT(CONVERT_TZ(FROM_UNIXTIME(%s), '+00:00', '%s'), '%s') as %s`, fieldName, mysqlOffset, dateFormat, alias)
	}

	return groupSelect
}

func quotePostgresField(field string) string {
	if common.UsingPostgreSQL {
		return fmt.Sprintf(`"%s"`, field)
	}

	return fmt.Sprintf("`%s`", field)
}

func assembleSumSelectStr(selectStr string) string {
	sumSelectStr := "%s(sum(%s),0)"
	nullfunc := "ifnull"
	if common.UsingPostgreSQL {
		nullfunc = "coalesce"
	}

	sumSelectStr = fmt.Sprintf(sumSelectStr, nullfunc, selectStr)

	return sumSelectStr
}

func RecordExists(table interface{}, fieldName string, fieldValue interface{}, excludeID interface{}) bool {
	var count int64
	query := DB.Model(table).Where(fmt.Sprintf("%s = ?", fieldName), fieldValue)
	if excludeID != nil {
		query = query.Not("id", excludeID)
	}
	query.Count(&count)
	return count > 0
}

// RecordExistsWithTx 在指定事务中检查记录是否存在
func RecordExistsWithTx(tx *gorm.DB, table interface{}, fieldName string, fieldValue interface{}, excludeID interface{}) bool {
	var count int64
	query := tx.Model(table).Where(fmt.Sprintf("%s = ?", fieldName), fieldValue)
	if excludeID != nil {
		query = query.Not("id", excludeID)
	}
	query.Count(&count)
	return count > 0
}

func GetFieldsByID(model interface{}, fieldNames []string, id int, result interface{}) error {
	err := DB.Model(model).Where("id = ?", id).Select(fieldNames).Find(result).Error
	return err
}

func UpdateFieldsByID(model interface{}, id int, fields map[string]interface{}) error {
	err := DB.Model(model).Where("id = ?", id).Updates(fields).Error
	return err
}
