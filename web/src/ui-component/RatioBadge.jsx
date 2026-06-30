import PropTypes from 'prop-types';
import { Box, alpha } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import { useTranslation } from 'react-i18next';

// 倍率主色：免费(0)=success，>1=warning（溢价），其余=info。供 RatioBadge 与 GroupRatioLabel 共用。
export const getRatioColorMain = (theme, ratio) => {
  if (ratio === 0) return theme.palette.success.main;
  if (ratio > 1) return theme.palette.warning.main;
  return theme.palette.info.main;
};

// 倍率底色：与主色配套的浅色填充（暗色模式略深）。
export const getRatioBgColor = (theme, ratio) => alpha(getRatioColorMain(theme, ratio), theme.palette.mode === 'dark' ? 0.3 : 0.2);

const RatioBadge = ({ ratio }) => {
  const theme = useTheme();
  const { t } = useTranslation();

  if (ratio === undefined || ratio === null) return null;

  const isFree = ratio === 0;
  const colorMain = getRatioColorMain(theme, ratio);

  // 尺寸与 ui-component/Label 对齐（height 24 / radius 6 / 12px），方便与 Label 横排时视觉等高
  return (
    <Box
      component="span"
      sx={{
        display: 'inline-flex',
        alignItems: 'center',
        justifyContent: 'center',
        minWidth: 28,
        height: 24,
        borderRadius: '6px',
        backgroundColor: getRatioBgColor(theme, ratio),
        color: colorMain,
        fontSize: '0.75rem',
        fontWeight: 700,
        lineHeight: 1,
        px: 0.75,
        flexShrink: 0
      }}
    >
      {isFree ? t('modelpricePage.free') : `x${ratio}`}
    </Box>
  );
};

RatioBadge.propTypes = {
  ratio: PropTypes.number
};

export default RatioBadge;
