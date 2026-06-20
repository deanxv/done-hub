import { Link } from 'react-router-dom';
import { useSelector } from 'react-redux';
import { useTranslation } from 'react-i18next';
import { Box, Button, Container, Stack, Typography } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import { Icon } from '@iconify/react';
import ApiTerminalDemo from './ApiTerminalDemo';
import AnimateInView from './AnimateInView';
import Eyebrow from './Eyebrow';
import { DISPLAY } from './styles';

const Hero = () => {
  const theme = useTheme();
  const { t } = useTranslation();
  const account = useSelector((state) => state.account);
  const isLoggedIn = Boolean(account.user);

  return (
    <Box component="section" sx={{ pt: { xs: 8, md: 16 }, pb: { xs: 9, md: 18 } }}>
      <Container maxWidth="lg">
        <Box
          sx={{
            display: 'grid',
            gridTemplateColumns: { xs: '1fr', md: '1.1fr 0.9fr' },
            gap: { xs: 6, md: 8 },
            alignItems: 'center'
          }}
        >
          <Stack spacing={4} alignItems="flex-start">
            <AnimateInView>
              <Eyebrow label="Open source" />
            </AnimateInView>

            <AnimateInView delay={0.08}>
              <Typography
                variant="h1"
                sx={{
                  fontFamily: DISPLAY,
                  fontWeight: 600,
                  lineHeight: 0.98,
                  letterSpacing: '-0.045em',
                  fontSize: 'clamp(2.75rem, 6.4vw, 5.5rem)'
                }}
              >
                {t('home.hero.title')}
              </Typography>
            </AnimateInView>

            <AnimateInView delay={0.16}>
              <Typography
                sx={{ color: theme.palette.text.secondary, fontSize: { xs: '1.05rem', md: '1.2rem' }, lineHeight: 1.6, maxWidth: 460 }}
              >
                {t('home.hero.subtitle')}
              </Typography>
            </AnimateInView>

            <AnimateInView delay={0.24}>
              <Stack direction="row" spacing={1.5} alignItems="center" flexWrap="wrap" useFlexGap sx={{ pt: 0.5 }}>
                <Button
                  component={Link}
                  to={isLoggedIn ? '/panel/dashboard' : '/register'}
                  variant="contained"
                  size="large"
                  endIcon={<Icon icon="solar:arrow-right-linear" />}
                >
                  {isLoggedIn ? t('home.hero.console') : t('home.hero.getStarted')}
                </Button>
                <Button component={Link} to="/price" variant="outlined" size="large">
                  {t('home.hero.viewPricing')}
                </Button>
              </Stack>
            </AnimateInView>
          </Stack>

          <AnimateInView delay={0.18} y={32}>
            <ApiTerminalDemo />
          </AnimateInView>
        </Box>
      </Container>
    </Box>
  );
};

export default Hero;
