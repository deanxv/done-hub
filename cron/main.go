package cron

import (
	"done-hub/common/config"
	"done-hub/common/logger"
	"done-hub/common/scheduler"
	"time"

	"github.com/go-co-op/gocron/v2"
)

// InitCron 保留调度框架，默认注册若干 Mock 任务，方便二开改造
func InitCron() {
	if !config.IsMasterNode {
		logger.SysLog("Cron is disabled on slave node")
		return
	}

	// 每 10 分钟心跳日志（示例）
	_ = scheduler.Manager.AddJob(
		"framework_heartbeat",
		gocron.DurationJob(10*time.Minute),
		gocron.NewTask(func() {
			logger.SysLog("[cron] framework heartbeat")
		}),
	)

	// 每日 00:05 执行一次（示例）
	_ = scheduler.Manager.AddJob(
		"daily_mock_task",
		gocron.DailyJob(1, gocron.NewAtTimes(gocron.NewAtTime(0, 5, 0))),
		gocron.NewTask(func() {
			logger.SysLog("[cron] daily mock task executed")
		}),
	)
}
