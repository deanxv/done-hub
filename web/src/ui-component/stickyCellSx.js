// 右侧 sticky 列的共享样式。集中维护阴影、背景色、行 hover overlay，
// 避免 12+ TableRow 与 TableHead 复制粘贴 sx 块。
// --sticky-shadow-opacity CSS 变量由 hooks/useStickyShadow 在 scroll 容器上写入：
// 已滚到最右或不溢出时为 0，否则为 0.1；变量未注入时各 cell 退回常驻阴影 0.1。

const baseSticky = {
  whiteSpace: 'nowrap',
  position: 'sticky',
  right: 0,
  boxShadow: '-2px 0 4px rgba(0,0,0,var(--sticky-shadow-opacity, 0.1))'
};

export const stickyCellSx = {
  ...baseSticky,
  backgroundColor: 'background.paper',
  zIndex: 1,
  // 行 hover 时叠一层与 MuiTableRow:hover 同色的 overlay（compStyleOverride.js 同样从
  // theme.tableRowHoverBackgroundColor 读取，源头唯一），否则 sticky cell 的 paper
  // 底色会盖住行的 hover 色，横向滚动暴露后会出现色差。
  '.MuiTableRow-root:hover &': {
    backgroundImage: (theme) => {
      const overlay = theme.tableRowHoverBackgroundColor;
      return `linear-gradient(${overlay}, ${overlay})`;
    }
  }
};

export const stickyHeadCellSx = {
  ...baseSticky,
  backgroundColor: (theme) => theme.headBackgroundColor,
  zIndex: 2
};
