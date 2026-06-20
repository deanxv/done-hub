import PropTypes from 'prop-types';
import { Box, Stack } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import { useReducedMotion } from 'framer-motion';
// Import each brand's Mono mark directly. The brand index eagerly pulls in an
// Avatar variant that depends on the (uninstalled) @lobehub/ui peer, whereas
// the Mono entry is a self-contained SVG that renders in currentColor.
import OpenAI from '@lobehub/icons/es/OpenAI/components/Mono';
import Claude from '@lobehub/icons/es/Claude/components/Mono';
import Gemini from '@lobehub/icons/es/Gemini/components/Mono';
import Azure from '@lobehub/icons/es/Azure/components/Mono';
import DeepSeek from '@lobehub/icons/es/DeepSeek/components/Mono';
import Moonshot from '@lobehub/icons/es/Moonshot/components/Mono';
import Qwen from '@lobehub/icons/es/Qwen/components/Mono';
import Wenxin from '@lobehub/icons/es/Wenxin/components/Mono';
import Hunyuan from '@lobehub/icons/es/Hunyuan/components/Mono';
import Zhipu from '@lobehub/icons/es/Zhipu/components/Mono';
import Groq from '@lobehub/icons/es/Groq/components/Mono';
import Mistral from '@lobehub/icons/es/Mistral/components/Mono';
import Cohere from '@lobehub/icons/es/Cohere/components/Mono';
import Bedrock from '@lobehub/icons/es/Bedrock/components/Mono';
import XAI from '@lobehub/icons/es/XAI/components/Mono';
import OpenRouter from '@lobehub/icons/es/OpenRouter/components/Mono';
import VertexAI from '@lobehub/icons/es/VertexAI/components/Mono';
import Ollama from '@lobehub/icons/es/Ollama/components/Mono';
import { CHANNEL_OPTIONS } from 'constants/ChannelConstants';
import { MONO } from './styles';

// Provider ids paired with their brand mark, pulled from the real channel
// constants so the names always match what the system actually supports.
const PROVIDER_ICONS = {
  1: OpenAI,
  14: Claude,
  25: Gemini,
  3: Azure,
  28: DeepSeek,
  29: Moonshot,
  17: Qwen,
  15: Wenxin,
  40: Hunyuan,
  16: Zhipu,
  31: Groq,
  30: Mistral,
  36: Cohere,
  32: Bedrock,
  56: XAI,
  20: OpenRouter,
  42: VertexAI,
  39: Ollama
};
const FEATURED_IDS = [1, 14, 25, 3, 28, 29, 17, 15, 40, 16, 31, 30, 36, 32, 56, 20, 42, 39];
const PROVIDERS = FEATURED_IDS.map((id) => ({ name: CHANNEL_OPTIONS[id]?.text, Brand: PROVIDER_ICONS[id] })).filter((p) => p.name);

const Pill = ({ name, Brand }) => {
  const theme = useTheme();
  return (
    <Stack
      direction="row"
      spacing={1}
      alignItems="center"
      sx={{
        flexShrink: 0,
        px: 2,
        py: 1.1,
        borderRadius: '12px',
        border: `1px solid ${theme.palette.divider}`,
        bgcolor: theme.palette.background.paper,
        color: theme.palette.text.primary,
        fontFamily: MONO,
        fontSize: '0.82rem',
        fontWeight: 500,
        whiteSpace: 'nowrap'
      }}
    >
      {Brand && <Brand size={17} />}
      <span>{name}</span>
    </Stack>
  );
};

Pill.propTypes = { name: PropTypes.string, Brand: PropTypes.elementType };

const Row = ({ items, reverse }) => (
  <Box sx={{ display: 'flex', width: 'max-content' }}>
    {[0, 1].map((copy) => (
      <Box
        key={copy}
        aria-hidden={copy === 1}
        className="hh-marquee-track"
        sx={{
          display: 'flex',
          gap: 1.5,
          pr: 1.5,
          animation: `${reverse ? 'marqueeReverse' : 'marquee'} 48s linear infinite`,
          '@keyframes marquee': { from: { transform: 'translateX(0)' }, to: { transform: 'translateX(-100%)' } },
          '@keyframes marqueeReverse': { from: { transform: 'translateX(-100%)' }, to: { transform: 'translateX(0)' } }
        }}
      >
        {items.map(({ name, Brand }) => (
          <Pill key={name} name={name} Brand={Brand} />
        ))}
      </Box>
    ))}
  </Box>
);

Row.propTypes = { items: PropTypes.array, reverse: PropTypes.bool };

// Two brand rows scrolling in opposite directions, faded at both edges. Under
// prefers-reduced-motion it collapses to a static wrapped grid.
const ProviderMarquee = () => {
  const theme = useTheme();
  const reduce = useReducedMotion();

  if (reduce) {
    return (
      <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 1.5 }}>
        {PROVIDERS.map(({ name, Brand }) => (
          <Pill key={name} name={name} Brand={Brand} />
        ))}
      </Box>
    );
  }

  const fade = `linear-gradient(to right, transparent, ${theme.palette.common.black} 7%, ${theme.palette.common.black} 93%, transparent)`;
  const second = [...PROVIDERS].reverse();

  return (
    <Box
      sx={{
        display: 'grid',
        gap: 1.5,
        maskImage: fade,
        WebkitMaskImage: fade,
        '&:hover .hh-marquee-track': { animationPlayState: 'paused' }
      }}
    >
      <Row items={PROVIDERS} />
      <Row items={second} reverse />
    </Box>
  );
};

export default ProviderMarquee;
