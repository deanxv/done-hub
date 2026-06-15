import { useCallback, useState } from 'react';
import { Button, TextField } from '@mui/material';
import { IconSend } from '@tabler/icons-react';
import { API } from 'utils/api';
import { showError, showSuccess } from 'utils/common';
import { useTranslation } from 'react-i18next';
import BatchChannelSelector from './BatchChannelSelector';

const AZURE_CHANNEL_TYPE = 3;

const BatchAzureAPI = () => {
  const { t } = useTranslation();
  const [keyword, setKeyword] = useState('');
  const [selectedIds, setSelectedIds] = useState(() => new Set());
  const [replaceValue, setReplaceValue] = useState('');
  const [loading, setLoading] = useState(false);
  const [refreshSignal, setRefreshSignal] = useState(0);

  const searchFilter = useCallback((kw) => {
    const trimmed = (kw || '').trim();
    return trimmed ? { other: trimmed, type: AZURE_CHANNEL_TYPE } : { type: AZURE_CHANNEL_TYPE };
  }, []);

  const subtitleRender = useCallback((item) => `${t('channel_index.currentAzureAPIVersion')}: ${item.other || '-'}`, [t]);

  const handleSubmit = async () => {
    const target = replaceValue.trim();
    setLoading(true);
    try {
      const res = await API.put(`/api/channel/batch/azure_api`, {
        ids: [...selectedIds],
        value: target
      });
      const { success, message, data } = res.data;
      if (success) {
        showSuccess(t('channel_index.batchAzureAPISuccess', { count: data }));
        // 保留 replaceValue：用户常需要把同一新版本继续应用到别的渠道
        // 但 keyword（旧版本号）要清：成功后这些渠道的 other 已变成 replaceValue，
        // 沿用旧 keyword 刷新会让列表"突然空了"，体验断层
        setSelectedIds(new Set());
        setKeyword('');
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

  return (
    <BatchChannelSelector
      tip={t('channel_index.batchAzureAPITip')}
      searchPlaceholder={t('channel_index.azureApiSearchPlaceholder')}
      keyword={keyword}
      onKeywordChange={setKeyword}
      searchFilter={searchFilter}
      clearSelectionOnSearch
      subtitleRender={subtitleRender}
      selectedIds={selectedIds}
      onSelectedChange={setSelectedIds}
      refreshSignal={refreshSignal}
    >
      <TextField
        fullWidth
        size="medium"
        label={t('channel_index.newAzureAPIVersion')}
        placeholder={t('channel_index.inputAPIVersion')}
        value={replaceValue}
        onChange={(e) => setReplaceValue(e.target.value)}
        sx={{ '& .MuiInputBase-root': { height: '48px' } }}
      />
      <Button
        variant="contained"
        onClick={handleSubmit}
        disabled={loading || selectedIds.size === 0 || !replaceValue.trim()}
        startIcon={<IconSend />}
        fullWidth
      >
        {loading ? t('channel_index.replacing') : t('channel_index.replaceAzureAPIVersion', { count: selectedIds.size })}
      </Button>
    </BatchChannelSelector>
  );
};

export default BatchAzureAPI;
