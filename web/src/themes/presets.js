// 可选主题主色预设，颜色取自 Minimals 调色板，与项目既有语义色同源
const themePresets = [
  { id: 'default', primaryLight: '#5BE49B', primaryMain: '#00A76F', primaryDark: '#007867', primary200: '#C8FAD6', primary800: '#004B50' },
  { id: 'blue', primaryLight: '#76B0F1', primaryMain: '#2065D1', primaryDark: '#103996', primary200: '#D1E9FC', primary800: '#061B64' },
  { id: 'cyan', primaryLight: '#68CDF9', primaryMain: '#078DEE', primaryDark: '#0351AB', primary200: '#CCF4FE', primary800: '#012972' },
  { id: 'purple', primaryLight: '#B985F4', primaryMain: '#7635DC', primaryDark: '#431A9E', primary200: '#EBD6FD', primary800: '#200A69' },
  { id: 'red', primaryLight: '#FFC1AC', primaryMain: '#FF3030', primaryDark: '#B71833', primary200: '#FFE3D5', primary800: '#7A0930' }
];

export const defaultPrimaryColor = themePresets[0].id;

// 根据预设 id 返回需要覆盖到 scss 主色的颜色集合，找不到时回退到默认预设
export function getPrimaryColors(id) {
  const preset = themePresets.find((item) => item.id === id) || themePresets[0];
  return {
    primaryLight: preset.primaryLight,
    primaryMain: preset.primaryMain,
    primaryDark: preset.primaryDark,
    primary200: preset.primary200,
    primary800: preset.primary800
  };
}

export default themePresets;
