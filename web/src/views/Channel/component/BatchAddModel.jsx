import { useCallback, useMemo, useState } from 'react';
import PropTypes from 'prop-types';
import { Autocomplete, Button, Checkbox, Chip, TextField } from '@mui/material';
import { createFilterOptions } from '@mui/material/Autocomplete';
import CheckBoxIcon from '@mui/icons-material/CheckBox';
import CheckBoxOutlineBlankIcon from '@mui/icons-material/CheckBoxOutlineBlank';
import { IconPlus } from '@tabler/icons-react';
import { API } from 'utils/api';
import { copy, showError, showSuccess } from 'utils/common';
import { useTranslation } from 'react-i18next';
import BatchChannelSelector from './BatchChannelSelector';
import { splitCsv } from './batchHelpers';

const icon = <CheckBoxOutlineBlankIcon fontSize="small" />;
const checkedIcon = <CheckBoxIcon fontSize="small" />;
const filter = createFilterOptions();

const normalizeModels = (list) => list.map((item) => (typeof item === 'string' ? item : item.id)).filter(Boolean);

const BatchAddModel = ({ modelOptions }) => {
  const { t } = useTranslation();
  const [keyword, setKeyword] = useState('');
  const [selectedIds, setSelectedIds] = useState(() => new Set());
  const [selectedModels, setSelectedModels] = useState([]);
  const [inputValue, setInputValue] = useState('');
  const [loading, setLoading] = useState(false);
  const [refreshSignal, setRefreshSignal] = useState(0);

  const searchFilter = useCallback((kw) => {
    const trimmed = (kw || '').trim();
    return trimmed ? { name: trimmed } : {};
  }, []);

  const subtitleRender = useCallback((item) => `${t('channel_index.currentModels')}: ${item.models || t('channel_index.noModels')}`, [t]);

  const targetModels = useMemo(() => normalizeModels(selectedModels), [selectedModels]);

  const itemConflict = useMemo(() => {
    if (targetModels.length === 0) return null;
    return (item) => {
      const channelModels = new Set(splitCsv(item.models));
      const existingCount = targetModels.reduce((acc, m) => acc + (channelModels.has(m) ? 1 : 0), 0);
      if (existingCount === 0) return null;
      if (existingCount === targetModels.length) {
        return { disabled: true, hint: t('channel_index.channelAlreadyHasAllModels') };
      }
      return { disabled: false, hint: t('channel_index.channelAlreadyHasPartialModels') };
    };
  }, [targetModels, t]);

  const handleSubmit = async () => {
    setLoading(true);
    try {
      const modelsString = targetModels.join(',');
      const res = await API.put(`/api/channel/batch/add_model`, {
        ids: [...selectedIds],
        value: modelsString
      });
      const { success, message, data } = res.data;
      if (success) {
        showSuccess(t('channel_index.batchAddModelSuccess', { count: data, model: modelsString }));
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
      tip={t('channel_index.batchAddModelTip')}
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
        multiple
        freeSolo
        disableCloseOnSelect
        options={modelOptions}
        value={selectedModels}
        inputValue={inputValue}
        onInputChange={(event, newInputValue) => {
          if (newInputValue.includes(',')) {
            const tokens = splitCsv(newInputValue);
            const updated = [...selectedModels];
            tokens.forEach((id) => {
              if (updated.some((u) => (typeof u === 'string' ? u : u.id) === id)) return;
              // 若该 id 已存在于 modelOptions，沿用其原分组，避免被替换成「自定义」分组
              const existing = modelOptions.find((o) => o.id === id);
              updated.push(existing || { id, group: t('channel_edit.customModelTip') });
            });
            setSelectedModels(updated);
            setInputValue('');
          } else {
            setInputValue(newInputValue);
          }
        }}
        onChange={(e, value) => {
          setSelectedModels(
            value.map((item) => {
              if (typeof item !== 'string') return item;
              // freeSolo 回车输入的字符串：优先匹配既有 option，保留其原分组
              const existing = modelOptions.find((o) => o.id === item);
              return existing || { id: item, group: t('channel_edit.customModelTip') };
            })
          );
        }}
        renderInput={(params) => (
          <TextField
            {...params}
            label={t('channel_index.selectModelToAdd')}
            placeholder={t('channel_index.pleaseSelectModel')}
            helperText={t('channel_index.modelInputTip')}
          />
        )}
        groupBy={(option) => option.group}
        getOptionLabel={(option) => {
          if (typeof option === 'string') return option;
          if (option.inputValue) return option.inputValue;
          return option.id;
        }}
        filterOptions={(options, params) => {
          const filtered = filter(options, params);
          const { inputValue: iv } = params;
          const isExisting = options.some((option) => iv === option.id);
          if (iv !== '' && !isExisting) {
            filtered.push({ id: iv, group: t('channel_edit.customModelTip') });
          }
          return filtered;
        }}
        renderOption={(props, option, { selected }) => (
          <li {...props}>
            <Checkbox icon={icon} checkedIcon={checkedIcon} style={{ marginRight: 8 }} checked={selected} />
            {option.id}
          </li>
        )}
        renderTags={(value, getTagProps) =>
          value.map((option, index) => {
            const tagProps = getTagProps({ index });
            return (
              <Chip
                key={index}
                label={option.id}
                {...tagProps}
                onClick={() => copy(option.id)}
                sx={{
                  maxWidth: '100%',
                  height: 'auto',
                  margin: '3px',
                  '& .MuiChip-label': {
                    whiteSpace: 'normal',
                    wordBreak: 'break-word',
                    padding: '6px 8px',
                    lineHeight: 1.4,
                    fontWeight: 400
                  },
                  '& .MuiChip-deleteIcon': { margin: '0 5px 0 -6px' }
                }}
              />
            );
          })
        }
        sx={{
          '& .MuiAutocomplete-tag': { margin: '2px' },
          '& .MuiAutocomplete-inputRoot': { flexWrap: 'wrap' }
        }}
      />
      <Button
        variant="contained"
        onClick={handleSubmit}
        disabled={loading || selectedIds.size === 0 || targetModels.length === 0}
        startIcon={<IconPlus />}
        fullWidth
      >
        {loading ? t('channel_index.addingModel') : t('channel_index.addModelToChannels', { count: selectedIds.size })}
      </Button>
    </BatchChannelSelector>
  );
};

BatchAddModel.propTypes = {
  modelOptions: PropTypes.array
};

export default BatchAddModel;
