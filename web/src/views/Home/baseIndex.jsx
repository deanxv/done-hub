import { Box } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import AmbientBackground from './components/AmbientBackground';
import Hero from './components/Hero';
import Stats from './components/Stats';
import Features from './components/Features';
import HowItWorks from './components/HowItWorks';
import CTA from './components/CTA';

// Default visitor landing page (shown when no custom home_page_content is set).
// Sections are transparent and float over a single shared AmbientBackground so
// the page reads as one continuous surface rather than stacked cards. Everything
// follows the active preset color and light/dark mode. The site Footer is
// rendered by MinimalLayout — not here.
const BaseIndex = () => {
  const theme = useTheme();

  return (
    <Box sx={{ position: 'relative', overflowX: 'clip', backgroundColor: theme.palette.background.default }}>
      <AmbientBackground />
      <Box sx={{ position: 'relative', zIndex: 1 }}>
        <Hero />
        <Stats />
        <Features />
        <HowItWorks />
        <CTA />
      </Box>
    </Box>
  );
};

export default BaseIndex;
