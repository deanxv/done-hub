import { Box } from '@mui/material';
import { useTheme, alpha } from '@mui/material/styles';
import { motion, useScroll, useTransform, useReducedMotion } from 'framer-motion';

// One shared canvas behind the whole landing page: a faint technical grid that
// fades out under the hero, plus two soft mesh blobs derived from the theme's
// primary/secondary colors. The blobs drift on scroll for a sense of depth.
// Pure transform/opacity work (no blur filters) so it stays cheap on scroll,
// and it falls back to a static render under prefers-reduced-motion.
const AmbientBackground = () => {
  const theme = useTheme();
  const reduce = useReducedMotion();
  const { scrollYProgress } = useScroll();
  const yA = useTransform(scrollYProgress, [0, 1], ['-4%', '10%']);
  const yB = useTransform(scrollYProgress, [0, 1], ['8%', '-12%']);

  const isDark = theme.palette.mode === 'dark';
  const blobA = `radial-gradient(closest-side, ${alpha(theme.palette.primary.main, isDark ? 0.22 : 0.16)}, transparent)`;
  const blobB = `radial-gradient(closest-side, ${alpha(theme.palette.secondary.main, isDark ? 0.18 : 0.12)}, transparent)`;
  const grid = alpha(theme.palette.text.primary, isDark ? 0.05 : 0.04);

  return (
    <Box aria-hidden sx={{ position: 'absolute', inset: 0, overflow: 'hidden', pointerEvents: 'none', zIndex: 0 }}>
      <Box
        sx={{
          position: 'absolute',
          top: 0,
          left: 0,
          right: 0,
          height: { xs: 680, md: 900 },
          backgroundImage: `linear-gradient(to right, ${grid} 1px, transparent 1px), linear-gradient(to bottom, ${grid} 1px, transparent 1px)`,
          backgroundSize: '64px 64px',
          maskImage: 'radial-gradient(ellipse 75% 100% at 30% 0%, black 10%, transparent 75%)',
          WebkitMaskImage: 'radial-gradient(ellipse 75% 100% at 30% 0%, black 10%, transparent 75%)'
        }}
      />
      <motion.div
        style={{
          position: 'absolute',
          top: '-12%',
          left: '-8%',
          width: '60vw',
          height: '60vw',
          maxWidth: 820,
          maxHeight: 820,
          background: blobA,
          y: reduce ? 0 : yA
        }}
      />
      <motion.div
        style={{
          position: 'absolute',
          top: '38%',
          right: '-12%',
          width: '55vw',
          height: '55vw',
          maxWidth: 760,
          maxHeight: 760,
          background: blobB,
          y: reduce ? 0 : yB
        }}
      />
    </Box>
  );
};

export default AmbientBackground;
