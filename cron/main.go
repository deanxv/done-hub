package cron

import (
	"done-hub/common/config"
	"done-hub/common/logger"
	"done-hub/common/scheduler"
	"done-hub/model"
	"fmt"
	"github.com/spf13/viper"
	"time"

	"github.com/go-co-op/gocron/v2"
)

func InitCron() {
	if !config.IsMasterNode {
		logger.SysLog("Cron is disabled on slave node")
		return
	}

	// 添加每日统计任务
	err := scheduler.Manager.AddJob(
		"update_daily_statistics",
		gocron.DailyJob(
			1,
			gocron.NewAtTimes(
				gocron.NewAtTime(0, 0, 30),
			)),
		gocron.NewTask(func() {
			model.UpdateStatistics(model.StatisticsUpdateTypeYesterday)
			logger.SysLog("更新昨日统计数据")
		}),
	)
	if err != nil {
		logger.SysError("Cron job error: " + err.Error())
		return
	}

	if config.UserInvoiceMonth {
		// 每月一号早上四点生成上个月的账单数据
		err = scheduler.Manager.AddJob(
			"generate_statistics_month",
			gocron.DailyJob(1, gocron.NewAtTimes(gocron.NewAtTime(4, 0, 0))),
			gocron.NewTask(func() {
				err := model.InsertStatisticsMonth()
				if err != nil {
					logger.SysError("Generate statistics month data error:" + err.Error())
				}
			}),
		)
	}

	// 每十分钟更新一次统计数据
	err = scheduler.Manager.AddJob(
		"update_statistics",
		gocron.DurationJob(10*time.Minute),
		gocron.NewTask(func() {
			model.UpdateStatistics(model.StatisticsUpdateTypeToDay)
			logger.SysLog("10分钟统计数据")
		}),
	)

	// 每天凌晨 3:00 自动清理过期消费日志
	err = scheduler.Manager.AddJob(
		"log_auto_delete",
		gocron.DailyJob(1, gocron.NewAtTimes(gocron.NewAtTime(3, 0, 0))),
		gocron.NewTask(func() {
			if !config.LogAutoDeleteEnabled || config.LogAutoDeleteDays <= 0 {
				return
			}
			targetTimestamp := time.Now().AddDate(0, 0, -config.LogAutoDeleteDays).Unix()
			const batchSize = 10000
			var totalDeleted int64
			for {
				affected, err := model.DeleteOldLogBatch(targetTimestamp, batchSize)
				if err != nil {
					logger.SysError(fmt.Sprintf("[cron] 消费日志自动清理失败，已删 %d 行: %v", totalDeleted, err))
					break
				}
				totalDeleted += affected
				if affected == 0 {
					break
				}
			}
			if totalDeleted > 0 {
				logger.SysLog(fmt.Sprintf("[cron] 消费日志自动清理完成，共删除 %d 行", totalDeleted))
			}
		}),
	)
	if err != nil {
		logger.SysError("Cron job error: " + err.Error())
		return
	}

	// 开启自动更新 并且设置了有效自动更新时间 同时自动更新模式不是system 则会从服务器拉取最新价格表
	autoPriceUpdatesInterval := viper.GetInt("auto_price_updates_interval")
	autoPriceUpdates := viper.GetBool("auto_price_updates")
	autoPriceUpdatesMode := viper.GetString("auto_price_updates_mode")

	if autoPriceUpdates &&
		autoPriceUpdatesInterval > 0 &&
		(autoPriceUpdatesMode == string(model.PriceUpdateModeAdd) ||
			autoPriceUpdatesMode == string(model.PriceUpdateModeOverwrite) ||
			autoPriceUpdatesMode == string(model.PriceUpdateModeUpdate)) {
		// 指定时间周期更新价格表
		err := scheduler.Manager.AddJob(
			"update_pricing_by_service",
			gocron.DurationJob(time.Duration(autoPriceUpdatesInterval)*time.Minute),
			gocron.NewTask(func() {
				err := model.UpdatePriceByPriceService()
				if err != nil {
					logger.SysError("Update Price Error: " + err.Error())
					return
				}
				logger.SysLog("Update Price Done")
			}),
		)
		if err != nil {
			logger.SysError("Cron job error: " + err.Error())
			return
		}
	}

	if err != nil {
		logger.SysError("Cron job error: " + err.Error())
		return
	}
}
