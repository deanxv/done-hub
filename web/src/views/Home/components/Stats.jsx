import { useEffect, useRef, useState } from 'react';
import PropTypes from 'prop-types';
import { useTranslation } from 'react-i18next';
import { Box, Container, Typography } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import { useReducedMotion } from 'framer-motion';
import { CHANNEL_OPTIONS } from 'constants/ChannelConstants';
import AnimateInView from './AnimateInView';
import Eyebrow from './Eyebrow';
import { DISPLAY, MONO } from './styles';

// Every figure here is derived from the codebase, never marketing fluff:
// the channel count is read straight from CHANNEL_OPTIONS so it can never drift,
// the three native protocols match what ApiTerminalDemo exposes, and the project
// is fully open source. No model/user/request counts — those depend on the
// deployment and would be fabricated here.
const FIGURES = [
  { value: Object.keys(CHANNEL_OPTIONS).length, suffix: '', key: 'channels' },
  { value: 3, suffix: '', key: 'protocols' },
  { value: 100, suffix: '%', key: 'openSource' }
];

const DURATION = 1400;
const easeOut = (p) => 1 - Math.pow(1 - p, 3);

// Counts up from 0 to `value` once the figure scrolls into view. Honors
// prefers-reduced-motion by rendering the final value immediately.
const Figure = ({ value, suffix, label, isFirst }) => {
  const theme = useTheme();
  const reduce = useReducedMotion();
  const ref = useRef(null);
  const [display, setDisplay] = useState(reduce ? value : 0);

  useEffect(() => {
    if (reduce) return undefined;
    const node = ref.current;
    if (!node) return undefined;

    let frame;
    let start;
    const run = () => {
      const tick = (now) => {
        if (start === undefined) start = now;
        const progress = Math.min((now - start) / DURATION, 1);
        setDisplay(Math.round(easeOut(progress) * value));
        if (progress < 1) frame = requestAnimationFrame(tick);
      };
      frame = requestAnimationFrame(tick);
    };

    const observer = new IntersectionObserver(
      ([entry]) => {
        if (entry.isIntersecting) {
          run();
          observer.disconnect();
        }
      },
      { threshold: 0.4 }
    );
    observer.observe(node);

    return () => {
      observer.disconnect();
      if (frame) cancelAnimationFrame(frame);
    };
  }, [reduce, value]);

  return (
    <Box
      ref={ref}
      sx={{
        px: { xs: 0, sm: 4 },
        py: { xs: 3, sm: 0 },
        borderColor: theme.palette.divider,
        borderStyle: 'solid',
        borderWidth: isFirst ? 0 : { xs: '1px 0 0 0', sm: '0 0 0 1px' }
      }}
    >
      <Typography
        component="div"
        sx={{ fontFamily: DISPLAY, fontWeight: 600, fontSize: 'clamp(2.6rem, 5vw, 4rem)', lineHeight: 1, letterSpacing: '-0.03em' }}
      >
        {display}
        {suffix}
      </Typography>
      <Typography
        sx={{
          mt: 1.5,
          fontFamily: MONO,
          fontSize: '0.74rem',
          fontWeight: 500,
          letterSpacing: '0.16em',
          textTransform: 'uppercase',
          color: theme.palette.text.secondary
        }}
      >
        {label}
      </Typography>
    </Box>
  );
};

Figure.propTypes = {
  value: PropTypes.number,
  suffix: PropTypes.string,
  label: PropTypes.string,
  isFirst: PropTypes.bool
};

const Stats = () => {
  const { t } = useTranslation();

  return (
    <Box component="section" sx={{ py: { xs: 6, md: 10 } }}>
      <Container maxWidth="lg">
        <AnimateInView>
          <Eyebrow index="01" label="By the numbers" sx={{ mb: { xs: 4, md: 5 } }} />
          <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', sm: 'repeat(3, 1fr)' } }}>
            {FIGURES.map((figure, index) => (
              <Figure
                key={figure.key}
                value={figure.value}
                suffix={figure.suffix}
                label={t(`home.stats.${figure.key}`)}
                isFirst={index === 0}
              />
            ))}
          </Box>
        </AnimateInView>
      </Container>
    </Box>
  );
};

export default Stats;
