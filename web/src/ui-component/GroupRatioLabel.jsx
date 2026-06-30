import PropTypes from 'prop-types';
import { Box } from '@mui/material';
import { Icon } from '@iconify/react';
import { alpha, useTheme } from '@mui/material/styles';
import { useTranslation } from 'react-i18next';
import { getRatioColorMain, getRatioBgColor } from 'ui-component/RatioBadge';

// 双色胶囊：左侧分组标签与右侧倍率徽章无缝拼接（容器 overflow:hidden 裁切子级圆角），
// 让「分组 ↔ 倍率」的归属一目了然；ratio 为空时只渲染标签部分。
// color 复用 MUI 调色板键：default 为灰底常规态，error 等用于「不存在 / 不可用」失效态。
// 传入 onDelete 时在尾部追加删除 ×，并与其相邻段（有倍率时即彩色倍率段、否则灰底 code 段）共用同一底色，
// 始终保持整枚胶囊干净的两段式，删除键作为 chip 内部元素低饱和呈现、hover 加深（遵循 M3 / MUI 规范）。
const GroupRatioLabel = ({ label, ratio, color = 'default', onDelete, sx, ...other }) => {
  const theme = useTheme();
  const { t } = useTranslation();
  const isDefault = color === 'default';
  const bgBase = isDefault ? theme.palette.grey[500] : theme.palette[color].main;
  const textColor = isDefault ? theme.palette.text.secondary : theme.palette[color].dark;

  const hasRatio = ratio !== undefined && ratio !== null;
  const ratioColor = hasRatio ? getRatioColorMain(theme, ratio) : null;
  // 删除段底色延续左邻段（有倍率→彩色倍率段，无倍率→灰底 code 段）以保持无缝；
  // 但 × 图标本身用统一中性灰，避免跟随倍率色而显得每个删除键颜色都不同。
  const deleteBg = hasRatio ? getRatioBgColor(theme, ratio) : 'transparent';

  return (
    <Box
      sx={{
        display: 'inline-flex',
        alignItems: 'stretch',
        maxWidth: '100%',
        height: 24,
        borderRadius: '6px',
        overflow: 'hidden',
        backgroundColor: alpha(bgBase, 0.16),
        ...sx
      }}
      {...other}
    >
      <Box
        component="span"
        sx={{
          minWidth: 0,
          px: 0.75,
          fontSize: '0.75rem',
          fontWeight: 700,
          lineHeight: '24px',
          color: textColor,
          whiteSpace: 'nowrap',
          overflow: 'hidden',
          textOverflow: 'ellipsis'
        }}
      >
        {label}
      </Box>
      {hasRatio && (
        <Box
          component="span"
          sx={{
            display: 'inline-flex',
            alignItems: 'center',
            justifyContent: 'center',
            flexShrink: 0,
            minWidth: 28,
            px: 0.75,
            fontSize: '0.75rem',
            fontWeight: 700,
            lineHeight: 1,
            backgroundColor: getRatioBgColor(theme, ratio),
            color: ratioColor
          }}
        >
          {ratio === 0 ? t('modelpricePage.free') : `x${ratio}`}
        </Box>
      )}
      {onDelete && (
        <Box
          component="span"
          onClick={onDelete}
          sx={{
            display: 'inline-flex',
            alignItems: 'center',
            flexShrink: 0,
            pl: hasRatio ? 0.25 : 0.5,
            pr: 0.5,
            cursor: 'pointer',
            backgroundColor: deleteBg,
            color: 'text.secondary',
            opacity: 0.6,
            '&:hover': { opacity: 1 }
          }}
        >
          <Icon icon="mdi:close" width={14} />
        </Box>
      )}
    </Box>
  );
};

GroupRatioLabel.propTypes = {
  label: PropTypes.node,
  ratio: PropTypes.number,
  color: PropTypes.string,
  onDelete: PropTypes.func,
  sx: PropTypes.object
};

export default GroupRatioLabel;
