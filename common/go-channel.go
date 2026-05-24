package common

import (
	"done-hub/common/logger"
	"fmt"
	"runtime/debug"
	"sync"
	"time"
)

func SafeGoroutine(f func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.SysError(fmt.Sprintf("child goroutine panic occured: error: %v, stack: %s", r, string(debug.Stack())))
			}
		}()
		f()
	}()
}

var trackedGoroutines sync.WaitGroup

// TrackedGoroutine 启动一个 panic-safe 的 goroutine 并注册到全局 WaitGroup。
// 用于 handler 派生出去、又承担关键副作用（如计费 / 写消费日志）的协程，
// 让 main 在 graceful shutdown 时能等它们跑完再 flush batch 队列。
func TrackedGoroutine(f func()) {
	trackedGoroutines.Add(1)
	go func() {
		defer trackedGoroutines.Done()
		defer func() {
			if r := recover(); r != nil {
				logger.SysError(fmt.Sprintf("tracked goroutine panic: %v, stack: %s", r, string(debug.Stack())))
			}
		}()
		f()
	}()
}

// WaitTrackedGoroutines 阻塞等待所有 TrackedGoroutine 完成，或直到 timeout。
// 返回 true 表示全部完成；false 表示有协程未在 timeout 内退出（数据可能丢失）。
//
// 注意：仅用于进程一次性 shutdown 场景。timeout 触发时内部辅助 goroutine
// 仍会阻塞在 WaitGroup.Wait() 上直到所有 tracked 协程归零——sync.WaitGroup
// 没有取消机制，无法回避。进程即将退出所以无影响，不要在长生命周期场景复用。
func WaitTrackedGoroutines(timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		trackedGoroutines.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

func SafeSend(ch chan bool, value bool) (closed bool) {
	defer func() {
		// Recover from panic if one occured. A panic would mean the channel was closed.
		if recover() != nil {
			closed = true
		}
	}()

	// This will panic if the channel is closed.
	ch <- value

	// If the code reaches here, then the channel was not closed.
	return false
}
