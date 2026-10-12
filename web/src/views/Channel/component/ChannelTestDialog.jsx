import PropTypes from 'prop-types';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';

import {
  Box,
  Button,
  Checkbox,
  Chip,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  FormControl,
  InputLabel,
  LinearProgress,
  MenuItem,
  Paper,
  Select,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Tooltip,
  Typography
} from '@mui/material';
import { Icon } from '@iconify/react';

import { API } from 'utils/api';
import { showError, showInfo } from 'utils/common';

// 同渠道并发过高容易被上游限流
const TEST_CONCURRENCY = 4;

const ENDPOINT_TYPE_AUTO = 'auto';

const ENDPOINT_TYPE_OPTIONS = [
  { value: 'chat', label: 'chat (/v1/chat/completions)' },
  { value: 'response', label: 'response (/v1/responses)' },
  { value: 'embeddings', label: 'embeddings (/v1/embeddings)' },
  { value: 'image', label: 'image (/v1/images/generations)' }
];

// runWithConcurrency 按固定并发跑一组任务；isStopped 返回 true 后不再领取新任务，
// 已在途的请求任其自然返回（不中断 HTTP），停止只影响后续排队项。
async function runWithConcurrency(items, limit, worker, isStopped) {
  let cursor = 0;
  const runners = new Array(Math.min(limit, items.length)).fill(null).map(async () => {
    for (;;) {
      if (isStopped()) return;
      const index = cursor;
      cursor += 1;
      if (index >= items.length) return;
      await worker(items[index]);
    }
  });
  await Promise.all(runners);
}

function StatusCell({ result, t }) {
  if (!result) {
    return (
      <Typography variant="caption" sx={{ color: 'text.secondary' }}>
        {t('res_time.noTest')}
      </Typography>
    );
  }

  if (result.status === 'testing') {
    return (
      <Stack direction="row" spacing={0.75} alignItems="center">
        <CircularProgress size={14} />
        <Typography variant="caption" sx={{ color: 'info.main' }}>
          {t('channel_row.testing')}
        </Typography>
      </Stack>
    );
  }

  if (result.status === 'success') {
    return <Chip size="small" color="success" variant="outlined" label={t('channel_row.testPassed')} />;
  }

  return <Chip size="small" color="error" variant="outlined" label={t('channel_row.testFailed')} />;
}

function TestResultCell({ result, t }) {
  if (!result || result.status === 'testing') {
    return (
      <Typography variant="caption" sx={{ color: 'text.secondary' }}>
        -
      </Typography>
    );
  }
  if (result.status === 'success') {
    return (
      <Stack direction="row" spacing={0.5} alignItems="center">
        <Icon icon="solar:stopwatch-bold" width={14} />
        <Typography variant="caption">
          {result.time.toFixed(2)}
          {t('res_time.second')}
        </Typography>
      </Stack>
    );
  }
  return (
    <Tooltip title={result.message || ''} placement="top">
      <Typography
        variant="caption"
        sx={{
          color: 'error.main',
          display: '-webkit-box',
          WebkitLineClamp: 2,
          WebkitBoxOrient: 'vertical',
          overflow: 'hidden',
          wordBreak: 'break-all'
        }}
      >
        {result.message || t('channel_row.testFailed')}
      </Typography>
    </Tooltip>
  );
}

StatusCell.propTypes = {
  result: PropTypes.object,
  t: PropTypes.func
};

TestResultCell.propTypes = {
  result: PropTypes.object,
  t: PropTypes.func
};

