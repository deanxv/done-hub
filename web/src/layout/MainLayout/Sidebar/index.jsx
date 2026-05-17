import PropTypes from 'prop-types';

// material-ui
import { useTheme } from '@mui/material/styles';
import { Box, Chip, Drawer, IconButton, Stack, SvgIcon, useMediaQuery } from '@mui/material';

// project imports
import MenuList from './MenuList';
import LogoSection from '../LogoSection';
import MenuCard from './MenuCard';
import { drawerWidth, miniDrawerWidth } from 'store/constant';
import { useTranslation } from 'react-i18next';
import { varAlpha } from 'themes/utils';

const transitionEasing = 'cubic-bezier(0.4, 0, 0.2, 1)';
const transitionDuration = '200ms';

// ==============================|| SIDEBAR DRAWER ||============================== //

const Sidebar = ({ drawerOpen, drawerToggle, window: windowProp }) => {
  const theme = useTheme();
  const matchUpMd = useMediaQuery(theme.breakpoints.up('md'));
  const { t } = useTranslation();
  const isDark = theme.palette.mode === 'dark';
  const isMini = matchUpMd && !drawerOpen;
  const currentWidth = isMini ? miniDrawerWidth : drawerWidth;

  const scrollbarStyles = {
    scrollbarWidth: 'thin',
    scrollbarColor: `${isDark ? 'rgba(255,255,255,0.15)' : 'rgba(0,0,0,0.12)'} transparent`,
    '&::-webkit-scrollbar': {
      width: '5px'
    },
    '&::-webkit-scrollbar-thumb': {
      background: isDark ? 'rgba(255,255,255,0.15)' : 'rgba(0,0,0,0.12)',
      borderRadius: '4px'
    },
    '&::-webkit-scrollbar-track': {
      background: 'transparent'
    }
  };

  const sidebarContent = (
    <Box
      sx={{
        display: 'flex',
        flexDirection: 'column',
        height: '100%',
        overflow: 'hidden'
      }}
    >
      {!matchUpMd && (
        <Box sx={{ pl: 3.5, pt: 2.5, pb: 1 }}>
          <LogoSection />
        </Box>
      )}

      <Box
        sx={{
          flex: '1 1 auto',
          overflowY: 'auto',
          overflowX: 'hidden',
          px: isMini ? 1 : 2,
          pt: matchUpMd ? 1 : 0,
          pb: 2,
          transition: `padding ${transitionDuration} ${transitionEasing}`,
          ...scrollbarStyles
        }}
      >
        {!isMini && <MenuCard />}
        <MenuList isMini={isMini} />
      </Box>

      {!isMini && (
        <Box
          sx={{
            flexShrink: 0,
            py: 1.5,
            borderTop: `1px dashed ${varAlpha(theme.palette.grey[500], 0.12)}`
          }}
        >
          <Stack direction="row" justifyContent="center">
            <Chip
              label={import.meta.env.VITE_APP_VERSION || t('menu.unknownVersion')}
              disabled
              size="small"
              sx={{
                cursor: 'default',
                backgroundColor: isDark ? 'rgba(255, 255, 255, 0.08)' : 'rgba(0, 0, 0, 0.04)',
                color: theme.palette.text.disabled,
                fontSize: '0.6875rem',
                height: '22px',
                '& .MuiChip-label': {
                  px: 1
                }
              }}
            />
          </Stack>
        </Box>
      )}
    </Box>
  );

  const toggleButton = matchUpMd && (
    <IconButton
      size="small"
      onClick={drawerToggle}
      sx={{
        p: 0.5,
        top: 24,
        position: 'fixed',
        color: 'action.active',
        bgcolor: 'background.default',
        transform: 'translateX(-50%)',
        zIndex: theme.zIndex.drawer + 2,
        left: `${currentWidth}px`,
        border: `1px solid ${varAlpha(theme.palette.grey[500], 0.12)}`,
        transition: `left ${transitionDuration} ${transitionEasing}`,
        '&:hover': {
          color: 'text.primary',
          bgcolor: isDark ? 'rgba(255,255,255,0.08)' : 'rgba(0,0,0,0.04)'
        }
      }}
    >
      <SvgIcon sx={{ width: 16, height: 16, ...(isMini && { transform: 'scaleX(-1)' }) }}>
        <path
          fill="currentColor"
          d="M13.83 19a1 1 0 0 1-.78-.37l-4.83-6a1 1 0 0 1 0-1.27l5-6a1 1 0 0 1 1.54 1.28L10.29 12l4.32 5.36a1 1 0 0 1-.78 1.64"
        />
      </SvgIcon>
    </IconButton>
  );

  if (matchUpMd) {
    return (
      <>
        {toggleButton}
        <Box
          component="nav"
          sx={{
            position: 'fixed',
            top: '64px',
            left: 0,
            height: 'calc(100% - 64px)',
            width: `${currentWidth}px`,
            zIndex: theme.zIndex.drawer,
            display: 'flex',
            flexDirection: 'column',
            bgcolor: 'background.default',
            borderRight: `1px solid ${varAlpha(theme.palette.grey[500], 0.12)}`,
            transition: `width ${transitionDuration} ${transitionEasing}`,
            overflowX: 'hidden'
          }}
        >
          {sidebarContent}
        </Box>
      </>
    );
  }

  return (
    <Box component="nav">
      <Drawer
        container={windowProp?.document.body}
        variant="temporary"
        anchor="left"
        open={drawerOpen}
        onClose={drawerToggle}
        sx={{
          '& .MuiDrawer-paper': {
            width: drawerWidth,
            background: theme.palette.background.default,
            color: theme.palette.text.primary,
            borderRight: `1px solid ${varAlpha(theme.palette.grey[500], 0.12)}`,
            boxSizing: 'border-box',
            borderRadius: 0,
            top: '0',
            height: '100%',
            boxShadow: theme.shadows[8],
            zIndex: 1300,
            overflowX: 'hidden'
          },
          '& .MuiBackdrop-root': {
            zIndex: 1290
          }
        }}
        ModalProps={{
          keepMounted: true,
          closeAfterTransition: true
        }}
        color="inherit"
      >
        {sidebarContent}
      </Drawer>
    </Box>
  );
};

Sidebar.propTypes = {
  drawerOpen: PropTypes.bool,
  drawerToggle: PropTypes.func,
  window: PropTypes.object
};

export default Sidebar;
