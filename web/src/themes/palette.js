/**
 * Color intention that you want to used in your theme
 * @param {JsonObject} theme Theme customization object
 */

import { varAlpha } from './utils';

export default function themePalette(theme) {
  return {
    mode: theme.mode,
    common: {
      black: '#000000',
      white: '#FFFFFF'
    },
    primary: {
      lighter: theme.colors?.primary200 || '#C8FAD6',
      light: theme.colors?.primaryLight,
      main: theme.colors?.primaryMain,
      dark: theme.colors?.primaryDark,
      darker: theme.colors?.primary800,
      200: theme.colors?.primary200,
      800: theme.colors?.primary800,
      contrastText: '#FFFFFF'
    },
    secondary: {
      lighter: theme.colors?.secondary200 || '#EFD6FF',
      light: theme.colors?.secondaryLight,
      main: theme.colors?.secondaryMain,
      dark: theme.colors?.secondaryDark,
      darker: theme.colors?.secondary800,
      200: theme.colors?.secondary200,
      800: theme.colors?.secondary800,
      contrastText: '#FFFFFF'
    },
    info: {
      lighter: '#CAFDF5',
      light: theme.colors?.infoLight || '#61F3F3',
      main: theme.colors?.infoMain || '#00B8D9',
      dark: theme.colors?.infoDark || '#006C9C',
      darker: '#003768',
      contrastText: '#FFFFFF'
    },
    error: {
      lighter: '#FFE9D5',
      light: theme.colors?.errorLight,
      main: theme.colors?.errorMain,
      dark: theme.colors?.errorDark,
      darker: '#7A0916',
      contrastText: '#FFFFFF'
    },
    orange: {
      lighter: '#FFE9D5',
      light: theme.colors?.orangeLight,
      main: theme.colors?.orangeMain,
      dark: theme.colors?.orangeDark,
      contrastText: theme.mode === 'dark' ? '#fff' : '#000'
    },
    warning: {
      lighter: '#FFF5CC',
      light: theme.colors?.warningLight,
      main: theme.colors?.warningMain,
      dark: theme.colors?.warningDark,
      darker: '#7A4100',
      contrastText: '#1C252E'
    },
    success: {
      lighter: '#D3FCD2',
      light: theme.colors?.successLight,
      200: theme.colors?.success200,
      main: theme.colors?.successMain,
      dark: theme.colors?.successDark,
      darker: '#065E49',
      contrastText: '#ffffff'
    },
    grey: {
      50: theme.colors?.grey50,
      100: theme.colors?.grey100,
      200: theme.colors?.grey200,
      300: theme.colors?.grey300,
      400: theme.colors?.grey400 || '#C4CDD5',
      500: theme.colors?.grey500,
      600: theme.colors?.grey600,
      700: theme.colors?.grey700,
      800: theme.colors?.grey800 || '#1C252E',
      900: theme.colors?.grey900 || '#141A21'
    },
    dark: {
      light: theme.colors?.darkTextPrimary,
      main: theme.colors?.darkLevel1,
      dark: theme.colors?.darkLevel2,
      800: theme.colors?.darkBackground,
      900: theme.colors?.darkPaper
    },
    text: {
      primary: theme.darkTextPrimary,
      secondary: theme.darkTextSecondary,
      dark: theme.textDark,
      hint: theme.colors?.grey100,
      disabled: theme.mode === 'dark' ? theme.colors?.grey600 : theme.colors?.grey500
    },
    divider: theme.divider || (theme.mode === 'dark' ? varAlpha(theme.colors?.grey500, 0.2) : varAlpha(theme.colors?.grey500, 0.2)),
    background: {
      paper: theme.paper,
      default: theme.backgroundDefault,
      neutral: theme.mode === 'dark' ? '#28323D' : theme.colors?.grey200
    },
    action: {
      hover: varAlpha(theme.colors?.grey500, 0.08),
      selected: varAlpha(theme.colors?.grey500, 0.16),
      disabled: varAlpha(theme.colors?.grey500, 0.8),
      disabledBackground: varAlpha(theme.colors?.grey500, 0.24),
      focus: varAlpha(theme.colors?.grey500, 0.24),
      active: theme.mode === 'dark' ? theme.colors?.grey500 : theme.colors?.grey600
    }
  };
}
