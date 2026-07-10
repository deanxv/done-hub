import PropTypes from 'prop-types';
import SubCard from 'ui-component/cards/SubCard';
import { Typography, Tooltip, Divider, Box, Select, MenuItem, FormControl, ButtonBase } from '@mui/material';
import { Icon } from '@iconify/react';
import { alpha } from '@mui/material/styles';
import SkeletonDataCard from 'ui-component/cards/Skeleton/DataCard';

export default function DataCard({
  isLoading,
  title,
  content,
  tip,
  subContent,
  showFilter = false,
  filterValue,
  filterOptions = [],
  onFilterChange,
  showSwitch = false,
  switchChecked = false,
  switchLabel,
  onSwitchChange
}) {
  if (isLoading) {
    return <SkeletonDataCard />;
  }

  const renderContent = (content, tip) => (
    <Typography variant="h3" sx={{ fontSize: '2rem', lineHeight: 1.5, fontWeight: 700 }}>
      {tip ? (
        <Tooltip title={tip} placement="top">
          <span>{content}</span>
        </Tooltip>
      ) : (
        content
      )}
    </Typography>
  );

  return (
    <SubCard sx={{ height: '190px' }} contentSX={{ py: 2 }}>
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', minHeight: '32px' }}>
        <Typography variant="subtitle1" sx={{ fontWeight: 600 }}>
          {title}
        </Typography>
        {showSwitch && (
          <ButtonBase
            onClick={onSwitchChange}
            sx={{
              display: 'flex',
              alignItems: 'center',
              gap: 0.5,
              px: 1,
              height: '28px',
              borderRadius: '14px',
              color: switchChecked ? 'primary.main' : 'text.disabled',
              bgcolor: (theme) => (switchChecked ? alpha(theme.palette.primary.main, 0.12) : 'transparent'),
              transition: 'background-color 0.2s, color 0.2s',
              '&:hover': { bgcolor: (theme) => alpha(theme.palette.primary.main, switchChecked ? 0.2 : 0.08) }
            }}
          >
            <Icon icon="tabler:refresh" width={16} height={16} />
            <Typography variant="caption" sx={{ fontWeight: 500, lineHeight: 1 }}>
              {switchLabel}
            </Typography>
          </ButtonBase>
        )}
        {showFilter && (
          <FormControl size="small" sx={{ minWidth: 80 }}>
            <Select
              value={filterValue}
              onChange={onFilterChange}
              variant="outlined"
              disabled={isLoading}
              sx={{
                height: '32px',
                fontSize: '0.75rem',
                '& .MuiSelect-select': {
                  padding: '4px 8px',
                  fontSize: '0.75rem'
                }
              }}
            >
              {filterOptions.map((option) => (
                <MenuItem key={option.value} value={option.value} sx={{ fontSize: '0.75rem' }}>
                  {option.label}
                </MenuItem>
              ))}
            </Select>
          </FormControl>
        )}
      </Box>
      {renderContent(content, tip)}
      <Divider />
      <Typography variant="subtitle2" sx={{ mt: 2 }}>
        {subContent}
      </Typography>
    </SubCard>
  );
}

DataCard.propTypes = {
  isLoading: PropTypes.bool,
  title: PropTypes.string,
  content: PropTypes.oneOfType([PropTypes.string, PropTypes.number]),
  tip: PropTypes.node,
  subContent: PropTypes.node,
  showFilter: PropTypes.bool,
  filterValue: PropTypes.string,
  filterOptions: PropTypes.array,
  onFilterChange: PropTypes.func,
  showSwitch: PropTypes.bool,
  switchChecked: PropTypes.bool,
  switchLabel: PropTypes.string,
  onSwitchChange: PropTypes.func
};
