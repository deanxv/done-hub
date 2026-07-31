// detectBrowserLanguage 把浏览器语言（如 zh-CN、en、ja-JP）映射为本站支持的语言代码。
// 无法匹配到任何受支持语言时返回 null（此时不做任何提示）。
export const detectBrowserLanguage = () => {
  const candidates = navigator.languages?.length ? navigator.languages : [navigator.language];
  for (const raw of candidates) {
    const mapped = mapLanguage(raw);
    if (mapped) {
      return mapped;
    }
  }
  return null;
};

// mapLanguage 把单个 BCP-47 语言标签归一到受支持的语言代码（zh_CN / zh_HK / en_US / ja_JP）。
const mapLanguage = (raw) => {
  const lower = (raw || '').toLowerCase();
  if (!lower.startsWith('zh')) {
    if (lower.startsWith('ja')) return 'ja_JP';
    if (lower.startsWith('en')) return 'en_US';
    return null;
  }
  // 繁体（台湾/香港/澳门/Hant）归到 zh_HK，其余中文归到 zh_CN
  return /hant|tw|hk|mo/.test(lower) ? 'zh_HK' : 'zh_CN';
};
