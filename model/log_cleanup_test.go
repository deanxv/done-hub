package model

import (
	"errors"
	"testing"
)

// 已有清理在跑时，再次触发必须立即返回 ErrLogCleanupRunning，且不碰 DB。
func TestDeleteOldLogs_RejectsWhenRunning(t *testing.T) {
	if !logCleanupRunning.CompareAndSwap(false, true) {
		t.Fatal("cleanup flag unexpectedly set before test")
	}
	defer logCleanupRunning.Store(false)

	if !IsLogCleanupRunning() {
		t.Fatal("IsLogCleanupRunning should report true while flag is held")
	}

	n, err := DeleteOldLogs("test", 0)
	if !errors.Is(err, ErrLogCleanupRunning) {
		t.Fatalf("expected ErrLogCleanupRunning, got n=%d err=%v", n, err)
	}
	if n != 0 {
		t.Fatalf("expected 0 deleted, got %d", n)
	}
	if !IsLogCleanupRunning() {
		t.Fatal("rejected call must not clear the flag held by the running cleanup")
	}
}
