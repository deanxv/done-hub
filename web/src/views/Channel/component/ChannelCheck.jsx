import PropTypes from 'prop-types';
import { Fragment, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';

import {
  Alert,
  Box,
  Button,
  Checkbox,
  Chip,
  CircularProgress,
  Collapse,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  IconButton,
  LinearProgress,
  Paper,
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
import { LoadingButton } from '@mui/lab';
import { Icon } from '@iconify/react';

import { API } from 'utils/api';
import { showError, showInfo } from 'utils/common';
import { runWithConcurrency, splitCsv } from './batchHelpers';
import RowActionButton from './RowActionButton';

// 每个模型在服务端先跑基础检测，再并行发出其余 4 个检测请求，模型并发 2 即上游并发不超过 8
const CHECK_CONCURRENCY = 2;

const PROBE_KEYS = ['base', 'image', 'json', 'tool', 'relay'];

// 与后端 check_channel 的状态值一致
const STATUS = { FAILED: 0, SUCCESS: 1, WARNING: 2, SKIPPED: 3 };

const STATUS_META = {
  [STATUS.SUCCESS]: { icon: 'solar:check-circle-bold', color: 'success', labelKey: 'channel_check.statusPassed' },
  [STATUS.WARNING]: { icon: 'solar:danger-triangle-bold', color: 'warning', labelKey: 'channel_check.statusWarning' },
  [STATUS.FAILED]: { icon: 'solar:close-circle-bold', color: 'error', labelKey: 'channel_check.statusFailed' },
  [STATUS.SKIPPED]: { icon: 'solar:minus-circle-linear', color: 'text', labelKey: 'channel_check.statusSkipped' }
};

// statusColor 返回 MUI 颜色路径；不适用项用次要文字色，避免和通过/失败混淆
const statusColor = (meta) => (meta.color === 'text' ? 'text.secondary' : `${meta.color}.main`);

// rowOutcome 把一行结果归为 passed / warning / failed，请求本身出错也算失败
function rowOutcome(result) {
  if (result.status === 'error' || result.data.status === STATUS.FAILED) return 'failed';
  if (result.data.status === STATUS.WARNING) return 'warning';
  return 'passed';
}

function ProbeCell({ probe }) {
  if (!probe) {
    return (
      <Typography variant="caption" sx={{ color: 'text.disabled' }}>
        -
      </Typography>
    );
  }
  const meta = STATUS_META[probe.status] || STATUS_META[STATUS.FAILED];
  const tip = (probe.results || []).map((r) => `${r.name}：${r.remark}`).join('\n');
  return (
    <Tooltip title={<Box sx={{ whiteSpace: 'pre-wrap' }}>{tip}</Box>} placement="top" arrow>
      <Box component="span" sx={{ display: 'inline-flex', color: statusColor(meta) }}>
        <Icon icon={meta.icon} width={20} />
      </Box>
    </Tooltip>
  );
}

ProbeCell.propTypes = {
  probe: PropTypes.object
};

function OverallCell({ result, t }) {
  if (!result) {
    return (
      <Typography variant="caption" sx={{ color: 'text.secondary' }}>
        {t('res_time.noTest')}
      </Typography>
    );
  }
  if (result.status === 'checking') {
    return (
      <Stack direction="row" spacing={0.75} alignItems="center">
        <CircularProgress size={14} />
        <Typography variant="caption" sx={{ color: 'info.main' }}>
          {t('channel_check.checking')}
        </Typography>
      </Stack>
    );
  }
  if (result.status === 'error') {
    return (
      <Tooltip title={result.message || ''} placement="top">
        <Chip size="small" color="error" variant="outlined" label={t('channel_check.statusFailed')} />
      </Tooltip>
    );
  }
  const meta = STATUS_META[result.data.status] || STATUS_META[STATUS.FAILED];
  return <Chip size="small" color={meta.color === 'text' ? 'default' : meta.color} variant="outlined" label={t(meta.labelKey)} />;
}

OverallCell.propTypes = {
  result: PropTypes.object,
  t: PropTypes.func
};

function DetailPanel({ result, t }) {
  const [openResponse, setOpenResponse] = useState({});

  if (result.status === 'error') {
    return (
      <Alert severity="error" sx={{ wordBreak: 'break-all' }}>
        {result.message}
      </Alert>
    );
  }

  return (
    <Stack spacing={1.5}>
      {result.data.items.map((probe) => {
        const meta = STATUS_META[probe.status] || STATUS_META[STATUS.FAILED];
        return (
          <Box key={probe.key}>
            <Stack direction="row" spacing={1} alignItems="center" sx={{ mb: 0.75 }}>
              <Box component="span" sx={{ display: 'inline-flex', color: statusColor(meta) }}>
                <Icon icon={meta.icon} width={16} />
              </Box>
              <Typography variant="subtitle2">{t(`channel_check.probe_${probe.key}`)}</Typography>
              {probe.category && <Chip size="small" color="error" variant="outlined" label={probe.category} />}
              {probe.latency_ms > 0 && (
                <Typography variant="caption" sx={{ color: 'text.secondary' }}>
                  {(probe.latency_ms / 1000).toFixed(2)}
                  {t('res_time.second')}
                </Typography>
              )}
              <Box sx={{ flex: 1 }} />
              {probe.response && (
                <Button
                  size="small"
                  onClick={() => setOpenResponse((prev) => ({ ...prev, [probe.key]: !prev[probe.key] }))}
                  endIcon={<Icon icon={openResponse[probe.key] ? 'solar:alt-arrow-up-linear' : 'solar:alt-arrow-down-linear'} />}
                >
                  {t('channel_check.rawResponse')}
                </Button>
              )}
            </Stack>
            <Stack spacing={0.5} sx={{ pl: 3 }}>
              {probe.results.map((r) => {
                const rMeta = STATUS_META[r.status] || STATUS_META[STATUS.FAILED];
                return (
                  <Stack key={r.name} direction="row" spacing={1} alignItems="flex-start">
                    <Typography variant="caption" sx={{ color: statusColor(rMeta), minWidth: 88, fontWeight: 500 }}>
                      {r.name}
                    </Typography>
                    <Typography variant="caption" sx={{ color: 'text.secondary', whiteSpace: 'pre-wrap', wordBreak: 'break-all' }}>
                      {r.remark}
                    </Typography>
                  </Stack>
                );
              })}
            </Stack>
            {probe.response && (
              <Collapse in={!!openResponse[probe.key]} unmountOnExit>
                <Box
                  component="pre"
                  sx={{
                    // 主题给表格单元格设了居中，pre 会继承，原始 JSON 必须左对齐
                    textAlign: 'left',
                    m: 0,
                    mt: 1,
                    ml: 3,
                    p: 1.5,
                    maxHeight: 240,
                    overflow: 'auto',
                    borderRadius: 1,
                    fontSize: 12,
                    whiteSpace: 'pre-wrap',
                    wordBreak: 'break-all',
                    bgcolor: (theme) => (theme.palette.mode === 'dark' ? 'rgba(0, 0, 0, 0.2)' : 'grey.50')
                  }}
                >
                  {JSON.stringify(probe.response, null, 2)}
                </Box>
              </Collapse>
            )}
          </Box>
        );
      })}
    </Stack>
  );
}

DetailPanel.propTypes = {
  result: PropTypes.object,
  t: PropTypes.func
};

export function ChannelCheck({ item, open, onClose }) {
  const { t } = useTranslation();
  const stopRef = useRef(false);
  // 批次号：关闭/卸载/重新发起时自增，丢弃上一批在途请求的迟到写回
  const runIdRef = useRef(0);
  const [models, setModels] = useState([]);
  const [selected, setSelected] = useState([]);
  const [results, setResults] = useState({});
  const [expanded, setExpanded] = useState({});
  const [running, setRunning] = useState(false);
  const [fetching, setFetching] = useState(false);
  const [progress, setProgress] = useState({ done: 0, total: 0 });
  const selectedSet = useMemo(() => new Set(selected), [selected]);

  // 打开时用渠道已配置的模型重置；关闭或卸载时停止领取新任务并作废在途批次
  useEffect(() => {
    if (!open) return undefined;
    const initial = Array.from(new Set(splitCsv(item?.models)));
    setModels(initial);
    setSelected(initial);
    setResults({});
    setExpanded({});
    setRunning(false);
    setProgress({ done: 0, total: 0 });
    return () => {
      stopRef.current = true;
      runIdRef.current += 1;
    };
  }, [open, item?.models]);

  const handleFetchUpstream = useCallback(async () => {
    setFetching(true);
    try {
      // 列表接口不返回 key，后端会按 id 读取完整渠道配置
      const res = await API.post(`/api/channel/provider_models_list`, { id: item.id });
      if (!res?.data) return;
      const { success, message, data } = res.data;
      if (success && Array.isArray(data)) {
        const list = Array.from(new Set(data));
        setModels(list);
        setSelected(list);
        setResults({});
        setExpanded({});
        setProgress({ done: 0, total: 0 });
      } else {
        showError(message || t('channel_check.fetchFailed'));
      }
    } finally {
      setFetching(false);
    }
  }, [item?.id, t]);

  const handleToggleModel = useCallback((model) => {
    setSelected((prev) => (prev.includes(model) ? prev.filter((m) => m !== model) : [...prev, model]));
  }, []);
  const handleSelectAll = useCallback(() => setSelected(models), [models]);
  const handleClearAll = useCallback(() => setSelected([]), []);
  const handleInvert = useCallback(() => setSelected((prev) => models.filter((model) => !prev.includes(model))), [models]);
  const handleStop = useCallback(() => {
    stopRef.current = true;
  }, []);

  // checkOne 检测单个模型并写回该行；返回 null 表示所属批次已作废（对话框已关闭或重开）
  const checkOne = useCallback(
    async (model, runId) => {
      setResults((prev) => ({ ...prev, [model]: { status: 'checking' } }));
      let rowResult;
      try {
        const res = await API.get(`/api/channel/check/${item.id}`, { params: { model } });
        // API 拦截器在 HTTP 层出错时已弹过提示，且返回 undefined 而非 reject
        if (!res?.data) throw new Error(t('channel_check.statusFailed'));
        const { success, message, data } = res.data;
        rowResult = success && data ? { status: 'done', data } : { status: 'error', message: message || t('channel_check.statusFailed') };
      } catch (error) {
        rowResult = { status: 'error', message: error.message || t('channel_check.statusFailed') };
      }
      if (runId !== runIdRef.current) return null;
      setResults((prev) => ({ ...prev, [model]: rowResult }));
      return rowResult;
    },
    [item?.id, t]
  );

  // runBatch 批量检测 targets；reset 为 false 时保留其他行的结果（用于重试失败项）
  const runBatch = useCallback(
    async (targets, reset) => {
      stopRef.current = false;
      const runId = (runIdRef.current += 1);
      // 关闭后重开再发起会复位 stopRef，仅靠它拦不住旧批次继续领取任务，需同时校验批次号
      const isStopped = () => stopRef.current || runId !== runIdRef.current;
      setRunning(true);
      if (reset) {
        setResults({});
        setExpanded({});
      }
      setProgress({ done: 0, total: targets.length });

      let done = 0;
      const batchCounts = { passed: 0, warning: 0, failed: 0 };
      await runWithConcurrency(
        targets,
        CHECK_CONCURRENCY,
        async (model) => {
          const rowResult = await checkOne(model, runId);
          if (!rowResult) return;
          batchCounts[rowOutcome(rowResult)] += 1;
          done += 1;
          setProgress({ done, total: targets.length });
        },
        isStopped
      );

      if (runId !== runIdRef.current) return;
      setRunning(false);
      const summary = t('channel_check.summary', batchCounts);
      if (stopRef.current && done < targets.length) showInfo(`${t('channel_check.stopped')}${summary}`);
      else if (batchCounts.failed > 0) showError(summary);
      else showInfo(summary);
    },
    [checkOne, t]
  );

  const handleStart = useCallback(
    () =>
      runBatch(
        models.filter((model) => selectedSet.has(model)),
        true
      ),
    [models, runBatch, selectedSet]
  );

  const failedModels = useMemo(
    () => models.filter((model) => results[model] && results[model].status !== 'checking' && rowOutcome(results[model]) === 'failed'),
    [models, results]
  );
  const handleRetryFailed = useCallback(() => runBatch(failedModels, false), [failedModels, runBatch]);

  // 单行检测不新建批次：复用当前批次号，其他行的结果保持不变，关闭对话框时同样作废
  const handleCheckOne = useCallback((model) => checkOne(model, runIdRef.current), [checkOne]);

  // 单行检测在途时禁止发起批量，单行检测数也不超过并发上限，避免叠加打满上游
  const checkingCount = useMemo(() => Object.values(results).filter((r) => r.status === 'checking').length, [results]);
  const busy = running || checkingCount > 0;
  const selectedCount = selected.length;
  const progressValue = progress.total === 0 ? 0 : Math.round((progress.done / progress.total) * 100);
  const columnCount = PROBE_KEYS.length + 5;

  return (
    <Dialog
      fullWidth
      maxWidth={false}
      open={open}
      onClose={onClose}
      PaperProps={{ sx: { maxWidth: 960, borderRadius: 2, backgroundImage: 'none' } }}
    >
      <DialogTitle sx={{ pb: 2, borderBottom: 1, borderColor: 'divider' }}>
        <Stack direction="row" alignItems="center" justifyContent="space-between" spacing={2}>
          <Box sx={{ minWidth: 0 }}>
            <Typography variant="h5" sx={{ fontWeight: 500 }} noWrap>
              {t('channel_check.title')}
            </Typography>
            <Typography variant="caption" sx={{ color: 'text.secondary' }} noWrap component="div">
              {item?.name} · #{item?.id}
            </Typography>
          </Box>
          <LoadingButton
            size="small"
            variant="outlined"
            loading={fetching}
            disabled={busy}
            onClick={handleFetchUpstream}
            startIcon={<Icon icon="solar:refresh-bold" />}
          >
            {t('channel_check.fetchUpstream')}
          </LoadingButton>
        </Stack>
      </DialogTitle>

      <DialogContent sx={{ py: 2 }}>
        <Stack spacing={2}>
          <Alert severity="info" variant="outlined" sx={{ py: 0 }}>
            {t('channel_check.hint')}
          </Alert>

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
              <>
                {failedModels.length > 0 && (
                  <Button
                    size="small"
                    color="error"
                    variant="outlined"
                    onClick={handleRetryFailed}
                    disabled={busy || fetching}
                    startIcon={<Icon icon="solar:restart-bold" />}
                  >
                    {t('channel_row.retryFailed', { count: failedModels.length })}
                  </Button>
                )}
                <Button
                  size="small"
                  variant="contained"
                  onClick={handleStart}
                  disabled={selectedCount === 0 || fetching || busy}
                  startIcon={<Icon icon="solar:play-bold" />}
                >
                  {t('channel_check.start')}
                </Button>
              </>
            )}
          </Stack>

          {progress.total > 0 && (
            <Box>
              <LinearProgress variant="determinate" value={progressValue} sx={{ height: 6, borderRadius: 1 }} />
              <Typography variant="caption" sx={{ color: 'text.secondary', mt: 0.5, display: 'block' }}>
                {progress.done}/{progress.total}
              </Typography>
            </Box>
          )}

          <Paper variant="outlined" sx={{ borderRadius: 2, overflow: 'hidden' }}>
            <TableContainer sx={{ maxHeight: 460 }}>
              <Table size="small" stickyHeader>
                <TableHead>
                  <TableRow>
                    <TableCell padding="checkbox" />
                    <TableCell align="left">{t('channel_index.model')}</TableCell>
                    <TableCell align="left" width={96}>
                      {t('channel_index.status')}
                    </TableCell>
                    {PROBE_KEYS.map((key) => (
                      <TableCell key={key} align="center" width={84}>
                        {t(`channel_check.probe_${key}`)}
                      </TableCell>
                    ))}
                    <TableCell width={56} align="center">
                      {t('common.action')}
                    </TableCell>
                    <TableCell padding="checkbox" />
                  </TableRow>
                </TableHead>
                <TableBody>
                  {models.length === 0 && (
                    <TableRow>
                      <TableCell colSpan={columnCount} align="center" sx={{ py: 4 }}>
                        <Typography variant="body2" sx={{ color: 'text.secondary' }}>
                          {t('channel_row.multiTestNoModels')}
                        </Typography>
                      </TableCell>
                    </TableRow>
                  )}
                  {models.map((model) => {
                    const result = results[model];
                    const probes = result?.status === 'done' ? Object.fromEntries(result.data.items.map((p) => [p.key, p])) : {};
                    const expandable = result?.status === 'done' || result?.status === 'error';
                    return (
                      <Fragment key={model}>
                        <TableRow hover sx={{ '& > td': { borderBottom: expandable && expanded[model] ? 'none' : undefined } }}>
                          <TableCell padding="checkbox">
                            <Checkbox
                              size="small"
                              checked={selectedSet.has(model)}
                              disabled={running}
                              onChange={() => handleToggleModel(model)}
                            />
                          </TableCell>
                          <TableCell align="left">
                            <Typography variant="body2" sx={{ fontFamily: 'monospace', wordBreak: 'break-all' }}>
                              {model}
                            </Typography>
                          </TableCell>
                          <TableCell align="left">
                            <OverallCell result={result} t={t} />
                          </TableCell>
                          {PROBE_KEYS.map((key) => (
                            <TableCell key={key} align="center">
                              <ProbeCell probe={probes[key]} />
                            </TableCell>
                          ))}
                          <TableCell align="center">
                            <RowActionButton
                              retry={expandable && rowOutcome(result) === 'failed'}
                              label={t('channel_check.checkOne')}
                              disabled={running || fetching || checkingCount >= CHECK_CONCURRENCY || result?.status === 'checking'}
                              onClick={() => handleCheckOne(model)}
                            />
                          </TableCell>
                          <TableCell padding="checkbox">
                            <IconButton
                              size="small"
                              disabled={!expandable}
                              aria-label={t('channel_check.details')}
                              onClick={() => setExpanded((prev) => ({ ...prev, [model]: !prev[model] }))}
                            >
                              <Icon icon={expanded[model] ? 'solar:alt-arrow-up-linear' : 'solar:alt-arrow-down-linear'} width={18} />
                            </IconButton>
                          </TableCell>
                        </TableRow>
                        {expandable && (
                          <TableRow>
                            <TableCell
                              colSpan={columnCount}
                              align="left"
                              sx={{ py: 0, ...(expanded[model] ? {} : { borderBottom: 'none' }) }}
                            >
                              <Collapse in={!!expanded[model]} unmountOnExit>
                                <Box sx={{ py: 2, px: 1, textAlign: 'left' }}>
                                  <DetailPanel result={result} t={t} />
                                </Box>
                              </Collapse>
                            </TableCell>
                          </TableRow>
                        )}
                      </Fragment>
                    );
                  })}
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

ChannelCheck.propTypes = {
  item: PropTypes.object,
  open: PropTypes.bool,
  onClose: PropTypes.func
};
