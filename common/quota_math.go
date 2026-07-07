package common

import "math"

// MaxQuota 单次请求可计费额度的安全上限。
// 历史上额度列为 int32(见 users/orders 迁移),此上限用于把溢出/坏输入
// 饱和到一个确定值,避免回绕成负数(把扣费变成退款)或产生垃圾值。
const MaxQuota = math.MaxInt32

// QuotaFromFloat 将 float 计费额度饱和转换为 int:
//   - NaN 或 <= 0    -> 0
//   - >= MaxQuota    -> MaxQuota
//   - 其它           -> int(f)
//
// 计费代码任何路径都不得因算术溢出或未校验输入产生负额度,
// 所有从 float 计算单次额度的强转都应经过本函数。
func QuotaFromFloat(f float64) int {
	if math.IsNaN(f) || f <= 0 {
		return 0
	}
	if f >= float64(MaxQuota) {
		return MaxQuota
	}
	return int(f)
}
