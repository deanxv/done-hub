import PropTypes from 'prop-types';
import { useTheme } from '@mui/material/styles';
import { Box, Typography, Stack, FormControl, InputLabel, OutlinedInput } from '@mui/material';
import { Icon } from '@iconify/react';
import { useTranslation } from 'react-i18next';

export const LongContextSelector = ({ value = {}, onChange }) => {
  const { t } = useTranslation();
  const theme = useTheme();

  const handleChangeField = (field, fieldValue) => {
    onChange({ ...value, [field]: fieldValue });
  };

  const fields = [
    { key: 'threshold', label: t('pricing_edit.longContextThreshold'), step: '1', integer: true },
    { key: 'input_ratio', label: t('pricing_edit.longContextInputRatio'), step: '0.01' },
    { key: 'output_ratio', label: t('pricing_edit.longContextOutputRatio'), step: '0.01' }
  ];

  return (
    <Box sx={{ width: '100%' }}>
      <Typography
        variant="body1"
        gutterBottom
        sx={{
          fontWeight: 500,
          mb: 1.5,
          display: 'flex',
          alignItems: 'center',
          gap: 0.8
        }}
      >
        <Icon icon="tabler:ruler-measure" width={18} height={18} color={theme.palette.primary.main} />
        {t('pricing_edit.longContext')}
      </Typography>

      <Stack direction={{ xs: 'column', sm: 'row' }} spacing={1}>
        {fields.map((field) => (
          <FormControl key={field.key} fullWidth>
            <InputLabel shrink>{field.label}</InputLabel>
            <OutlinedInput
              notched
              label={field.label}
              type="number"
              value={value[field.key] ?? ''}
              inputProps={{ step: field.step, min: '0' }}
              onChange={(e) =>
                handleChangeField(field.key, (field.integer ? parseInt(e.target.value, 10) : parseFloat(e.target.value)) || 0)
              }
            />
          </FormControl>
        ))}
      </Stack>

      <Typography variant="caption" color="textSecondary" sx={{ mt: 1, display: 'block' }}>
        {t('pricing_edit.longContextHelp')}
      </Typography>
    </Box>
  );
};

LongContextSelector.propTypes = {
  value: PropTypes.object,
  onChange: PropTypes.func.isRequired
};
