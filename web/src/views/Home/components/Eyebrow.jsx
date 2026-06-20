import PropTypes from 'prop-types';
import { Box, Stack, Typography } from '@mui/material';
import { useTheme, alpha } from '@mui/material/styles';
import { MONO } from './styles';

// Small monospace section label with an optional index and a hairline rule —
// the editorial marker used at the top of each section for a consistent rhythm.
const Eyebrow = ({ index, label, sx }) => {
  const theme = useTheme();

  return (
    <Stack direction="row" spacing={1.25} alignItems="center" sx={sx}>
      {index && (
        <Typography component="span" sx={{ fontFamily: MONO, fontSize: '0.72rem', fontWeight: 500, color: theme.palette.primary.main }}>
          {index}
        </Typography>
      )}
      <Box sx={{ width: 22, height: '1px', bgcolor: alpha(theme.palette.text.primary, 0.25) }} />
      <Typography
        component="span"
        sx={{
          fontFamily: MONO,
          fontSize: '0.72rem',
          fontWeight: 500,
          letterSpacing: '0.18em',
          textTransform: 'uppercase',
          color: theme.palette.text.secondary
        }}
      >
        {label}
      </Typography>
    </Stack>
  );
};

Eyebrow.propTypes = {
  index: PropTypes.string,
  label: PropTypes.string,
  sx: PropTypes.object
};

export default Eyebrow;
