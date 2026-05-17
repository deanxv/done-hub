import { createTheme } from '@mui/material/styles';

// assets
import colors from 'assets/scss/_themes-vars.module.scss';

// project imports
import componentStyleOverrides from './compStyleOverride';
import themePalette from './palette';
import themeTypography from './typography';
import { varAlpha, createGradient } from './utils';

// 创建自定义渐变背景色
const customGradients = {
  primary: createGradient(colors.primaryMain, colors.primaryDark),
  secondary: createGradient(colors.secondaryMain, colors.secondaryDark)
};

/**
 * Represent theme style and structure as per Material-UI
 * @param {JsonObject} customization customization parameter object
 */

export const theme = (customization) => {
  const color = colors;
  const options = customization.theme === 'light' ? GetLightOption() : GetDarkOption();
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
  themes.components = componentStyleOverrides(themeOption);

  return themes;
};

export default theme;

function GetDarkOption() {
  const color = colors;
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
    tableBorderBottom: varAlpha(color.grey500, 0.2)
  };
}

function GetLightOption() {
  const color = colors;
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
    tableBorderBottom: color.grey300
  };
}
