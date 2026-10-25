package model

import (
	"done-hub/common/config"
	"testing"
	"time"
)

func resetQuotaWarningLocalDedup() {
	quotaWarningLocalDedup.Lock()
	quotaWarningLocalDedup.expireAt = map[string]time.Time{}
	quotaWarningLocalDedup.Unlock()
}

type sentWarning struct {
	userId      int
	quota       int
	noMoreQuota bool
}

// captureQuotaWarningEmails 把发送入口替换成同步收集器，用例因此是确定性的。
func captureQuotaWarningEmails(t *testing.T) func() []sentWarning {
	t.Helper()
	var sent []sentWarning
	orig := sendQuotaWarningEmailFunc
	sendQuotaWarningEmailFunc = func(userId int, quota int, noMoreQuota bool) {
		sent = append(sent, sentWarning{userId, quota, noMoreQuota})
	}
	t.Cleanup(func() { sendQuotaWarningEmailFunc = orig })
	return func() []sentWarning { return sent }
}

// fakeQuotaWarningClock 用可推进的假时钟替换去重窗口的时钟源。
func fakeQuotaWarningClock(t *testing.T) func(d time.Duration) {
	t.Helper()
	now := time.Unix(1_000_000, 0)
	orig := quotaWarningNow
	quotaWarningNow = func() time.Time { return now }
	t.Cleanup(func() { quotaWarningNow = orig })
	return func(d time.Duration) { now = now.Add(d) }
}

// setupQuotaWarningTest 固定为：全局阈值 500000（$1）、预扣开销 500、Redis 关闭。
// 对应的重新上膛水位：「即将用尽」= 500000 + max(50000, 500) = 550000；「已用尽」= 余额 > 500。
func setupQuotaWarningTest(t *testing.T) {
	t.Helper()
	resetQuotaWarningLocalDedup()
	origEnabled, origThreshold, origRedis, origPre := config.QuotaRemindEnabled, config.QuotaRemindThreshold, config.RedisEnabled, config.PreConsumedQuota
	config.QuotaRemindEnabled = true
	config.QuotaRemindThreshold = 500_000
	config.RedisEnabled = false
	config.PreConsumedQuota = 500
	t.Cleanup(func() {
		config.QuotaRemindEnabled, config.QuotaRemindThreshold, config.RedisEnabled, config.PreConsumedQuota = origEnabled, origThreshold, origRedis, origPre
		resetQuotaWarningLocalDedup()
	})
}

// drain 以固定步长从 from 扣到 to（含），模拟真实的小步扣费。
func drain(userId, from, to, step int) {
	for q := from; q > to; q -= step {
		d := step
		if q-d < to {
			d = q - to
		}
		notifyQuotaWarningIfNeeded(userId, q, d, nil, true)
	}
}

func TestEvaluateQuotaWarning(t *testing.T) {
	setupQuotaWarningTest(t)
	cases := []struct {
		name                   string
		after, thresh          int
		state                  quotaWarningState
		lowRearm, exhaustRearm bool
	}{
		{"well above", 1_000_000, 500_000, quotaWarningStateSafe, true, true},
		{"exactly rearm level", 550_000, 500_000, quotaWarningStateSafe, true, true},
		{"just under rearm level", 549_999, 500_000, quotaWarningStateBand, false, true},
		{"exactly threshold", 500_000, 500_000, quotaWarningStateBand, false, true},
		{"just below threshold", 499_999, 500_000, quotaWarningStateLow, false, true},
		{"low but above refund margin", 501, 500_000, quotaWarningStateLow, false, true},
		{"low within refund margin", 500, 500_000, quotaWarningStateLow, false, false},
		{"exhausted zero", 0, 500_000, quotaWarningStateExhausted, false, false},
		{"exhausted negative", -37, 500_000, quotaWarningStateExhausted, false, false},
		{"threshold zero positive within margin", 1, 0, quotaWarningStateBand, false, false},
		{"threshold zero above margin", 501, 0, quotaWarningStateSafe, true, true},
		{"threshold zero exhausted", 0, 0, quotaWarningStateExhausted, false, false},
		{"small threshold uses refund margin", 1_600, 1_000, quotaWarningStateSafe, true, true},
		{"small threshold band", 1_499, 1_000, quotaWarningStateBand, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			state, low, ex := evaluateQuotaWarning(c.after, c.thresh)
			if state != c.state || low != c.lowRearm || ex != c.exhaustRearm {
				t.Fatalf("evaluateQuotaWarning(%d,%d) = (%v,%v,%v), want (%v,%v,%v)",
					c.after, c.thresh, state, low, ex, c.state, c.lowRearm, c.exhaustRearm)
			}
		})
	}
}

