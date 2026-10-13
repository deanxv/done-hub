// 通用工具：把以逗号分隔的字符串列拆成数组（trim + 去空）
// 用于 channel.models / channel.group 等 CSV 风格字段
export const splitCsv = (raw) => {
  if (!raw) return [];
  return raw
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean);
};

// 分组下拉展示文案：管理员靠 code（symbol）唯一定位分组（name 可重复），故 code 作主体置前，
// name 作补充信息括注其后；name 为空或与 code 相同时只展示 code。
export const formatGroupLabel = (symbol, groupMap = {}) => {
  const name = groupMap[symbol]?.name;
  return name && name !== symbol ? `${symbol} (${name})` : symbol;
};

// runWithConcurrency 按固定并发跑一组任务；isStopped 返回 true 后不再领取新任务，
// 已在途的请求任其自然返回（不中断 HTTP），停止只影响后续排队项。
export async function runWithConcurrency(items, limit, worker, isStopped) {
  let cursor = 0;
  const runners = new Array(Math.min(limit, items.length)).fill(null).map(async () => {
    for (;;) {
      if (isStopped()) return;
      const index = cursor;
      cursor += 1;
      if (index >= items.length) return;
      await worker(items[index]);
    }
  });
  await Promise.all(runners);
}
