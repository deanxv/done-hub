import { useEffect, useState } from 'react';
import { useSelector } from 'react-redux';
import { Box, Stack, Typography, alpha } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import { useTranslation } from 'react-i18next';
import { useReducedMotion } from 'framer-motion';
import { MONO } from './styles';

// One flat play sequence behind three native protocol tabs, all sharing the one
// base URL. The OpenAI-compatible entry fronts several providers in a row, so the
// auto-play alone tells the whole story: dozens of providers, three protocols,
// one endpoint. Clicking a tab hands control to the visitor.
const STEPS = [
  { proto: 'OpenAI', model: 'gpt-5.5', route: '/v1/chat/completions' },
  { proto: 'OpenAI', model: 'deepseek-v4', route: '/v1/chat/completions' },
  { proto: 'OpenAI', model: 'grok-4.3', route: '/v1/chat/completions' },
  { proto: 'OpenAI', model: 'qwen3.7-max', route: '/v1/chat/completions' },
  { proto: 'Claude', model: 'claude-opus-4-8', route: '/claude/v1/messages' },
  { proto: 'Gemini', model: 'gemini-3.5-flash', route: '/gemini/v1beta/models' }
];
const TABS = ['OpenAI', 'Claude', 'Gemini'];

const TYPE_MS = 24;
const HOLD_MS = 2600;
const LEAD_MS = 360;

// Interactive code window: pick a client, watch the request hit the same base URL
// on its native protocol and stream a reply back. The base comes from the
// configured server_address, falling back to the current origin — never a
// placeholder. Honors prefers-reduced-motion by rendering the settled frame.
const ApiTerminalDemo = () => {
  const theme = useTheme();
  const { t } = useTranslation();
  const reduce = useReducedMotion();
  const siteInfo = useSelector((state) => state.siteInfo);
  const base = (siteInfo?.server_address || (typeof window !== 'undefined' ? window.location.origin : '')).replace(/\/+$/, '');
  const dot = alpha(theme.palette.text.primary, 0.2);

  const [active, setActive] = useState(0);
  const [manual, setManual] = useState(false);
  const [typed, setTyped] = useState(0);

  const step = STEPS[active];
  const reply = t('home.code.reply', { model: step.model });

  useEffect(() => {
    const full = t('home.code.reply', { model: STEPS[active].model });
    if (reduce) {
      setTyped(full.length);
      return undefined;
    }

    let cancelled = false;
    let timer;
    setTyped(0);
    let n = 0;
    const type = () => {
      if (cancelled) return;
      n += 1;
      setTyped(n);
      if (n < full.length) {
        timer = setTimeout(type, TYPE_MS);
      } else if (!manual) {
        timer = setTimeout(() => {
          if (!cancelled) setActive((a) => (a + 1) % STEPS.length);
        }, HOLD_MS);
      }
    };
    timer = setTimeout(type, LEAD_MS);

    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [active, manual, reduce, t]);

  const selectTab = (proto) => {
    const idx = STEPS.findIndex((s) => s.proto === proto);
    if (idx < 0) return;
    setManual(true);
    setActive(idx);
  };

  return (
    <Stack spacing={1.5} sx={{ width: '100%' }}>
      <Box
        sx={{
          width: '100%',
          borderRadius: '14px',
          border: `1px solid ${theme.palette.divider}`,
          bgcolor: theme.palette.background.paper,
          overflow: 'hidden'
        }}
      >
        <Stack
          direction="row"
          alignItems="center"
          spacing={1.5}
          sx={{ px: 2, py: 1.5, borderBottom: `1px solid ${theme.palette.divider}` }}
        >
          <Stack direction="row" spacing={0.75}>
            {[0, 1, 2].map((i) => (
              <Box key={i} sx={{ width: 11, height: 11, borderRadius: '50%', bgcolor: dot }} />
            ))}
          </Stack>
          <Typography sx={{ color: theme.palette.text.secondary, fontFamily: MONO, fontSize: '0.78rem', fontWeight: 500 }}>
            {t('home.code.title')}
          </Typography>
        </Stack>

        <Stack direction="row" spacing={0.5} sx={{ px: { xs: 1.5, sm: 2 }, py: 1.25, borderBottom: `1px solid ${theme.palette.divider}` }}>
          {TABS.map((tab) => {
            const on = step.proto === tab;
            return (
              <Box
                key={tab}
                component="button"
                type="button"
                aria-pressed={on}
                onClick={() => selectTab(tab)}
                sx={{
                  cursor: 'pointer',
                  border: 0,
                  appearance: 'none',
                  px: 1.25,
                  py: 0.5,
                  borderRadius: '7px',
                  fontFamily: MONO,
                  fontSize: '0.76rem',
                  fontWeight: 600,
                  letterSpacing: '0.01em',
                  color: on ? theme.palette.primary.main : theme.palette.text.secondary,
                  bgcolor: on ? alpha(theme.palette.primary.main, 0.1) : 'transparent',
                  transition: 'color .2s ease, background-color .2s ease',
                  '&:hover': { color: on ? theme.palette.primary.main : theme.palette.text.primary }
                }}
              >
                {tab}
              </Box>
            );
          })}
        </Stack>

        <Stack spacing={1.5} sx={{ p: { xs: 2.25, sm: 3 } }}>
          <Typography sx={{ fontFamily: MONO, fontSize: { xs: '0.78rem', sm: '0.86rem' }, lineHeight: 1.7, overflowWrap: 'anywhere' }}>
            <Box component="span" sx={{ color: theme.palette.text.secondary, fontWeight: 500 }}>
              POST{'  '}
            </Box>
            {base && (
              <Box component="span" sx={{ color: theme.palette.primary.main }}>
                {base}
              </Box>
            )}
            <Box component="span" sx={{ color: theme.palette.text.primary, fontWeight: 500 }}>
              {step.route}
            </Box>
          </Typography>

          <Typography sx={{ fontFamily: MONO, fontSize: { xs: '0.78rem', sm: '0.86rem' }, lineHeight: 1.7 }}>
            <Box component="span" sx={{ color: theme.palette.text.secondary, fontWeight: 500 }}>
              model{'  '}
            </Box>
            <Box component="span" sx={{ color: theme.palette.primary.main, fontWeight: 600 }}>
              {step.model}
            </Box>
          </Typography>

          <Typography
            sx={{
              fontFamily: MONO,
              fontSize: { xs: '0.78rem', sm: '0.86rem' },
              lineHeight: 1.8,
              color: theme.palette.text.primary,
              overflowWrap: 'anywhere',
              minHeight: '2.7em'
            }}
          >
            <Box component="span" sx={{ color: theme.palette.text.secondary }}>
              ↳{'  '}
            </Box>
            {reply.slice(0, typed)}
            {!reduce && (
              <Box
                component="span"
                aria-hidden
                sx={{
                  display: 'inline-block',
                  width: '0.55em',
                  height: '1.05em',
                  ml: '1px',
                  verticalAlign: 'text-bottom',
                  bgcolor: theme.palette.primary.main,
                  animation: 'donehubCaret 1s step-end infinite',
                  '@keyframes donehubCaret': { '0%, 49%': { opacity: 1 }, '50%, 100%': { opacity: 0 } }
                }}
              />
            )}
          </Typography>
        </Stack>
      </Box>

      <Typography sx={{ color: theme.palette.text.secondary, fontSize: '0.82rem' }}>{t('home.code.subtitle')}</Typography>
    </Stack>
  );
};

export default ApiTerminalDemo;
