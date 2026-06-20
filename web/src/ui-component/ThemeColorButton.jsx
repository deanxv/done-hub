import { useState } from 'react';
import { useDispatch, useSelector } from 'react-redux';
import { useTheme } from '@mui/material/styles';
import { Avatar, Box, ButtonBase, Menu, MenuItem, Tooltip, Typography } from '@mui/material';
import { Icon } from '@iconify/react';
import { useTranslation } from 'react-i18next';
import { SET_PRIMARY_COLOR } from 'store/actions';
import themePresets from 'themes/presets';

export default function ThemeColorButton() {
  const theme = useTheme();
  const dispatch = useDispatch();
  const { t } = useTranslation();

  const primaryColor = useSelector((state) => state.customization.primaryColor);

  const [anchorEl, setAnchorEl] = useState(null);

  const handleMenuOpen = (event) => {
    setAnchorEl(event.currentTarget);
  };

  const handleMenuClose = () => {
    setAnchorEl(null);
  };

  const handleColorChange = (id) => {
    localStorage.setItem('primaryColor', id);
    dispatch({ type: SET_PRIMARY_COLOR, primaryColor: id });
    handleMenuClose();
  };

  return (
    <Box
      sx={{
        ml: 2,
        mr: 3,
        [theme.breakpoints.down('md')]: {
          mr: 2
        }
      }}
    >
      <Tooltip title={t('theme.color')} placement="bottom">
        <ButtonBase sx={{ borderRadius: '12px' }} onClick={handleMenuOpen}>
          <Avatar
            variant="rounded"
            sx={{
              ...theme.typography.commonAvatar,
              ...theme.typography.mediumAvatar,
              ...theme.typography.menuButton,
              transition: 'all .2s ease-in-out',
              borderColor: theme.typography.menuChip.background,
              borderRadius: '50%',
              background: 'transparent',
              overflow: 'hidden',
              '&[aria-controls="menu-list-grow"],&:hover': {
                boxShadow: '0 4px 8px rgba(0,0,0,0.15)',
                background: 'transparent !important'
              }
            }}
            color="inherit"
          >
            <Icon icon="mdi:palette" width="1.5rem" color={theme.palette.primary.main} />
          </Avatar>
        </ButtonBase>
      </Tooltip>
      <Menu
        anchorEl={anchorEl}
        open={Boolean(anchorEl)}
        onClose={handleMenuClose}
        anchorOrigin={{
          vertical: 'bottom',
          horizontal: 'center'
        }}
        transformOrigin={{
          vertical: 'top',
          horizontal: 'center'
        }}
      >
        {themePresets.map((preset) => (
          <MenuItem
            key={preset.id}
            onClick={() => handleColorChange(preset.id)}
            selected={preset.id === primaryColor}
            sx={{
              display: 'flex',
              alignItems: 'center',
              gap: 1
            }}
          >
            <Box sx={{ width: '1.25rem', height: '1.25rem', borderRadius: '50%', backgroundColor: preset.primaryMain }} />
            <Typography variant="body1">{t(`theme.colors.${preset.id}`)}</Typography>
          </MenuItem>
        ))}
      </Menu>
    </Box>
  );
}
