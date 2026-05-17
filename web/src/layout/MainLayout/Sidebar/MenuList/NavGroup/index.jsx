import PropTypes from 'prop-types';
import { useState, useCallback } from 'react';

// material-ui
import { useTheme } from '@mui/material/styles';
import { Box, Collapse, Divider, List, ListSubheader, Typography } from '@mui/material';
import { IconChevronDown, IconChevronRight } from '@tabler/icons-react';

// project imports
import NavItem from '../NavItem';
import NavCollapse from '../NavCollapse';
import { varAlpha } from 'themes/utils';

// ==============================|| SIDEBAR MENU LIST GROUP ||============================== //

const NavGroup = ({ item, isMini = false }) => {
  const theme = useTheme();
  const [open, setOpen] = useState(true);

  const handleToggle = useCallback(() => {
    setOpen((prev) => !prev);
  }, []);

  const items = item.children?.map((menu) => {
    switch (menu.type) {
      case 'collapse':
        return <NavCollapse key={menu.id} menu={menu} level={1} isMini={isMini} />;
      case 'item':
        return <NavItem key={menu.id} item={menu} level={1} isMini={isMini} />;
      default:
        return (
          <Typography key={menu.id} variant="h6" color="error" align="center">
            Menu Items Error
          </Typography>
        );
    }
  });

  if (isMini) {
    return (
      <Box sx={{ py: 0.5 }}>
        <Divider sx={{ mx: 1, mb: 1, borderColor: varAlpha(theme.palette.grey[500], 0.12) }} />
        <Box sx={{ display: 'flex', flexDirection: 'column', gap: '4px', alignItems: 'center' }}>{items}</Box>
      </Box>
    );
  }

  return (
    <List
      disablePadding
      subheader={
        item.title && (
          <ListSubheader
            disableSticky
            disableGutters
            component="div"
            onClick={handleToggle}
            sx={{
              display: 'inline-flex',
              alignItems: 'center',
              alignSelf: 'flex-start',
              gap: 0.5,
              cursor: 'pointer',
              position: 'relative',
              typography: 'overline',
              fontSize: '0.6875rem',
              fontWeight: 700,
              lineHeight: 1.5,
              textTransform: 'uppercase',
              letterSpacing: '0.04em',
              color: theme.palette.text.disabled,
              p: theme.spacing(2, 1, 1, 1.5),
              transition: theme.transitions.create(['color', 'padding-left'], {
                duration: theme.transitions.duration.standard
              }),
              '&:hover': {
                pl: 2,
                color: theme.palette.text.primary,
                '& .nav-group-arrow': {
                  opacity: 1
                }
              },
              background: 'transparent'
            }}
          >
            <Box
              component="span"
              className="nav-group-arrow"
              sx={{
                position: 'absolute',
                left: -4,
                opacity: 0,
                display: 'inline-flex',
                transition: theme.transitions.create(['opacity'], {
                  duration: theme.transitions.duration.standard
                })
              }}
            >
              {open ? <IconChevronDown size={16} stroke={1.5} /> : <IconChevronRight size={16} stroke={1.5} />}
            </Box>
            {item.title}
          </ListSubheader>
        )
      }
    >
      <Collapse in={open} timeout="auto">
        <Box sx={{ display: 'flex', flexDirection: 'column', gap: '4px' }}>{items}</Box>
      </Collapse>
    </List>
  );
};

NavGroup.propTypes = {
  item: PropTypes.object,
  isMini: PropTypes.bool
};

export default NavGroup;
