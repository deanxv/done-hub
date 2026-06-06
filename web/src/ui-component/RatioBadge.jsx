import PropTypes from 'prop-types';
import { Box, alpha } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import { useTranslation } from 'react-i18next';

const RatioBadge = ({ ratio }) => {
  const theme = useTheme();
  const { t } = useTranslation();

  if (ratio === undefined || ratio === null) return null;

  const isFree = ratio === 0;
  const isPremium = ratio > 1;
  const colorMain = isFree
    ? theme.palette.success.main
    : isPremium
      ? theme.palette.warning.main
      : theme.palette.info.main;

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
        backgroundColor: alpha(colorMain, theme.palette.mode === 'dark' ? 0.3 : 0.2),
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
