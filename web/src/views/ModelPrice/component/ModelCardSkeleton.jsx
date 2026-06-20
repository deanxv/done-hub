import { Card, Box, Stack, Skeleton } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import { alpha } from '@mui/material/styles';
// ----------------------------------------------------------------------

export default function ModelCardSkeleton() {
  const theme = useTheme();

  return (
    <Card
      sx={{
        height: '100%',
        display: 'flex',
        flexDirection: 'column',
        overflow: 'hidden',
        borderRadius: '12px',
        border: `1px solid ${theme.palette.mode === 'dark' ? alpha('#fff', 0.08) : alpha('#000', 0.05)}`,
        backgroundColor: theme.palette.mode === 'dark' ? alpha(theme.palette.background.paper, 0.6) : theme.palette.background.paper
      }}
    >
      {/* 卡片头部 */}
      <Box sx={{ p: 2, pb: 1.5, borderBottom: `1px solid ${theme.palette.mode === 'dark' ? alpha('#fff', 0.05) : alpha('#000', 0.03)}` }}>
        <Stack direction="row" alignItems="center" spacing={1.5}>
          <Skeleton variant="circular" width={32} height={32} />
          <Box sx={{ flex: 1 }}>
            <Skeleton variant="text" width="60%" sx={{ fontSize: '0.95rem' }} />
            <Skeleton variant="text" width="35%" sx={{ fontSize: '0.7rem' }} />
          </Box>
        </Stack>
      </Box>

      {/* 卡片内容 */}
      <Box sx={{ p: 2, flex: 1, display: 'flex', flexDirection: 'column' }}>
        {/* 描述 */}
        <Skeleton variant="text" width="100%" sx={{ fontSize: '0.8125rem' }} />
        <Skeleton variant="text" width="80%" sx={{ fontSize: '0.8125rem', mb: 1.5 }} />

        {/* 标签 */}
        <Stack direction="row" spacing={0.5} sx={{ mb: 2 }}>
          <Skeleton variant="rectangular" width={56} height={20} sx={{ borderRadius: '4px' }} />
          <Skeleton variant="rectangular" width={56} height={20} sx={{ borderRadius: '4px' }} />
        </Stack>

        {/* 价格信息 */}
        <Box
          sx={{ mt: 'auto', pt: 2, borderTop: `1px solid ${theme.palette.mode === 'dark' ? alpha('#fff', 0.05) : alpha('#000', 0.05)}` }}
        >
          <Stack spacing={1}>
            <Stack direction="row" alignItems="center" justifyContent="space-between">
              <Skeleton variant="text" width="30%" sx={{ fontSize: '0.7rem' }} />
              <Skeleton variant="rectangular" width={70} height={20} sx={{ borderRadius: '6px' }} />
            </Stack>
            <Stack direction="row" alignItems="center" justifyContent="space-between">
              <Skeleton variant="text" width="30%" sx={{ fontSize: '0.7rem' }} />
              <Skeleton variant="rectangular" width={70} height={20} sx={{ borderRadius: '6px' }} />
            </Stack>
          </Stack>
        </Box>

        {/* 操作按钮 */}
        <Box sx={{ mt: 2 }}>
          <Skeleton variant="rectangular" height={34} sx={{ borderRadius: '8px' }} />
        </Box>
      </Box>
    </Card>
  );
}
