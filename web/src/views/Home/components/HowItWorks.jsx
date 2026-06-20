import { useTranslation } from 'react-i18next';
import { Box, Container, Stack, Typography } from '@mui/material';
import { useTheme, alpha } from '@mui/material/styles';
import AnimateInView from './AnimateInView';
import Eyebrow from './Eyebrow';
import { DISPLAY, MONO } from './styles';

const STEPS = [
  { num: '01', key: 's1' },
  { num: '02', key: 's2' },
  { num: '03', key: 's3' },
  { num: '04', key: 's4' }
];

const HowItWorks = () => {
  const theme = useTheme();
  const { t } = useTranslation();

  return (
    <Box component="section" sx={{ py: { xs: 9, md: 14 } }}>
      <Container maxWidth="lg">
        <Box
          sx={{
            display: 'grid',
            gridTemplateColumns: { xs: '1fr', md: '0.85fr 1.15fr' },
            gap: { xs: 5, md: 8 },
            alignItems: 'start'
          }}
        >
          <AnimateInView>
            <Stack spacing={2.5} sx={{ position: { md: 'sticky' }, top: { md: 120 } }}>
              <Eyebrow index="04" label="Workflow" />
              <Typography
                component="h2"
                sx={{
                  fontFamily: DISPLAY,
                  fontWeight: 600,
                  fontSize: 'clamp(1.9rem, 3.6vw, 3rem)',
                  lineHeight: 1.08,
                  letterSpacing: '-0.03em'
                }}
              >
                {t('home.howItWorks.title')}
              </Typography>
              <Typography
                sx={{ color: theme.palette.text.secondary, fontSize: { xs: '1rem', md: '1.1rem' }, lineHeight: 1.6, maxWidth: 420 }}
              >
                {t('home.howItWorks.subtitle')}
              </Typography>
            </Stack>
          </AnimateInView>

          <Box>
            {STEPS.map((step, index) => (
              <AnimateInView key={step.num} delay={index * 0.06} y={20}>
                <Box
                  sx={{
                    display: 'grid',
                    gridTemplateColumns: 'auto minmax(0, 1fr)',
                    gap: { xs: 2.5, sm: 4 },
                    alignItems: 'baseline',
                    py: { xs: 3, md: 3.5 },
                    borderTop: index === 0 ? 'none' : `1px solid ${theme.palette.divider}`
                  }}
                >
                  <Typography
                    sx={{
                      fontFamily: MONO,
                      fontSize: { xs: '1.5rem', md: '2rem' },
                      fontWeight: 500,
                      lineHeight: 1,
                      color: alpha(theme.palette.text.primary, 0.25)
                    }}
                  >
                    {step.num}
                  </Typography>
                  <Box>
                    <Typography sx={{ fontFamily: DISPLAY, fontWeight: 600, fontSize: '1.2rem', letterSpacing: '-0.01em', mb: 0.75 }}>
                      {t(`home.howItWorks.${step.key}Title`)}
                    </Typography>
                    <Typography sx={{ color: theme.palette.text.secondary, fontSize: '0.95rem', lineHeight: 1.6 }}>
                      {t(`home.howItWorks.${step.key}Desc`)}
                    </Typography>
                  </Box>
                </Box>
              </AnimateInView>
            ))}
          </Box>
        </Box>
      </Container>
    </Box>
  );
};

export default HowItWorks;
