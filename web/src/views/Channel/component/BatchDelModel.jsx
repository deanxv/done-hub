import { useCallback, useMemo, useState } from 'react';
import { Button } from '@mui/material';
import { IconTrash } from '@tabler/icons-react';
import { API } from 'utils/api';
import { showError, showSuccess } from 'utils/common';
import { useTranslation } from 'react-i18next';
import BatchChannelSelector from './BatchChannelSelector';
import { splitCsv } from './batchHelpers';

const BatchDelModel = () => {
  const { t } = useTranslation();
  const [keyword, setKeyword] = useState('');
  const [selectedIds, setSelectedIds] = useState(() => new Set());
  const [loading, setLoading] = useState(false);
  const [refreshSignal, setRefreshSignal] = useState(0);

  const searchFilter = useCallback((kw) => {
    const trimmed = (kw || '').trim();
    if (!trimmed) return null;
    return { models: trimmed };
  }, []);

  const filterData = useCallback((rows, kw) => {
    const target = (kw || '').trim();
    if (!target) return [];
    return rows.filter((row) => splitCsv(row.models).includes(target));
  }, []);

  const subtitleRender = useCallback((item) => `${t('channel_index.currentModels')}: ${item.models || t('channel_index.noModels')}`, [t]);

  // 「只剩此模型」的处理：旧实现直接从列表过滤掉，新实现在列表中保留但 disabled+提示
  // 是有意的 UX 升级——透明告知"为何不可删"，比凭空消失更清晰
  const itemConflict = useMemo(() => {
    const target = (keyword || '').trim();
    if (!target) return null;
    return (item) => {
      const models = splitCsv(item.models);
      if (models.length <= 1 && models.includes(target)) {
        return { disabled: true, hint: t('channel_index.channelOnlyHasThisModel') };
      }
      return null;
    };
  }, [keyword, t]);

  const handleSubmit = async () => {
    const target = keyword.trim();
    setLoading(true);
    try {
      const res = await API.put(`/api/channel/batch/del_model`, {
        ids: [...selectedIds],
        value: target
      });
      const { success, message, data } = res.data;
      if (success) {
        showSuccess(t('channel_index.batchDeleteSuccess', { count: data }));
        setSelectedIds(new Set());
        setRefreshSignal((s) => s + 1);
      } else {
        showError(message);
      }
    } catch (error) {
      showError(error.message);
    } finally {
      setLoading(false);
    }
  };

  const target = keyword.trim();

  return (
    <BatchChannelSelector
      tip={t('channel_index.batchDeleteTipV2')}
      searchPlaceholder={t('channel_index.modelToDelete')}
      keyword={keyword}
      onKeywordChange={setKeyword}
      searchFilter={searchFilter}
      filterData={filterData}
      autoLoad={false}
      clearSelectionOnSearch
      emptyHint={t('channel_index.delModelEmptyHint')}
      subtitleRender={subtitleRender}
      itemConflict={itemConflict}
      selectedIds={selectedIds}
      onSelectedChange={setSelectedIds}
      refreshSignal={refreshSignal}
    >
      <Button
        variant="contained"
        color="error"
        onClick={handleSubmit}
        disabled={loading || selectedIds.size === 0 || !target}
        startIcon={<IconTrash />}
        fullWidth
      >
        {loading
          ? t('channel_index.deleting')
          : t('channel_index.confirmDeleteModel', {
              count: selectedIds.size,
              model: target || '...'
            })}
      </Button>
    </BatchChannelSelector>
  );
};

export default BatchDelModel;
