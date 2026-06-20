import { createTheme } from '@mui/material/styles';

// assets
import colors from 'assets/scss/_themes-vars.module.scss';

// project imports
import componentStyleOverrides from './compStyleOverride';
import themePalette from './palette';
import themeTypography from './typography';
import { varAlpha, createGradient } from './utils';
import { getPrimaryColors } from './presets';

/**
 * Represent theme style and structure as per Material-UI
 * @param {JsonObject} customization customization parameter object
 */

export const theme = (customization) => {
  // 用用户选择的主题色覆盖默认主色
  const color = { ...colors, ...getPrimaryColors(customization.primaryColor) };
  // 创建自定义渐变背景色
  const customGradients = {
    primary: createGradient(color.primaryMain, color.primaryDark),
    secondary: createGradient(color.secondaryMain, color.secondaryDark)
  };
  const options = customization.theme === 'light' ? GetLightOption(color) : GetDarkOption(color);
  const themeOption = {
    colors: color,
    gradients: customGradients,
    ...options,
    customization
  };

  const themeOptions = {
    direction: 'ltr',
    palette: themePalette(themeOption),
    mixins: {
      toolbar: {
        minHeight: '48px',
        padding: '8px 16px',
        '@media (min-width: 600px)': {
          minHeight: '48px'
        }
      }
    },
    shape: {
      borderRadius: themeOption?.customization?.borderRadius || 8
    },
    typography: themeTypography(themeOption),
    breakpoints: {
      values: {
        xs: 0,
        sm: 600,
        md: 960,
        lg: 1280,
        xl: 1920
      }
    },
    zIndex: {
      modal: 1300,
      snackbar: 1400,
      tooltip: 1500
    }
  };

  const themes = createTheme(themeOptions);
  // 把自定义 themeOption 字段挂到 MUI theme 上，sx callback (theme) => theme.xxx 才拿得到
  themes.headBackgroundColor = themeOption.headBackgroundColor;
  themes.tableRowHoverBackgroundColor = themeOption.tableRowHoverBackgroundColor;
  themes.components = componentStyleOverrides(themeOption);

  return themes;
};

export default theme;

function GetDarkOption(color) {
  return {
    mode: 'dark',
    heading: '#FFFFFF',
    paper: color.darkPaper,
    backgroundDefault: color.darkBackground,
    background: color.darkLevel2,
    darkTextPrimary: '#FFFFFF',
    darkTextSecondary: color.grey500,
    textDark: '#FFFFFF',
    menuSelected: color.primaryLight,
    menuSelectedBack: varAlpha(color.primaryMain, 0.16),
    divider: varAlpha(color.grey500, 0.2),
    borderColor: varAlpha(color.grey500, 0.2),
    menuButton: '#28323D',
    menuButtonColor: color.primaryMain,
    menuChip: '#28323D',
    headBackgroundColor: '#28323D',
    headBackgroundColorHover: varAlpha('#28323D', 0.08),
    tableRowHoverBackgroundColor: 'rgba(0, 0, 0, 0.3)',
    tableBorderBottom: varAlpha(color.grey500, 0.2)
  };
}

function GetLightOption(color) {
  return {
    mode: 'light',
    heading: color.grey800,
    paper: '#FFFFFF',
    backgroundDefault: color.grey200,
    background: color.grey200,
    darkTextPrimary: color.grey800,
    darkTextSecondary: color.grey600,
    textDark: color.grey800,
    menuSelected: color.primaryMain,
    menuSelectedBack: varAlpha(color.primaryMain, 0.08),
    divider: varAlpha(color.grey500, 0.2),
    borderColor: color.grey300,
    menuButton: varAlpha(color.primaryMain, 0.08),
    menuButtonColor: color.primaryMain,
    menuChip: color.grey200,
    headBackgroundColor: color.grey200,
    headBackgroundColorHover: varAlpha(color.grey200, 0.12),
    tableRowHoverBackgroundColor: 'rgba(0, 0, 0, 0.04)',
    tableBorderBottom: color.grey300
  };
}