// 核心语义：小步扣费逐步跌破 → 充值回到安全区 → 再次小步跌破，同一天内要收到两次。
func TestNotifyQuotaWarning_SmallStepsRecrossAfterTopUp(t *testing.T) {
	setupQuotaWarningTest(t)
	sent := captureQuotaWarningEmails(t)
	advance := fakeQuotaWarningClock(t)

	drain(1, 540_000, 450_000, 10_000) // 每笔 10000，远小于 50000 余量
	advance(2 * time.Hour)
	drain(1, 1_000_000, 450_000, 10_000) // 充值后再跌破

	got := sent()
	if len(got) != 2 || got[0].noMoreQuota || got[1].noMoreQuota {
		t.Fatalf("expected 2 low warnings, got %+v", got)
	}
	if got[0].quota != 490_000 || got[1].quota != 490_000 {
		t.Fatalf("expected both sent on first step below threshold, got %+v", got)
	}
}

// 小步扣到用尽 → 充值 → 再小步扣到用尽，「已用尽」也要发两次；「即将用尽」同样两次。
func TestNotifyQuotaWarning_SmallStepsExhaustTwice(t *testing.T) {
	setupQuotaWarningTest(t)
	sent := captureQuotaWarningEmails(t)
	advance := fakeQuotaWarningClock(t)

	drain(2, 520_000, 0, 300) // 最后几笔 before < 500
	advance(time.Hour)
	drain(2, 600_000, -37, 300)

	got := sent()
	want := []sentWarning{{2, 499_900, false}, {2, 0, true}, {2, 499_800, false}, {2, 0, true}}
	if len(got) != len(want) {
		t.Fatalf("expected %d warnings, got %+v", len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("warning[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// 阈值附近因预扣退回产生的来回抖动不能重发：回升到 500000~549999 的滞回带不重新上膛。
func TestNotifyQuotaWarning_BounceInBandDoesNotRefire(t *testing.T) {
	setupQuotaWarningTest(t)
	sent := captureQuotaWarningEmails(t)
	advance := fakeQuotaWarningClock(t)

	notifyQuotaWarningIfNeeded(3, 500_200, 300, nil, true) // 跌破，发一封
	for i := 0; i < 6; i++ {
		advance(11 * time.Minute)                              // 并发去重锁已过期
		notifyQuotaWarningIfNeeded(3, 500_400, 500, nil, true) // 退回到 500400 再扣到 499900
	}

	if got := sent(); len(got) != 1 {
		t.Fatalf("expected band bounce to be absorbed, got %+v", got)
	}
}

// 0 附近的退回抖动同理：回到 1~500 不给「已用尽」重新上膛。
func TestNotifyQuotaWarning_BounceNearZeroDoesNotRefire(t *testing.T) {
	setupQuotaWarningTest(t)
	sent := captureQuotaWarningEmails(t)
	advance := fakeQuotaWarningClock(t)

	notifyQuotaWarningIfNeeded(4, 200, 200, nil, true)
	for i := 0; i < 6; i++ {
		advance(11 * time.Minute)
		notifyQuotaWarningIfNeeded(4, 400, 400, nil, true)
	}

	got := sent()
	if len(got) != 1 || !got[0].noMoreQuota {
		t.Fatalf("expected one exhausted warning, got %+v", got)
	}
}

// 并发请求都读到同一个 before 并同时判为跌破，只发一封。
func TestNotifyQuotaWarning_ConcurrentDuplicateSuppressed(t *testing.T) {
	setupQuotaWarningTest(t)
	sent := captureQuotaWarningEmails(t)

	for i := 0; i < 3; i++ {
		notifyQuotaWarningIfNeeded(5, 500_200, 300, nil, true)
	}

	if got := sent(); len(got) != 1 {
		t.Fatalf("expected 1 warning, got %+v", got)
	}
}

// 预扣费阶段跌破发了邮件后，最终结算仍低于阈值，要被「已发送」锁压住。
func TestNotifyQuotaWarning_PostConsumeAfterPreConsumeSuppressed(t *testing.T) {
	setupQuotaWarningTest(t)
	sent := captureQuotaWarningEmails(t)
	advance := fakeQuotaWarningClock(t)

	notifyQuotaWarningIfNeeded(6, 500_200, 300, nil, true) // 预扣
	advance(11 * time.Minute)
	notifyQuotaWarningIfNeeded(6, 499_900, 50, nil, true) // 最终结算

	if got := sent(); len(got) != 1 {
		t.Fatalf("expected post-consume to be suppressed, got %+v", got)
	}
}

// 用户把阈值设得比当前余额还高：余额永远到不了上膛水位，靠 24h 保底过期每天兜底一封。
func TestNotifyQuotaWarning_ThresholdAboveBalanceDailyFallback(t *testing.T) {
	setupQuotaWarningTest(t)
	sent := captureQuotaWarningEmails(t)
	advance := fakeQuotaWarningClock(t)

	threshold := 5_000_000_000 // $10000
	for i := 0; i < 5; i++ {
		notifyQuotaWarningIfNeeded(7, 200_000-i*500, 500, &threshold, true)
	}
	advance(25 * time.Hour)
	notifyQuotaWarningIfNeeded(7, 190_000, 500, &threshold, true)

	got := sent()
	if len(got) != 2 || got[0].noMoreQuota || got[1].noMoreQuota {
		t.Fatalf("expected one warning per 24h window, got %+v", got)
	}
	if got[0].quota != 199_500 {
		t.Fatalf("email should carry post-charge balance, got %+v", got[0])
	}
}

func TestNotifyQuotaWarning_Skips(t *testing.T) {
	t.Run("user disabled", func(t *testing.T) {
		setupQuotaWarningTest(t)
		sent := captureQuotaWarningEmails(t)
		notifyQuotaWarningIfNeeded(11, 500_200, 300, nil, false)
		if got := sent(); len(got) != 0 {
			t.Fatalf("expected none, got %+v", got)
		}
	})
	t.Run("global disabled", func(t *testing.T) {
		setupQuotaWarningTest(t)
		config.QuotaRemindEnabled = false
		sent := captureQuotaWarningEmails(t)
		notifyQuotaWarningIfNeeded(12, 500_200, 300, nil, true)
		if got := sent(); len(got) != 0 {
			t.Fatalf("expected none, got %+v", got)
		}
	})
	t.Run("refund delta", func(t *testing.T) {
		setupQuotaWarningTest(t)
		sent := captureQuotaWarningEmails(t)
		notifyQuotaWarningIfNeeded(13, 400_000, -100, nil, true)
		if got := sent(); len(got) != 0 {
			t.Fatalf("expected none, got %+v", got)
		}
	})
	t.Run("balance healthy", func(t *testing.T) {
		setupQuotaWarningTest(t)
		sent := captureQuotaWarningEmails(t)
		notifyQuotaWarningIfNeeded(14, 10_000_000, 100, nil, true)
		if got := sent(); len(got) != 0 {
			t.Fatalf("expected none, got %+v", got)
		}
	})
	t.Run("per-user threshold zero only exhausted", func(t *testing.T) {
		setupQuotaWarningTest(t)
		sent := captureQuotaWarningEmails(t)
		zero := 0
		notifyQuotaWarningIfNeeded(15, 50, 10, &zero, true)
		if got := sent(); len(got) != 0 {
			t.Fatalf("expected none, got %+v", got)
		}
	})
}

// Redis 启用时本地占位不能长于并发去重窗口，否则其它节点的 rearm 对本节点不生效。
func TestQuotaWarningLocalTTL_CappedWhenRedisEnabled(t *testing.T) {
	orig := config.RedisEnabled
	t.Cleanup(func() { config.RedisEnabled = orig })

	config.RedisEnabled = false
	if got := quotaWarningLocalTTL(quotaWarningSentTTL); got != quotaWarningSentTTL {
		t.Fatalf("without redis local ttl should be full %v, got %v", quotaWarningSentTTL, got)
	}

	config.RedisEnabled = true
	if got := quotaWarningLocalTTL(quotaWarningSentTTL); got != quotaWarningBurstTTL {
		t.Fatalf("with redis local ttl should be capped to %v, got %v", quotaWarningBurstTTL, got)
	}
	if got := quotaWarningLocalTTL(quotaWarningBurstTTL); got != quotaWarningBurstTTL {
		t.Fatalf("burst ttl should pass through, got %v", got)
	}
}

func TestQuotaWarningLocalAcquire_ExpiresAndPurges(t *testing.T) {
	resetQuotaWarningLocalDedup()
	defer resetQuotaWarningLocalDedup()

	now := time.Unix(1_000_000, 0)
	if !quotaWarningLocalAcquire("a", time.Minute, now) {
		t.Fatal("first acquire should succeed")
	}
	if quotaWarningLocalAcquire("a", time.Minute, now.Add(30*time.Second)) {
		t.Fatal("second acquire within window should fail")
	}
	if !quotaWarningLocalAcquire("a", time.Minute, now.Add(time.Minute)) {
		t.Fatal("acquire after expiry should succeed")
	}
	// 其它过期 key 应被顺手清理，map 不无限增长
	quotaWarningLocalAcquire("b", time.Minute, now.Add(2*time.Minute))
	quotaWarningLocalAcquire("c", time.Minute, now.Add(10*time.Minute))
	quotaWarningLocalDedup.Lock()
	n := len(quotaWarningLocalDedup.expireAt)
	quotaWarningLocalDedup.Unlock()
	if n != 1 {
		t.Fatalf("expected only live key to remain, got %d entries", n)
	}
}
