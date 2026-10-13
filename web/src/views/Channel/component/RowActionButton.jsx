import PropTypes from 'prop-types';
import { useTranslation } from 'react-i18next';
import { IconButton, Tooltip } from '@mui/material';
import { Icon } from '@iconify/react';

// RowActionButton 是批量测速 / 检测表格的行内操作：失败行显示重试，其余显示 label
export default function RowActionButton({ retry, label, disabled, onClick }) {
  const { t } = useTranslation();
  const title = retry ? t('channel_row.retry') : label;
  return (
    <Tooltip title={title} placement="top" arrow>
      <span>
        <IconButton size="small" color={retry ? 'error' : 'primary'} disabled={disabled} onClick={onClick} aria-label={title}>
          <Icon icon={retry ? 'solar:restart-bold' : 'solar:play-circle-linear'} width={18} />
        </IconButton>
      </span>
    </Tooltip>
  );
}

RowActionButton.propTypes = {
  retry: PropTypes.bool,
  label: PropTypes.string,
  disabled: PropTypes.bool,
  onClick: PropTypes.func
};
