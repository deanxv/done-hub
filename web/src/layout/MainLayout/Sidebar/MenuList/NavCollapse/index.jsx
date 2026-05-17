import PropTypes from 'prop-types';
import { useEffect, useState } from 'react';
import { useSelector } from 'react-redux';
import { useLocation } from 'react-router';

// material-ui
import { useTheme, alpha } from '@mui/material/styles';
import { Box, ButtonBase, Collapse, Tooltip, Typography } from '@mui/material';
import { IconChevronDown, IconChevronRight } from '@tabler/icons-react';

// project imports
import NavItem from '../NavItem';

const BULLET_SIZE = 14;
const BULLET_COLOR_LIGHT = '#EDEFF2';
const BULLET_COLOR_DARK = '#282F37';

// ==============================|| SIDEBAR MENU LIST COLLAPSE ITEMS ||============================== //

const NavCollapse = ({ menu, level, isMini = false }) => {
  const theme = useTheme();
  const customization = useSelector((state) => state.customization);
  const isDark = theme.palette.mode === 'dark';

  const [open, setOpen] = useState(false);
  const [selected, setSelected] = useState(null);

  const handleClick = () => {
    setOpen(!open);
    setSelected(!selected ? menu.id : null);
  };

  const { pathname } = useLocation();
  const checkOpenForParent = (child, id) => {
    child.forEach((item) => {
      if (item.url === pathname) {
        setOpen(true);
        setSelected(id);
      }
    });
  };

  useEffect(() => {
    setOpen(false);
    setSelected(null);
    if (menu.children) {
      menu.children.forEach((item) => {
        if (item.children?.length) {
          checkOpenForParent(item.children, menu.id);
        }
        if (item.url === pathname) {
          setSelected(menu.id);
          setOpen(true);
        }
      });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pathname, menu.children]);

  const menus = menu.children?.map((item) => {
    switch (item.type) {
      case 'collapse':
        return <NavCollapse key={item.id} menu={item} level={level + 1} />;
      case 'item':
        return <NavItem key={item.id} item={item} level={level + 1} />;
      default:
        return (
          <Typography key={item.id} variant="h6" color="error" align="center">
            Menu Items Error
          </Typography>
        );
    }
  });

  const isRootItem = level === 1;
  const isOpen = open && !selected;
  const isActive = !!selected;

  const IconComponent = menu.icon;
  const menuIcon = menu.icon ? (
    <IconComponent strokeWidth={1.5} size={isMini ? '1.375rem' : '1.25rem'} />
  ) : null;

  if (isMini) {
    return (
      <Tooltip title={menu.title} placement="right" arrow>
        <ButtonBase
          onClick={handleClick}
          sx={{
            width: '100%',
            borderRadius: `${customization.borderRadius}px`,
            minHeight: '56px',
            flexDirection: 'column',
            justifyContent: 'center',
            alignItems: 'center',
            textAlign: 'center',
            px: 0.5,
            py: 0.75,
            color: theme.palette.text.secondary,
            transition: theme.transitions.create(['background-color', 'color'], {
              duration: theme.transitions.duration.shortest
            }),
            '&:hover': {
              backgroundColor: theme.palette.action.hover
            },
            ...(isActive && {
              color: theme.palette.primary.main,
              backgroundColor: alpha(theme.palette.primary.main, 0.08),
              '&:hover': {
                backgroundColor: alpha(theme.palette.primary.main, 0.16)
              }
            }),
            ...(isOpen && {
              color: theme.palette.text.primary,
              backgroundColor: theme.palette.action.hover
            })
          }}
        >
          {menuIcon && (
            <Box
              component="span"
              sx={{
                display: 'inline-flex',
                width: '22px',
                height: '22px',
                mb: 0.5,
                '& > svg': { width: '100%', height: '100%' }
              }}
            >
              {menuIcon}
            </Box>
          )}
          <Typography
            variant="caption"
            sx={{
              maxWidth: '100%',
              overflow: 'hidden',
              whiteSpace: 'nowrap',
              textOverflow: 'ellipsis',
              lineHeight: '16px',
              fontSize: '0.625rem',
              fontWeight: isActive ? 700 : 600,
              color: 'inherit'
            }}
          >
            {menu.title}
          </Typography>
        </ButtonBase>
      </Tooltip>
    );
  }

  return (
    <>
      <ButtonBase
        onClick={handleClick}
        sx={{
          width: '100%',
          borderRadius: `${customization.borderRadius}px`,
          minHeight: isRootItem ? '44px' : '36px',
          py: 0.5,
          pl: isRootItem ? 1.5 : `${level * 20}px`,
          pr: 1,
          alignItems: 'center',
          justifyContent: 'flex-start',
          textAlign: 'left',
          color: theme.palette.text.secondary,
          transition: theme.transitions.create(['background-color', 'color'], {
            duration: theme.transitions.duration.shortest
          }),
          '&:hover': {
            backgroundColor: theme.palette.action.hover
          },
          ...(isActive && {
            color: theme.palette.primary.main,
            backgroundColor: alpha(theme.palette.primary.main, 0.08),
            '&:hover': {
              backgroundColor: alpha(theme.palette.primary.main, 0.16)
            }
          }),
          ...(isOpen && {
            color: theme.palette.text.primary,
            backgroundColor: theme.palette.action.hover
          })
        }}
      >
        {menuIcon && (
          <Box
            component="span"
            sx={{
              flexShrink: 0,
              display: 'inline-flex',
              width: '24px',
              height: '24px',
              mr: 1.5,
              '& > svg': { width: '100%', height: '100%' }
            }}
          >
            {menuIcon}
          </Box>
        )}

        <Box component="span" sx={{ flex: '1 1 auto', minWidth: 0 }}>
          <Typography
            variant="body2"
            sx={{
              overflow: 'hidden',
              whiteSpace: 'nowrap',
              textOverflow: 'ellipsis',
              fontWeight: isActive ? 600 : 500,
              color: 'inherit'
            }}
          >
            {menu.title}
          </Typography>
        </Box>

        <Box
          component="span"
          sx={{
            width: 16,
            height: 16,
            flexShrink: 0,
            ml: 0.75,
            display: 'inline-flex',
            color: 'inherit'
          }}
        >
          {open ? <IconChevronDown size={16} stroke={1.5} /> : <IconChevronRight size={16} stroke={1.5} />}
        </Box>
      </ButtonBase>

      <Collapse
        in={open}
        timeout="auto"
        unmountOnExit
        sx={{
          pl: `calc(${isRootItem ? '12px' : `${level * 20}px`} + 12px)`,
          '& > .nav-collapse-list': {
            position: 'relative',
            pl: `${BULLET_SIZE}px`,
            '&::before': {
              top: 0,
              left: 0,
              width: '2px',
              content: '""',
              position: 'absolute',
              bottom: `calc(36px - 2px - ${BULLET_SIZE / 2}px)`,
              bgcolor: isDark ? BULLET_COLOR_DARK : BULLET_COLOR_LIGHT
            }
          }
        }}
      >
        <Box
          className="nav-collapse-list"
          sx={{
            display: 'flex',
            flexDirection: 'column',
            gap: '4px',
            pt: '4px'
          }}
        >
          {menus}
        </Box>
      </Collapse>
    </>
  );
};

NavCollapse.propTypes = {
  menu: PropTypes.object,
  level: PropTypes.number,
  isMini: PropTypes.bool
};

export default NavCollapse;
