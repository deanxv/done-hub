import PropTypes from 'prop-types';
import { forwardRef, useEffect } from 'react';
import { Link, useLocation } from 'react-router-dom';
import { useDispatch, useSelector } from 'react-redux';

// material-ui
import { useTheme, alpha } from '@mui/material/styles';
import { Avatar, Box, ButtonBase, Chip, Tooltip, Typography, useMediaQuery } from '@mui/material';

// project imports
import { MENU_OPEN, SET_MENU } from 'store/actions';

const BULLET_SIZE = 14;
const BULLET_COLOR_LIGHT = '#EDEFF2';
const BULLET_COLOR_DARK = '#282F37';
const BULLET_SVG = `url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='14' height='14' fill='none' viewBox='0 0 14 14'%3E%3Cpath d='M1 1v4a8 8 0 0 0 8 8h4' stroke='%23efefef' stroke-width='2' stroke-linecap='round'/%3E%3C/svg%3E") no-repeat 50% 50%/100% auto`;

// ==============================|| SIDEBAR MENU LIST ITEMS ||============================== //

const NavItem = ({ item, level, isMini = false }) => {
  const theme = useTheme();
  const dispatch = useDispatch();
  const { pathname } = useLocation();
  const customization = useSelector((state) => state.customization);
  const matchesSM = useMediaQuery(theme.breakpoints.down('lg'));
  const isDark = theme.palette.mode === 'dark';

  const isSelected = customization.isOpen.findIndex((id) => id === item.id) > -1;
  const isRootItem = level === 1;
  const isSubItem = level > 1;

  const Icon = item.icon;
  const itemIcon = item?.icon ? (
    <Icon stroke={1.5} size={isMini ? '1.375rem' : '1.25rem'} />
  ) : null;

  let itemTarget = '_self';
  if (item.target) {
    itemTarget = '_blank';
  }

  let listItemProps = {
    component: forwardRef((props, ref) => <Link ref={ref} {...props} to={item.url} target={itemTarget} />)
  };
  if (item?.external) {
    listItemProps = { component: 'a', href: item.url, target: itemTarget };
  }

  const itemHandler = (id) => {
    dispatch({ type: MENU_OPEN, id });
    if (matchesSM) dispatch({ type: SET_MENU, opened: false });
  };

  useEffect(() => {
    const currentIndex = document.location.pathname
      .toString()
      .split('/')
      .findIndex((id) => id === item.id);
    if (currentIndex > -1) {
      dispatch({ type: MENU_OPEN, id: item.id });
    }
    // eslint-disable-next-line
  }, [pathname]);

  if (isMini) {
    return (
      <Tooltip title={item.title} placement="right" arrow>
        <ButtonBase
          {...listItemProps}
          disabled={item.disabled}
          onClick={() => itemHandler(item.id)}
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
            ...(isSelected && {
              color: theme.palette.primary.main,
              backgroundColor: alpha(theme.palette.primary.main, 0.08),
              '&:hover': {
                backgroundColor: alpha(theme.palette.primary.main, 0.16)
              }
            }),
            ...(item.disabled && {
              opacity: 0.48,
              pointerEvents: 'none'
            })
          }}
        >
          {itemIcon && (
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
              {itemIcon}
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
              fontWeight: isSelected ? 700 : 600,
              color: 'inherit'
            }}
          >
            {item.title}
          </Typography>
        </ButtonBase>
      </Tooltip>
    );
  }

  return (
    <ButtonBase
      {...listItemProps}
      disabled={item.disabled}
      onClick={() => itemHandler(item.id)}
      sx={{
        width: '100%',
        position: 'relative',
        borderRadius: `${customization.borderRadius}px`,
        minHeight: isRootItem ? '44px' : '36px',
        py: 0.5,
        pl: isRootItem ? 1.5 : 1,
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
        ...(isSelected &&
          isRootItem && {
            color: theme.palette.primary.main,
            backgroundColor: alpha(theme.palette.primary.main, 0.08),
            '&:hover': {
              backgroundColor: alpha(theme.palette.primary.main, 0.16)
            }
          }),
        ...(isSelected &&
          isSubItem && {
            color: theme.palette.text.primary,
            backgroundColor: theme.palette.action.hover
          }),
        ...(isSubItem && {
          '&::before': {
            left: 0,
            content: '""',
            position: 'absolute',
            width: `${BULLET_SIZE}px`,
            height: `${BULLET_SIZE}px`,
            transform: `translate(-${BULLET_SIZE}px, -${BULLET_SIZE * 0.4}px)`,
            backgroundColor: isDark ? BULLET_COLOR_DARK : BULLET_COLOR_LIGHT,
            mask: BULLET_SVG,
            WebkitMask: BULLET_SVG
          }
        }),
        ...(item.disabled && {
          opacity: 0.48,
          pointerEvents: 'none'
        })
      }}
    >
      {itemIcon && (
        <Box
          component="span"
          sx={{
            flexShrink: 0,
            display: 'inline-flex',
            width: '24px',
            height: '24px',
            mr: 1.5,
            '& > svg': {
              width: '100%',
              height: '100%'
            }
          }}
        >
          {itemIcon}
        </Box>
      )}

      <Box component="span" sx={{ flex: '1 1 auto', minWidth: 0 }}>
        <Typography
          variant="body2"
          sx={{
            width: '100%',
            maxWidth: '100%',
            display: 'block',
            overflow: 'hidden',
            whiteSpace: 'nowrap',
            textOverflow: 'ellipsis',
            fontWeight: isSelected ? 600 : 500,
            color: 'inherit'
          }}
        >
          {item.title}
        </Typography>
        {item.caption && (
          <Tooltip title={item.caption} placement="top-start">
            <Typography
              variant="caption"
              sx={{
                display: 'block',
                overflow: 'hidden',
                whiteSpace: 'nowrap',
                textOverflow: 'ellipsis',
                color: theme.palette.text.disabled
              }}
            >
              {item.caption}
            </Typography>
          </Tooltip>
        )}
      </Box>

      {item.chip && (
        <Chip
          color={item.chip.color}
          variant={item.chip.variant}
          size={item.chip.size}
          label={item.chip.label}
          avatar={item.chip.avatar && <Avatar>{item.chip.avatar}</Avatar>}
          sx={{ ml: 0.75 }}
        />
      )}
    </ButtonBase>
  );
};

NavItem.propTypes = {
  item: PropTypes.object,
  level: PropTypes.number,
  isMini: PropTypes.bool
};

export default NavItem;
