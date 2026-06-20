import { useTranslation } from 'react-i18next';
import { Box, Button, Container, Stack, Typography } from '@mui/material';
import { useTheme, alpha } from '@mui/material/styles';
import { GitHub } from '@mui/icons-material';
import AnimateInView from './AnimateInView';
import { DISPLAY } from './styles';

// Closing statement. The page's product entry points all live up in the Hero;
// this finale is the open-source "close" instead of a second hero — one plain
// statement line plus a single link to the GitHub repo. A soft primary-tinted
// glow rises from the bottom so the page resolves into the footer.
const CTA = () => {
  const theme = useTheme();
  const { t } = useTranslation();
  const glow = alpha(theme.palette.primary.main, theme.palette.mode === 'dark' ? 0.3 : 0.18);

  return (
    <Box component="section" sx={{ position: 'relative', overflow: 'hidden', py: { xs: 12, md: 18 } }}>
      <Box
        aria-hidden
        sx={{
          position: 'absolute',
          inset: 0,
          pointerEvents: 'none',
          background: `radial-gradient(ellipse 90% 80% at 50% 118%, ${glow}, transparent 70%)`
        }}
      />
      <Container maxWidth="md" sx={{ position: 'relative' }}>
        <AnimateInView>
          <Stack spacing={4} alignItems="center" textAlign="center">
            <Typography
              component="h2"
              sx={{
                fontFamily: DISPLAY,
                fontWeight: 600,
                fontSize: 'clamp(2.2rem, 4.6vw, 3.6rem)',
                lineHeight: 1.04,
                letterSpacing: '-0.035em',
                maxWidth: 720
              }}
            >
              {t('home.cta.title')}
            </Typography>

            <Button
              href="https://github.com/deanxv/done-hub"
              target="_blank"
              rel="noopener"
              variant="outlined"
              size="large"
              startIcon={<GitHub sx={{ fontSize: '1.1rem' }} />}
            >
              {t('home.cta.github')}
            </Button>
          </Stack>
        </AnimateInView>
      </Container>
    </Box>
  );
};

export default CTA;
