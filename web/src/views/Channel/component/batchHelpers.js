// 通用工具：把以逗号分隔的字符串列拆成数组（trim + 去空）
// 用于 channel.models / channel.group 等 CSV 风格字段
export const splitCsv = (raw) => {
  if (!raw) return [];
  return raw
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean);
};