export function ChannelTestDialog({ item, open, onClose, onTested }) {
  const { t } = useTranslation();
  const stopRef = useRef(false);
  // 批次号：关闭/卸载/重新发起时自增，丢弃上一批在途请求的迟到写回
  const runIdRef = useRef(0);
  const [selected, setSelected] = useState([]);
  const [results, setResults] = useState({});
  const [running, setRunning] = useState(false);
  const [progress, setProgress] = useState({ done: 0, total: 0, success: 0, failed: 0 });
  const [endpointType, setEndpointType] = useState(ENDPOINT_TYPE_AUTO);
  // 模型可达数百个，逐行 includes 在每次结果回写时都是 O(n²)
  const selectedSet = useMemo(() => new Set(selected), [selected]);

  const models = useMemo(() => {
    const list = (item?.models || '')
      .split(',')
      .map((model) => model.trim())
      .filter(Boolean);
    // 测速模型独立于 models 字段，可能不在列表里；补一行便于从这里直接验证自动测速用的模型。
    const testModel = (item?.test_model || '').trim();
    if (testModel && !list.includes(testModel)) {
      list.unshift(testModel);
    }
    return Array.from(new Set(list));
  }, [item?.models, item?.test_model]);

  // 打开时默认全选并清掉上一轮结果；关闭或卸载时停止领取新任务并作废在途批次
  useEffect(() => {
    if (!open) return undefined;
    setSelected(models);
    setResults({});
    setRunning(false);
    setProgress({ done: 0, total: 0, success: 0, failed: 0 });
    setEndpointType(ENDPOINT_TYPE_AUTO);
    return () => {
      stopRef.current = true;
      runIdRef.current += 1;
    };
  }, [open, models]);

  const handleToggleModel = useCallback((model) => {
    setSelected((prev) => (prev.includes(model) ? prev.filter((m) => m !== model) : [...prev, model]));
  }, []);

  const handleSelectAll = useCallback(() => setSelected(models), [models]);
  const handleClearAll = useCallback(() => setSelected([]), []);
  const handleInvert = useCallback(() => {
    setSelected((prev) => models.filter((model) => !prev.includes(model)));
  }, [models]);

  const handleStop = useCallback(() => {
    stopRef.current = true;
  }, []);

  const handleStart = useCallback(async () => {
    // 按列表顺序发起；selected 始终是 models 的子集，按钮在未选择时已禁用
    const targets = models.filter((model) => selectedSet.has(model));

    stopRef.current = false;
    const runId = (runIdRef.current += 1);
    // 关闭后重开再发起会复位 stopRef，仅靠它拦不住旧批次继续领取任务，需同时校验批次号
    const isStopped = () => stopRef.current || runId !== runIdRef.current;
    setRunning(true);
    setResults({});
    setProgress({ done: 0, total: targets.length, success: 0, failed: 0 });

    let done = 0;
    let success = 0;
    let failed = 0;

    await runWithConcurrency(
      targets,
      TEST_CONCURRENCY,
      async (model) => {
        // 领取时才标记测试中，排队项保持「未测试」，停止后无需回退
        setResults((prev) => ({ ...prev, [model]: { status: 'testing' } }));
        let rowResult;
        try {
          const res = await API.get(`/api/channel/test/${item.id}`, {
            params: {
              model,
              ...(endpointType === ENDPOINT_TYPE_AUTO ? {} : { endpoint_type: endpointType })
            }
          });
          // API 拦截器在 HTTP 层出错时已弹过提示，且返回 undefined 而非 reject
          if (!res?.data) throw new Error(t('channel_row.testFailed'));
          const { success: ok, message, time = 0 } = res.data;
          if (ok) {
            rowResult = { status: 'success', time };
            success += 1;
            onTested?.(time * 1000);
          } else {
            rowResult = { status: 'error', message: message || t('channel_row.testFailed') };
            failed += 1;
          }
        } catch (error) {
          rowResult = { status: 'error', message: error.message || t('channel_row.testFailed') };
          failed += 1;
        }

        if (runId !== runIdRef.current) return;

        done += 1;
        setResults((prev) => ({ ...prev, [model]: rowResult }));
        setProgress({ done, total: targets.length, success, failed });
      },
      isStopped
    );

    if (runId !== runIdRef.current) return;

    setRunning(false);
    if (stopRef.current && done < targets.length) {
      showInfo(t('channel_row.multiTestStopped', { success, failed }));
    } else if (failed > 0) {
      showError(t('channel_row.multiTestSummary', { success, failed }));
    } else {
      showInfo(t('channel_row.multiTestSummary', { success, failed }));
    }
  }, [endpointType, item?.id, models, onTested, selectedSet, t]);

  const selectedCount = selected.length;
  const progressValue = progress.total === 0 ? 0 : Math.round((progress.done / progress.total) * 100);

  return (
    <Dialog
      fullWidth
      maxWidth={false}
      open={open}
      onClose={onClose}
      PaperProps={{
        sx: {
          maxWidth: 820,
          borderRadius: 2,
          backgroundImage: 'none'
        }
      }}
    >
      <DialogTitle sx={{ pb: 2, borderBottom: 1, borderColor: 'divider' }}>
        <Stack direction="row" alignItems="center" justifyContent="space-between" spacing={2}>
          <Box sx={{ minWidth: 0 }}>
            <Typography variant="h5" sx={{ fontWeight: 500 }} noWrap>
              {t('channel_row.multiTest')}
            </Typography>
            <Typography variant="caption" sx={{ color: 'text.secondary' }} noWrap component="div">
              {item?.name} · #{item?.id}
            </Typography>
          </Box>
          <FormControl size="small" sx={{ minWidth: 220 }}>
            <InputLabel id="channel-test-endpoint-label">{t('channel_row.endpointType')}</InputLabel>
            <Select
              labelId="channel-test-endpoint-label"
              label={t('channel_row.endpointType')}
              value={endpointType}
              disabled={running}
              onChange={(event) => setEndpointType(event.target.value)}
            >
              <MenuItem value={ENDPOINT_TYPE_AUTO}>{t('channel_row.endpointAuto')}</MenuItem>
              {ENDPOINT_TYPE_OPTIONS.map((option) => (
                <MenuItem key={option.value} value={option.value}>
                  {option.label}
                </MenuItem>
              ))}
            </Select>
          </FormControl>
        </Stack>
      </DialogTitle>

      <DialogContent sx={{ py: 2 }}>
        <Stack spacing={2}>
          <Stack direction="row" spacing={1} alignItems="center" flexWrap="wrap" useFlexGap>
            <Button size="small" startIcon={<Icon icon="solar:check-square-bold" />} onClick={handleSelectAll} disabled={running}>
              {t('channel_edit.selectAll')}
            </Button>
            <Button size="small" startIcon={<Icon icon="solar:square-bold" />} onClick={handleClearAll} disabled={running}>
              {t('channel_row.clearAll')}
            </Button>
            <Button size="small" startIcon={<Icon icon="solar:refresh-square-bold" />} onClick={handleInvert} disabled={running}>
              {t('channel_edit.invertSelection')}
            </Button>
            <Box sx={{ flex: 1 }} />
            <Typography variant="caption" sx={{ color: 'text.secondary' }}>
              {t('channel_row.selectedModels', { count: selectedCount })}
            </Typography>
            {running ? (
              <Button size="small" color="error" variant="outlined" onClick={handleStop} startIcon={<Icon icon="solar:stop-bold" />}>
                {t('channel_row.stopTest')}
              </Button>
            ) : (
              <Button
                size="small"
                variant="contained"
                onClick={handleStart}
                disabled={selectedCount === 0}
                startIcon={<Icon icon="solar:play-bold" />}
              >
                {t('channel_row.startTest')}
              </Button>
            )}
          </Stack>

          {progress.total > 0 && (
            <Box>
              <LinearProgress variant="determinate" value={progressValue} sx={{ height: 6, borderRadius: 1 }} />
              <Stack direction="row" justifyContent="space-between" sx={{ mt: 0.5 }}>
                <Typography variant="caption" sx={{ color: 'text.secondary' }}>
                  {progress.done}/{progress.total}
                </Typography>
                <Typography variant="caption" sx={{ color: 'text.secondary' }}>
                  {t('channel_row.multiTestSummary', { success: progress.success, failed: progress.failed })}
                </Typography>
              </Stack>
            </Box>
          )}

          <Paper variant="outlined" sx={{ borderRadius: 2, overflow: 'hidden' }}>
            <TableContainer sx={{ maxHeight: 420 }}>
              <Table size="small" stickyHeader>
                <TableHead>
                  <TableRow>
                    <TableCell padding="checkbox" />
                    <TableCell>{t('channel_index.model')}</TableCell>
                    <TableCell width={110}>{t('channel_index.status')}</TableCell>
                    <TableCell width={260}>{t('channel_row.resultColumn')}</TableCell>
                  </TableRow>
                </TableHead>
                <TableBody>
                  {models.length === 0 && (
                    <TableRow>
                      <TableCell colSpan={4} align="center" sx={{ py: 4 }}>
                        <Typography variant="body2" sx={{ color: 'text.secondary' }}>
                          {t('channel_row.multiTestNoModels')}
                        </Typography>
                      </TableCell>
                    </TableRow>
                  )}
                  {models.map((model) => (
                    <TableRow key={model} hover>
                      <TableCell padding="checkbox">
                        <Checkbox
                          size="small"
                          checked={selectedSet.has(model)}
                          disabled={running}
                          onChange={() => handleToggleModel(model)}
                        />
                      </TableCell>
                      <TableCell>
                        <Stack direction="row" spacing={1} alignItems="center">
                          <Typography variant="body2" sx={{ fontFamily: 'monospace' }}>
                            {model}
                          </Typography>
                          {item?.test_model === model && <Chip size="small" variant="outlined" label={t('channel_index.testModel')} />}
                        </Stack>
                      </TableCell>
                      <TableCell>
                        <StatusCell result={results[model]} t={t} />
                      </TableCell>
                      <TableCell>
                        <TestResultCell result={results[model]} t={t} />
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </TableContainer>
          </Paper>
        </Stack>
      </DialogContent>

      <DialogActions sx={{ px: 3, pb: 2 }}>
        <Button onClick={onClose}>{t('common.close')}</Button>
      </DialogActions>
    </Dialog>
  );
}

ChannelTestDialog.propTypes = {
  item: PropTypes.object,
  open: PropTypes.bool,
  onClose: PropTypes.func,
  onTested: PropTypes.func
};
