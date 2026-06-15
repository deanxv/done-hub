import { useCallback, useMemo, useState } from 'react';
import PropTypes from 'prop-types';
import { Autocomplete, Button, TextField } from '@mui/material';
import { IconUserPlus } from '@tabler/icons-react';
import { API } from 'utils/api';
import { showError, showSuccess } from 'utils/common';
import { useTranslation } from 'react-i18next';
import BatchChannelSelector from './BatchChannelSelector';
import { splitCsv } from './batchHelpers';

const BatchAddUserGroup = ({ groupOptions }) => {
  const { t } = useTranslation();
  const [keyword, setKeyword] = useState('');
  const [selectedIds, setSelectedIds] = useState(() => new Set());
  const [selectedGroup, setSelectedGroup] = useState('');
  const [loading, setLoading] = useState(false);
  const [refreshSignal, setRefreshSignal] = useState(0);

  const searchFilter = useCallback((kw) => {
    const trimmed = (kw || '').trim();
    return trimmed ? { name: trimmed } : {};
  }, []);

  const subtitleRender = useCallback((item) => `${t('channel_index.currentGroup')}: ${item.group || t('channel_index.noGroup')}`, [t]);

  const itemConflict = useMemo(() => {
    if (!selectedGroup) return null;
    return (item) => {
      const groups = splitCsv(item.group);
      if (groups.includes(selectedGroup)) {
        return { disabled: true, hint: t('channel_index.channelAlreadyHasGroup') };
      }
      return null;
    };
  }, [selectedGroup, t]);

  const handleSubmit = async () => {
    setLoading(true);
    try {
      const res = await API.put(`/api/channel/batch/add_user_group`, {
        ids: [...selectedIds],
        value: selectedGroup
      });
      const { success, message, data } = res.data;
      if (success) {
        showSuccess(t('channel_index.batchAddUserGroupSuccess', { count: data, group: selectedGroup }));
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

  return (
    <BatchChannelSelector
      tip={t('channel_index.batchAddUserGroupTip')}
      keyword={keyword}
      onKeywordChange={setKeyword}
      searchFilter={searchFilter}
      subtitleRender={subtitleRender}
      itemConflict={itemConflict}
      selectedIds={selectedIds}
      onSelectedChange={setSelectedIds}
      refreshSignal={refreshSignal}
    >
      <Autocomplete
        options={groupOptions}
        value={selectedGroup || null}
        onChange={(event, newValue) => setSelectedGroup(newValue || '')}
        renderInput={(params) => (
          <TextField {...params} label={t('channel_index.selectUserGroupToAdd')} placeholder={t('channel_index.pleaseSelectUserGroup')} />
        )}
      />
      <Button
        variant="contained"
        onClick={handleSubmit}
        disabled={loading || selectedIds.size === 0 || !selectedGroup}
        startIcon={<IconUserPlus />}
        fullWidth
      >
        {loading ? t('channel_index.addingUserGroup') : t('channel_index.addUserGroupToChannels', { count: selectedIds.size })}
      </Button>
    </BatchChannelSelector>
  );
};

BatchAddUserGroup.propTypes = {
  groupOptions: PropTypes.array
};

export default BatchAddUserGroup;
