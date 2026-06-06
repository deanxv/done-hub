import PropTypes from 'prop-types';
import { useEffect, useState } from 'react';
import { Box, Collapse, Stack, Typography, useMediaQuery } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import { Icon } from '@iconify/react';
import { useTranslation } from 'react-i18next';

// 表头筛选区域的折叠包装。移动端默认收起、桌面端默认展开；
// 窗口跨断点时按当前尺寸自动重置（同断点内用户的手动 toggle 不受影响，
// useEffect 依赖只在 isMobile 变化时触发）。视觉沿用 Token 管理员搜索面板。
export default function FilterCollapse({ title, children }) {
  const { t } = useTranslation();
  const theme = useTheme();
  const isMobile = useMediaQuery(theme.breakpoints.down('sm'));
  const [expanded, setExpanded] = useState(() => !isMobile);

  useEffect(() => {
    setExpanded(!isMobile);
  }, [isMobile]);

  return (
    <Box>
      <Box
        sx={{
          px: 2,
          py: 1.25,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          cursor: 'pointer',
          // 常驻 1px border，仅切色，避免 toggle 瞬间盒高跳变 1px
          borderBottom: '1px solid',
          borderColor: expanded ? 'divider' : 'transparent',
          transition: (theme) => theme.transitions.create('border-color'),
          '&:hover': {
            backgroundColor: 'action.hover'
          }
        }}
        onClick={() => setExpanded((v) => !v)}
      >
        <Stack direction="row" alignItems="center" spacing={1}>
          <Icon icon="solar:filter-bold-duotone" width={20} color={theme.palette.text.secondary} />
          <Typography variant="subtitle2" fontWeight={600}>
            {title || t('common.searchFilters')}
          </Typography>
        </Stack>
        <Icon
          icon={expanded ? 'solar:alt-arrow-up-bold-duotone' : 'solar:alt-arrow-down-bold-duotone'}
          width={18}
          color={theme.palette.grey[500]}
        />
      </Box>
      <Collapse in={expanded} timeout="auto">
        {children}
      </Collapse>
    </Box>
  );
}

FilterCollapse.propTypes = {
  title: PropTypes.node,
  children: PropTypes.node
};
