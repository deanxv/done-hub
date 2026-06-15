import { useCallback, useEffect, useMemo, useState } from 'react';
import PropTypes from 'prop-types';
import {
  Alert,
  Box,
  Button,
  Checkbox,
  CircularProgress,
  Grid,
  IconButton,
  InputAdornment,
  TextField,
  Typography,
  useMediaQuery,
  useTheme
} from '@mui/material';
import { gridSpacing } from 'store/constant';
import { IconChevronDown, IconChevronLeft, IconChevronRight, IconChevronUp, IconSearch, IconX } from '@tabler/icons-react';
import { useTranslation } from 'react-i18next';
import { fetchChannelData } from '../index';

const PAGE_SIZE = 100;

// 选择/搜索/翻页行为约定：
// - 翻页（prev/next）：保留 selectedIds，跨页累积
// - 刷新（refreshSignal 变化）：保留 selectedIds，列表内容会重新拉但选中不丢
// - 搜索 / 清键（handleSearch / handleClearKeyword）：
//     · 默认（clearSelectionOnSearch=false）保留 selectedIds — 跨搜索累积
//     · 若 keyword 与提交语义强相关（如 DelModel 的 keyword 即删除目标），调用方需传 true
// - 行变 disabled：useEffect 自动从 selectedIds 剔除，避免 checked+disabled 死锁
const BatchChannelSelector = ({
  tip,
  searchPlaceholder,
  keyword,
  onKeywordChange,
  searchFilter,
  autoLoad,
  subtitleRender,
  itemConflict,
  filterData,
  clearSelectionOnSearch,
  emptyHint,
  selectedIds,
  onSelectedChange,
  refreshSignal,
  children
}) => {
  const { t } = useTranslation();
  const theme = useTheme();
  const isMobile = useMediaQuery(theme.breakpoints.down('sm'));

  const [page, setPage] = useState(0);
  const [data, setData] = useState([]);
  const [totalCount, setTotalCount] = useState(0);
  const [searching, setSearching] = useState(false);
  const [expandedIds, setExpandedIds] = useState({});
  const [initialized, setInitialized] = useState(false);

  const truncateLen = isMobile ? 40 : 70;

  const totalPages = useMemo(() => {
    if (totalCount <= 0) return 0;
    return Math.ceil(totalCount / PAGE_SIZE);
  }, [totalCount]);

  const fetchPage = useCallback(
    async (targetPage, currentKeyword) => {
      const params = searchFilter ? searchFilter(currentKeyword || '') : {};
      if (params === null || params === undefined) {
        // searchFilter 明确拒绝查询（例如 DelModel 在空 keyword 时）
        setData([]);
        setTotalCount(0);
        setInitialized(true);
        return;
      }
      setSearching(true);
      try {
        const result = await fetchChannelData(targetPage, PAGE_SIZE, params, 'desc', 'id');
        if (result) {
          const rawRows = result.data || [];
          // totalCount 始终用后端 total_count（驱动分页器）
          // filterData 模式下 data 是严格筛后的结果；UI 文案会切换为 selectionStatusFiltered，
          // 区分「本页严格匹配 / 服务器粗筛总数」，避免歧义
          const rows = filterData ? filterData(rawRows, currentKeyword || '') : rawRows;
          setData(rows);
          setTotalCount(result.total_count ?? rawRows.length);
        } else {
          setData([]);
          setTotalCount(0);
        }
      } finally {
        setSearching(false);
        setInitialized(true);
      }
    },
    [searchFilter, filterData]
  );

  useEffect(() => {
    if (autoLoad) {
      fetchPage(0, keyword);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    if (!initialized) return;
    // 提交成功后通常列表会大改：已加完模型/删过模型/改过 Azure 版本的渠道会被
    // itemConflict 标 disabled、自动从 selectedIds 剔除。统一回到第 0 页让用户
    // 从头确认状态，避免「停在第 3 页但首页才有剩下要处理的渠道」的脱节
    setPage(0);
    fetchPage(0, keyword);
    // 仅追踪 refreshSignal：keyword/fetchPage 在闭包中取最新值即可，
    // 列入依赖会让每次改 keyword 都重复触发刷新
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [refreshSignal]);

  const handleSearch = useCallback(() => {
    if (clearSelectionOnSearch) onSelectedChange(new Set());
    setPage(0);
    fetchPage(0, keyword);
  }, [fetchPage, keyword, onSelectedChange, clearSelectionOnSearch]);

  const handleKeywordChange = (value) => {
    if (onKeywordChange) onKeywordChange(value);
  };

  const handleClearKeyword = () => {
    handleKeywordChange('');
    if (clearSelectionOnSearch) onSelectedChange(new Set());
    setPage(0);
    fetchPage(0, '');
  };

  const handlePrevPage = () => {
    if (page <= 0) return;
    const nextPage = page - 1;
    setPage(nextPage);
    fetchPage(nextPage, keyword);
  };

  const handleNextPage = () => {
    if (page + 1 >= totalPages) return;
    const nextPage = page + 1;
    setPage(nextPage);
    fetchPage(nextPage, keyword);
  };

  const conflictByItem = useMemo(() => {
    const map = new Map();
    data.forEach((item) => {
      const conflict = itemConflict ? itemConflict(item) : null;
      map.set(item.id, conflict || null);
    });
    return map;
  }, [data, itemConflict]);

  // 已选中后又变为 disabled（例如选了渠道再选模型导致冲突）时，自动剔除
  // 否则 Checkbox 会保持 checked + disabled，用户无法手动取消
  // 只在 conflictByItem 变化时触发；selectedIds 变化由 toggleSelect 同步处理，
  // 不需要在 selectedIds 变化时重新跑此 effect（也能避免引入循环依赖）
  useEffect(() => {
    if (selectedIds.size === 0) return;
    let stale = false;
    const next = new Set(selectedIds);
    conflictByItem.forEach((conflict, id) => {
      if (conflict?.disabled && next.has(id)) {
        next.delete(id);
        stale = true;
      }
    });
    if (stale) onSelectedChange(next);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [conflictByItem]);

  const selectableIdsOnPage = useMemo(
    () => data.filter((item) => !conflictByItem.get(item.id)?.disabled).map((item) => item.id),
    [data, conflictByItem]
  );

  const selectedOnPageCount = useMemo(() => data.reduce((acc, item) => acc + (selectedIds.has(item.id) ? 1 : 0), 0), [data, selectedIds]);

  const allSelectableSelected = selectableIdsOnPage.length > 0 && selectableIdsOnPage.every((id) => selectedIds.has(id));

  const toggleSelect = (id) => {
    const next = new Set(selectedIds);
    if (next.has(id)) {
      next.delete(id);
    } else {
      next.add(id);
    }
    onSelectedChange(next);
  };

  const handleToggleAllOnPage = () => {
    if (selectableIdsOnPage.length === 0) return;
    const next = new Set(selectedIds);
    if (allSelectableSelected) {
      selectableIdsOnPage.forEach((id) => next.delete(id));
    } else {
      selectableIdsOnPage.forEach((id) => next.add(id));
    }
    onSelectedChange(next);
  };

  const handleClearSelection = () => {
    onSelectedChange(new Set());
  };

  const toggleExpanded = (id) => {
    setExpandedIds((prev) => ({ ...prev, [id]: !prev[id] }));
  };

  return (
    <Grid container spacing={gridSpacing}>
      {tip && (
        <Grid item xs={12}>
          <Alert severity="info">{tip}</Alert>
        </Grid>
      )}

      <Grid item xs={12}>
        <TextField
          fullWidth
          size="medium"
          placeholder={searchPlaceholder || t('channel_index.searchChannelPlaceholder')}
          inputProps={{ 'aria-label': t('channel_index.searchChannelLabel') }}
          value={keyword || ''}
          onChange={(e) => handleKeywordChange(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault();
              handleSearch();
            }
          }}
          InputProps={{
            endAdornment: (
              <InputAdornment position="end">
                {keyword ? (
                  <IconButton aria-label={t('channel_index.clearKeyword')} onClick={handleClearKeyword} edge="end" size="small">
                    <IconX size={18} />
                  </IconButton>
                ) : null}
                {searching ? (
                  <CircularProgress size={20} sx={{ ml: 1 }} />
                ) : (
                  <IconButton aria-label={t('channel_index.searchChannelLabel')} onClick={handleSearch} edge="end">
                    <IconSearch />
                  </IconButton>
                )}
              </InputAdornment>
            )
          }}
          sx={{ '& .MuiInputBase-root': { height: '48px' } }}
        />
      </Grid>

      {(data.length > 0 || selectedIds.size > 0) && (
        <Grid item xs={12}>
          <Box
            sx={{
              display: 'flex',
              flexWrap: 'wrap',
              justifyContent: 'space-between',
              alignItems: 'center',
              gap: 1
            }}
          >
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, flexWrap: 'wrap' }}>
              <Button size="small" onClick={handleToggleAllOnPage} disabled={selectableIdsOnPage.length === 0}>
                {allSelectableSelected ? t('channel_index.unselectCurrentPage') : t('channel_index.selectCurrentPage')}
              </Button>
              {selectedIds.size > 0 && (
                <Button size="small" color="inherit" onClick={handleClearSelection}>
                  {t('channel_index.clearSelection')}
                </Button>
              )}
            </Box>
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 2, flexWrap: 'wrap' }}>
              <Typography variant="caption" color="text.secondary">
                {t(filterData ? 'channel_index.selectionStatusFiltered' : 'channel_index.selectionStatus', {
                  selected: selectedIds.size,
                  onPage: selectedOnPageCount,
                  count: data.length,
                  total: totalCount
                })}
              </Typography>
              {totalPages > 0 && (
                <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>
                  <IconButton
                    size="small"
                    aria-label={t('channel_index.previousPage')}
                    onClick={handlePrevPage}
                    disabled={page <= 0 || searching}
                  >
                    <IconChevronLeft size={18} />
                  </IconButton>
                  <Typography variant="caption" color="text.secondary" sx={{ minWidth: 60, textAlign: 'center' }}>
                    {t('channel_index.pageInfo', { page: page + 1, total: totalPages })}
                  </Typography>
                  <IconButton
                    size="small"
                    aria-label={t('channel_index.nextPage')}
                    onClick={handleNextPage}
                    disabled={page + 1 >= totalPages || searching}
                  >
                    <IconChevronRight size={18} />
                  </IconButton>
                </Box>
              )}
            </Box>
          </Box>
        </Grid>
      )}

      <Grid item xs={12} sx={{ maxHeight: 300, overflow: 'auto', minHeight: 80 }}>
        {data.length === 0 ? (
          <Typography variant="body2" color="text.secondary" align="center" sx={{ py: 2 }}>
            {searching
              ? t('channel_index.loadingChannels')
              : // 「空 keyword + 调用方给了 emptyHint」优先于 initialized 判断
                // 否则首次进入 DelModel（initialized=false）会显示"点击搜索按钮"，
                // 但 DelModel 的入口是输入框 + Enter，没有独立的"搜索按钮"概念
                !(keyword || '').trim() && emptyHint
                ? emptyHint
                : initialized
                  ? t('channel_index.noMatchingChannels')
                  : t('channel_index.clickSearchToGetChannels')}
          </Typography>
        ) : (
          data.map((item) => {
            const conflict = conflictByItem.get(item.id);
            const disabled = !!conflict?.disabled;
            const hint = conflict?.hint;
            const subtitle = subtitleRender ? subtitleRender(item) : null;
            const subtitleText = typeof subtitle === 'string' ? subtitle : '';
            const isExpanded = !!expandedIds[item.id];
            const needsTruncation = subtitleText.length > truncateLen;
            const displaySubtitle = needsTruncation && !isExpanded ? `${subtitleText.substring(0, truncateLen)}...` : subtitleText;

            return (
              <Box key={item.id} sx={{ mb: 0.5, width: '100%' }}>
                <Box
                  sx={{
                    display: 'flex',
                    alignItems: 'flex-start',
                    width: '100%',
                    padding: '4px 8px',
                    borderRadius: 1,
                    boxSizing: 'border-box',
                    '&:hover': { backgroundColor: 'action.hover' },
                    ...(disabled && {
                      opacity: 0.6,
                      backgroundColor: 'action.disabledBackground'
                    })
                  }}
                >
                  <Checkbox
                    checked={selectedIds.has(item.id)}
                    onChange={() => toggleSelect(item.id)}
                    disabled={disabled}
                    size={isMobile ? 'small' : 'medium'}
                    sx={{ p: isMobile ? 0.5 : 1, flexShrink: 0, alignSelf: 'flex-start' }}
                  />
                  <Box sx={{ flex: 1, minWidth: 0, overflow: 'hidden', pr: needsTruncation ? 0 : 1 }}>
                    <Typography
                      variant="body2"
                      sx={{
                        overflow: 'hidden',
                        textOverflow: 'ellipsis',
                        whiteSpace: 'nowrap',
                        width: '100%',
                        mb: isMobile ? 0.25 : 0.5,
                        fontSize: isMobile ? '0.875rem' : undefined
                      }}
                      title={item.name}
                    >
                      {item.name}
                      {hint && (
                        <Typography component="span" variant="caption" color={disabled ? 'warning.main' : 'text.secondary'} sx={{ ml: 1 }}>
                          {hint}
                        </Typography>
                      )}
                    </Typography>
                    {subtitleText && (
                      <Typography
                        variant="caption"
                        color="text.secondary"
                        sx={{
                          display: 'block',
                          overflow: 'hidden',
                          textOverflow: isExpanded ? 'unset' : 'ellipsis',
                          whiteSpace: isExpanded ? 'normal' : 'nowrap',
                          wordBreak: isExpanded ? 'break-all' : 'normal',
                          lineHeight: isMobile ? 1.3 : 1.4,
                          fontSize: isMobile ? '0.75rem' : undefined
                        }}
                        title={subtitleText}
                      >
                        {displaySubtitle}
                      </Typography>
                    )}
                  </Box>
                  {needsTruncation && (
                    <IconButton
                      size="small"
                      onClick={(e) => {
                        e.stopPropagation();
                        e.preventDefault();
                        toggleExpanded(item.id);
                      }}
                      sx={{
                        p: isMobile ? 0.25 : 0.5,
                        ml: isMobile ? 0.25 : 0.5,
                        flexShrink: 0,
                        alignSelf: 'flex-start'
                      }}
                    >
                      {isExpanded ? <IconChevronUp size={16} /> : <IconChevronDown size={16} />}
                    </IconButton>
                  )}
                </Box>
              </Box>
            );
          })
        )}
      </Grid>

      {children && (
        <Grid item xs={12}>
          <Box sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>{children}</Box>
        </Grid>
      )}
    </Grid>
  );
};

BatchChannelSelector.propTypes = {
  tip: PropTypes.node,
  searchPlaceholder: PropTypes.string,
  keyword: PropTypes.string,
  onKeywordChange: PropTypes.func,
  searchFilter: PropTypes.func,
  autoLoad: PropTypes.bool,
  subtitleRender: PropTypes.func,
  itemConflict: PropTypes.func,
  filterData: PropTypes.func,
  clearSelectionOnSearch: PropTypes.bool,
  emptyHint: PropTypes.string,
  selectedIds: PropTypes.instanceOf(Set).isRequired,
  onSelectedChange: PropTypes.func.isRequired,
  refreshSignal: PropTypes.number,
  children: PropTypes.node
};

BatchChannelSelector.defaultProps = {
  autoLoad: true,
  refreshSignal: 0,
  clearSelectionOnSearch: false
};

export default BatchChannelSelector;
