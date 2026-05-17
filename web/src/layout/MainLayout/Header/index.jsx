import PropTypes from 'prop-types';
import { Icon } from '@iconify/react';
import { useLocation } from 'react-router-dom';
import { useSelector } from 'react-redux';

// material-ui
import { useTheme } from '@mui/material/styles';
import { Box, IconButton, Stack, useMediaQuery } from '@mui/material';

// project imports
import LogoSection from '../LogoSection';
import Profile from './Profile';
import ThemeButton from 'ui-component/ThemeButton';
import I18nButton from 'ui-component/i18nButton';
import { NoticeButton } from 'ui-component/notice';
import { drawerWidth, miniDrawerWidth } from 'store/constant';

// ==============================|| MAIN NAVBAR / HEADER ||============================== //

const Header = ({ handleLeftDrawerToggle, toggleProfileDrawer }) => {
  const theme = useTheme();
  const matchUpMd = useMediaQuery(theme.breakpoints.up('md'));
  const location = useLocation();
  const isConsoleRoute = location.pathname.startsWith('/panel');
  const leftDrawerOpened = useSelector((state) => state.customization.opened);

  return (
    <>
      <Box
        sx={{
          display: 'flex',
          alignItems: 'center',
          ...(matchUpMd && {
            width: `${leftDrawerOpened ? drawerWidth : miniDrawerWidth}px`,
            ml: leftDrawerOpened ? 0 : -3,
            transition: 'width 200ms cubic-bezier(0.4, 0, 0.2, 1), margin 200ms cubic-bezier(0.4, 0, 0.2, 1)'
          }),
          [theme.breakpoints.down('md')]: {
            width: 'auto'
          }
        }}
      >
        <Box
          component="span"
          sx={{
            display: { xs: 'none', md: 'flex' },
            alignItems: 'center',
            justifyContent: leftDrawerOpened ? 'flex-start' : 'center',
            width: '100%',
            overflow: 'visible',
            transition: 'all 200ms cubic-bezier(0.4, 0, 0.2, 1)'
          }}
        >
          <LogoSection isMini={!leftDrawerOpened} />
        </Box>
        {!matchUpMd && (
          <IconButton
            size="medium"
            edge="start"
            color="inherit"
            onClick={handleLeftDrawerToggle}
            sx={{
              width: 38,
              height: 38,
              borderRadius: '8px',
              backgroundColor: theme.palette.mode === 'dark' ? 'rgba(255, 255, 255, 0.05)' : 'rgba(0, 0, 0, 0.03)',
              '&:hover': {
                backgroundColor: theme.palette.mode === 'dark' ? 'rgba(255, 255, 255, 0.1)' : 'rgba(0, 0, 0, 0.06)'
              }
            }}
          >
            <Icon icon="solar:hamburger-menu-linear" width={22} height={22} />
          </IconButton>
        )}
      </Box>

      <Box sx={{ flexGrow: 1 }} />

      <Stack direction="row" spacing={1} alignItems="center">
        <NoticeButton />
        <ThemeButton />
        <I18nButton />
        {isConsoleRoute && <Profile toggleProfileDrawer={toggleProfileDrawer} />}
      </Stack>
    </>
  );
};

Header.propTypes = {
  handleLeftDrawerToggle: PropTypes.func,
  toggleProfileDrawer: PropTypes.func
};

export default Header;
