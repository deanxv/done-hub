import { useTranslation } from 'react-i18next';
import { Box, Container, Stack, Typography } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import { Icon } from '@iconify/react';
import AnimateInView from './AnimateInView';
import Eyebrow from './Eyebrow';
import ProviderMarquee from './ProviderMarquee';
import { DISPLAY } from './styles';

const FEATURES = [
  { icon: 'solar:routing-2-bold-duotone', key: 'routing' },
  { icon: 'solar:wallet-money-bold-duotone', key: 'billing' },
  { icon: 'solar:chart-2-bold-duotone', key: 'observability' },
  { icon: 'solar:plug-circle-bold-duotone', key: 'clients' }
];

const sectionTitle = {
  fontFamily: DISPLAY,
  fontWeight: 600,
  fontSize: 'clamp(1.9rem, 3.6vw, 3rem)',
  lineHeight: 1.08,
  letterSpacing: '-0.03em'
};

const Features = () => {
  const theme = useTheme();
  const { t } = useTranslation();

  return (
    <Box component="section" sx={{ py: { xs: 9, md: 14 } }}>
      <Container maxWidth="lg">
        <AnimateInView>
          <Eyebrow index="02" label="Providers" sx={{ mb: 3 }} />
          <Stack spacing={2} sx={{ maxWidth: 680, mb: { xs: 4.5, md: 6 } }}>
            <Typography component="h2" sx={sectionTitle}>
              {t('home.providers.title')}
            </Typography>
            <Typography sx={{ color: theme.palette.text.secondary, fontSize: { xs: '1rem', md: '1.1rem' }, lineHeight: 1.6 }}>
              {t('home.providers.subtitle')}
            </Typography>
          </Stack>
        </AnimateInView>

        <AnimateInView delay={0.06}>
          <ProviderMarquee />
        </AnimateInView>

        <AnimateInView>
          <Eyebrow index="03" label="Platform" sx={{ mt: { xs: 10, md: 16 }, mb: 3 }} />
          <Stack spacing={2} sx={{ maxWidth: 680, mb: { xs: 5, md: 7 } }}>
            <Typography component="h2" sx={sectionTitle}>
              {t('home.features.title')}
            </Typography>
            <Typography sx={{ color: theme.palette.text.secondary, fontSize: { xs: '1rem', md: '1.1rem' }, lineHeight: 1.6 }}>
              {t('home.features.subtitle')}
            </Typography>
          </Stack>
        </AnimateInView>

        <Box
          sx={{
            display: 'grid',
            gridTemplateColumns: { xs: '1fr', sm: 'repeat(2, minmax(0, 1fr))', md: 'repeat(4, minmax(0, 1fr))' },
            gap: { xs: 4, md: 5 }
          }}
        >
          {FEATURES.map((feature, index) => (
            <AnimateInView key={feature.key} delay={index * 0.06} y={20}>
              <Box
                sx={{
                  pt: 3,
                  borderTop: `1px solid ${theme.palette.divider}`,
                  transition: 'border-color .25s ease',
                  '&:hover': { borderTopColor: theme.palette.primary.main }
                }}
              >
                <Box sx={{ color: theme.palette.primary.main, mb: 2 }}>
                  <Icon icon={feature.icon} width="1.6rem" />
                </Box>
                <Typography sx={{ fontFamily: DISPLAY, fontWeight: 600, fontSize: '1.12rem', letterSpacing: '-0.01em', mb: 1 }}>
                  {t(`home.features.items.${feature.key}.title`)}
                </Typography>
                <Typography sx={{ color: theme.palette.text.secondary, fontSize: '0.9rem', lineHeight: 1.6 }}>
                  {t(`home.features.items.${feature.key}.desc`)}
                </Typography>
              </Box>
            </AnimateInView>
          ))}
        </Box>
      </Container>
    </Box>
  );
};

export default Features;
