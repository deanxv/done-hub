package requester

import (
	"done-hub/common/logger"
	"time"
)

const (
	// streamDrainDeadline 是 drain goroutine 自身的上限。极端情况下（handler
	// 死循环 send、上游永不关连接）防止 drain 变成新的泄漏源。
	streamDrainDeadline = 30 * time.Second

	// streamCloseTimeout 是 Close() 等待 drain 结束的上限。正常路径下 drain
	// 在 producer 退出后秒级完成，设上限只为避免调用方被异常情况拖死。
	streamCloseTimeout = 5 * time.Second
)

// DrainAndClose 解决"handler 在 unbuffered channel 上的阻塞 send + consumer
// 已 return → producer 永久卡死"的 deadlock，避免 H2 stream slot 泄漏。
//
// 调用顺序很关键：
//
//  1. 启动 drain goroutine 接管 dataChan/errChan 的读，让 handler 内任何
//     pending 的 unbuffered send 立刻能投出去
//  2. closer() 关闭底层 reader / response body，让 producer 的下一次读返回
//     EOF / 错误
//  3. producer 退出，其 defer 关闭 dataChan/errChan
//  4. drain 看到双 channel 关闭，自然退出
//
// 如果不做 drain：consumer 收到第一个 err 就 return → defer Close 关 body；
// 但 producer 此时可能正卡在 handler 内部 `dataChan <- x` 上，body 关闭只对
// 下次读生效，当前 send 没人收 → producer goroutine 永久阻塞、上游 stream
// slot 永久占用。
//
// 调用方约定：所有 dataChan/errChan 的消费者必须已退出再调用本函数，否则
// drain 会和消费者抢同一份数据。relay/common.go 的 `defer stream.Close()`
// 模式天然满足这个约定（consumer goroutine return 后 defer 才跑）。
//
// tag 用于 drain 超时时的告警日志，便于线上定位是哪个 stream reader 出了问题。
func DrainAndClose[T any](dataChan chan T, errChan chan error, closer func(), tag string) {
	drainDone := make(chan struct{})
	go func() {
		defer close(drainDone)
		dataCh := (<-chan T)(dataChan)
		errCh := (<-chan error)(errChan)
		deadline := time.NewTimer(streamDrainDeadline)
		defer deadline.Stop()
		for dataCh != nil || errCh != nil {
			select {
			case _, ok := <-dataCh:
				if !ok {
					dataCh = nil
				}
			case _, ok := <-errCh:
				if !ok {
					errCh = nil
				}
			case <-deadline.C:
				return
			}
		}
	}()

	closer()

	// 用 NewTimer + Stop 而不是 time.After：后者直到自然到期才会被 GC 回收，
	// 高频开关流时这点小垃圾会积成可见的内存压力。
	closeTimer := time.NewTimer(streamCloseTimeout)
	defer closeTimer.Stop()
	select {
	case <-drainDone:
	case <-closeTimer.C:
		logger.SysError(tag + ": drain timeout, producer goroutine may still be blocked")
	}
}
