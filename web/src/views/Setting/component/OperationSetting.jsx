import { useContext, useEffect, useRef, useState } from 'react';
import SubCard from 'ui-component/cards/SubCard';
import QuotaInput from 'ui-component/QuotaInput';
import {
  Alert,
  Badge,
  Box,
  Button,
  Checkbox,
  Chip,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Divider,
  FormControl,
  FormControlLabel,
  Grid,
  IconButton,
  InputAdornment,
  InputLabel,
  MenuItem,
  OutlinedInput,
  Select,
  Stack,
  TextField,
  Tooltip,
  Typography
} from '@mui/material';
import TuneIcon from '@mui/icons-material/Tune';
import { showError, showSuccess, verifyJSON } from 'utils/common';
import { API } from 'utils/api';
import { AdapterDayjs } from '@mui/x-date-pickers/AdapterDayjs';
import { LocalizationProvider } from '@mui/x-date-pickers/LocalizationProvider';
import { DatePicker } from '@mui/x-date-pickers/DatePicker';
import ChatLinksDataGrid from './ChatLinksDataGrid';
import dayjs from 'dayjs';
import { LoadStatusContext } from 'contexts/StatusContext';
import { useTranslation } from 'react-i18next';
import 'dayjs/locale/zh-cn';
import { DateTimePicker } from '@mui/x-date-pickers';
import { useSelector } from 'react-redux';

const OperationSetting = () => {
  const { t } = useTranslation();
  const siteInfo = useSelector((state) => state.siteInfo);
  let now = new Date();
  let [inputs, setInputs] = useState({
    QuotaForNewUser: 0,
    QuotaForInviter: 0,
    QuotaForInvitee: 0,
    InviterRewardType: 'fixed',
    InviterRewardValue: 0,
    QuotaRemindEnabled: 'true',
    QuotaRemindThreshold: 0,
    PreConsumedQuota: 0,
    TopUpLink: '',
    ChatLink: '',
    ChatLinks: '',
    BuiltinChatEnabled: 'true',
    QuotaPerUnit: 0,
    AutomaticDisableChannelEnabled: 'false',
    AutomaticEnableChannelEnabled: 'false',
    AutomaticDisableChannelNotifyEnabled: 'true',
    ChannelDisableThreshold: 0,
    LogConsumeEnabled: 'true',
    LogAutoDeleteEnabled: 'false',
    LogAutoDeleteDays: 30,
    DisplayInCurrencyEnabled: 'false',
    DisplayTokenStatEnabled: 'false',
    ApproximateTokenEnabled: 'false',
    EmptyResponseBillingEnabled: 'true',
    UnifiedRequestResponseModelEnabled: 'false',
    RetryTimes: 0,
    RetryTimeOut: 0,
    RetryCooldownSeconds: 0,
    RetryCooldownPerStatus: '',
    ChannelFailErrorWrapEnabled: 'true',
    ChannelFailErrorMessage: '',
    MjNotifyEnabled: 'false',
    ChatImageRequestProxy: '',
    PaymentUSDRate: 0,
    PaymentMinAmount: 1,
    RechargeDiscount: '',
    CFWorkerImageUrl: '',
    CFWorkerImageKey: '',
    ClaudeAPIEnabled: 'true',
    GeminiAPIEnabled: 'true',
    DisableChannelKeywords: '',
    EnableSafe: 'false',
    SafeToolName: '',
    SafeKeyWords: '',
    safeTools: [],
    ClaudeBudgetTokensPercentage: 0,
    ClaudeDefaultMaxTokens: '',
    GeminiOpenThink: ''
  });
  const [originInputs, setOriginInputs] = useState({});
  // cooldownRules: rows backing the RetryCooldownPerStatus JSON config, e.g.
  // [{ rid: 1, code: '503', secs: '120' }, ...]. rid 是稳定的本地行 id，仅用于 React key，
  // 不参与序列化；防止删除中间行时 DOM 复用错位导致输入框焦点/imperative state 残留。
  const [cooldownRules, setCooldownRules] = useState([]);
  const cooldownRidRef = useRef(0);
  const nextRid = () => ++cooldownRidRef.current;
  // cooldownDialogOpen 控制"按状态码自定义"弹窗的显隐——保留原行 6 列 UI 不变，
  // 在 RetryCooldownSeconds 输入框右侧加 IconButton 触发，避免主表单被新 control 撑大。
  const [cooldownDialogOpen, setCooldownDialogOpen] = useState(false);
  let [loading, setLoading] = useState(false);
  let [dataLoaded, setDataLoaded] = useState(false); // 添加数据加载状态
  let [historyTimestamp, setHistoryTimestamp] = useState(now.getTime() / 1000 - 30 * 24 * 3600); // a month ago new Date().getTime() / 1000 + 3600
  let [invoiceMonth, setInvoiceMonth] = useState(now.getTime()); // a month ago new Date().getTime() / 1000 + 3600
  const loadStatus = useContext(LoadStatusContext);
  const [safeToolsLoading, setSafeToolsLoading] = useState(true);

  const getOptions = async () => {
    try {
      const res = await API.get('/api/option/');
      const { success, message, data } = res.data;
      if (success) {
        let newInputs = { ...inputs }; // 保留现有的 inputs 内容，包括 safeTools
        data.forEach((item) => {
          if (item.key === 'RechargeDiscount') {
            item.value = JSON.stringify(JSON.parse(item.value), null, 2);
          }
          if (item.key === 'RetryCooldownPerStatus') {
            if (typeof item.value === 'string' && item.value.trim()) {
              try {
                const parsed = JSON.parse(item.value);
                setCooldownRules(
                  Object.entries(parsed).map(([code, secs]) => ({
                    rid: nextRid(),
                    code: String(code),
                    secs: String(secs)
                  }))
                );
              } catch (e) {
                console.error('解析 RetryCooldownPerStatus 失败:', e);
              }
            } else {
              // 后端值被清空，UI 也要同步刷成空，避免修改→保存清空→不刷新还看到旧规则
              setCooldownRules([]);
            }
          }
          if (item.key === 'SafeKeyWords' && typeof item.value === 'string' && item.value.startsWith('[')) {
            try {
              item.value = JSON.parse(item.value);
            } catch (e) {
              console.error('解析SafeKeyWords失败:', e);
            }
          }
          // 处理布尔值配置项，统一转换为字符串
          if (item.key.endsWith('Enabled') && typeof item.value === 'boolean') {
            item.value = item.value.toString();
          }
          newInputs[item.key] = item.value;
        });
        // 确保不会覆盖 safeTools
        setInputs((prev) => ({ ...newInputs, safeTools: prev.safeTools }));
        setOriginInputs(newInputs);
      } else {
        showError(message);
      }
    } catch (error) {}
  };

  const getSafeTools = async () => {
    setSafeToolsLoading(true);
    try {
      const res = await API.get('/api/option/safe_tools');
      const { success, message, data } = res.data;
      if (success) {
        setInputs((prev) => {
          const newInputs = {
            ...prev,
            safeTools: data
          };
          return newInputs;
        });
      } else {
        showError(message);
      }
    } catch (error) {
      console.error('获取安全工具列表失败:', error);
      showError('获取安全工具列表失败');
    } finally {
      setSafeToolsLoading(false);
    }
  };

  useEffect(() => {
    const initData = async () => {
      await getSafeTools();
      await getOptions();
      setDataLoaded(true); // 数据加载完成后设置状态
    };
    initData();
  }, []);

  const updateOption = async (key, value) => {
    setLoading(true);
    if (key.endsWith('Enabled')) {
      value = inputs[key] === 'true' ? 'false' : 'true';
    }

    try {
      const res = await API.put('/api/option/', {
        key,
        value
      });
      const { success, message } = res.data;
      if (success) {
        setInputs((inputs) => ({ ...inputs, [key]: value }));
        getOptions();
        await loadStatus();
      } else {
        // 不在这里显示错误，而是抛出异常让调用者处理
        throw new Error(message);
      }
    } catch (error) {
      setLoading(false);
      throw error;
    }

    setLoading(false);
  };

  const handleInputChange = async (event) => {
    let { name, value } = event.target;

    if (name.endsWith('Enabled')) {
      try {
        await updateOption(name, value);
        showSuccess('设置成功！');
      } catch (error) {
        showError(error.message || '设置失败');
      }
    } else {
      // origin 是 string、QuotaInput 回传 number；归一避免 submit diff 误判触发冗余 PUT
      // 目前仅 QuotaInput 会回传 number，未来接 Slider 等组件需重新审视
      const normalized = typeof value === 'number' ? String(value) : value;
      setInputs((inputs) => ({ ...inputs, [name]: normalized }));
    }
  };

  const handleTextFieldChange = (event) => {
    const { name, value } = event.target;
    setInputs((prev) => ({
      ...prev,
      [name]: value
    }));
  };

  const submitConfig = async (group) => {
    setLoading(true);
    try {
      switch (group) {
        case 'monitor':
          if (originInputs['ChannelDisableThreshold'] !== inputs.ChannelDisableThreshold) {
            await updateOption('ChannelDisableThreshold', inputs.ChannelDisableThreshold);
          }
          if (originInputs['QuotaRemindThreshold'] !== inputs.QuotaRemindThreshold) {
            await updateOption('QuotaRemindThreshold', inputs.QuotaRemindThreshold);
          }
          break;
        case 'chatlinks':
          if (originInputs['ChatLinks'] !== inputs.ChatLinks) {
            if (!verifyJSON(inputs.ChatLinks)) {
              showError('links不是合法的 JSON 字符串');
              return;
            }
            await updateOption('ChatLinks', inputs.ChatLinks);
          }
          if (originInputs['BuiltinChatEnabled'] !== inputs.BuiltinChatEnabled) {
            await updateOption('BuiltinChatEnabled', inputs.BuiltinChatEnabled);
          }
          break;
        case 'quota': {
          // QuotaInput 允许显示为空（清空保留 / 加载前置态）；仅拦截"本次变更且为空"的字段，避免别处空状态阻断当前编辑
          const quotaKeys = ['QuotaForNewUser', 'PreConsumedQuota', 'QuotaForInviter', 'QuotaForInvitee', 'InviterRewardValue'];
          const emptyKey = quotaKeys.find(
            (k) => originInputs[k] !== inputs[k] && (inputs[k] === '' || inputs[k] == null)
          );
          if (emptyKey) {
            showError('额度配置不能为空，请检查');
            return;
          }
          // 验证充值返利值的范围
          if (originInputs['InviterRewardValue'] !== inputs.InviterRewardValue) {
            const rewardValue = parseInt(inputs.InviterRewardValue);
            if (isNaN(rewardValue)) {
              showError('充值返利值必须是有效的数字');
              return;
            }

            if (inputs.InviterRewardType === 'percentage') {
              // 百分比类型：值应在0-100之间
              if (rewardValue < 0 || rewardValue > 100) {
                showError('当充值返利类型为百分比时，返利值应在0-100之间');
                return;
              }
            } else {
              // 固定类型：值应>=0
              if (rewardValue < 0) {
                showError('当充值返利类型为固定时，返利值应大于等于0');
                return;
              }
            }
          }

          if (originInputs['QuotaForNewUser'] !== inputs.QuotaForNewUser) {
            await updateOption('QuotaForNewUser', inputs.QuotaForNewUser);
          }
          if (originInputs['QuotaForInvitee'] !== inputs.QuotaForInvitee) {
            await updateOption('QuotaForInvitee', inputs.QuotaForInvitee);
          }
          if (originInputs['QuotaForInviter'] !== inputs.QuotaForInviter) {
            await updateOption('QuotaForInviter', inputs.QuotaForInviter);
          }
          if (originInputs['InviterRewardType'] !== inputs.InviterRewardType) {
            await updateOption('InviterRewardType', inputs.InviterRewardType);
          }
          if (originInputs['InviterRewardValue'] !== inputs.InviterRewardValue) {
            await updateOption('InviterRewardValue', inputs.InviterRewardValue);
          }
          if (originInputs['PreConsumedQuota'] !== inputs.PreConsumedQuota) {
            await updateOption('PreConsumedQuota', inputs.PreConsumedQuota);
          }
          break;
        }
        case 'general': {
          // 所有同步校验先做完，确保任何一项失败都不会让前面的 updateOption 已经落库。
          // case 'general' 整体用 block 包起来满足 ESLint no-case-declarations，便于声明 const。
          // handleInputChange 已把 number 归一为 string，显式 Number() 避免未来 refactor 加 typeof === 'number' 校验时误判
          if (
            Number(inputs.QuotaPerUnit) < 0 ||
            Number(inputs.RetryTimes) < 0 ||
            Number(inputs.RetryCooldownSeconds) < 0 ||
            Number(inputs.RetryTimeOut) < 0
          ) {
            showError('单位额度、重试次数、冷却时间、重试超时时间不能为负数');
            return;
          }
          const cleanRules = cooldownRules
            .map((r) => ({ code: String(r.code ?? '').trim(), secs: String(r.secs ?? '').trim() }))
            .filter((r) => r.code !== '' || r.secs !== '');
          const seenCodes = new Set();
          for (const r of cleanRules) {
            const code = Number(r.code);
            const secs = Number(r.secs);
            if (!Number.isInteger(code) || code < 100 || code > 599) {
              showError(t('setting_index.operationSettings.generalSettings.retryCooldownPerStatus.invalidStatusCode', { code: r.code }));
              return;
            }
            if (!Number.isInteger(secs) || secs < 0) {
              showError(t('setting_index.operationSettings.generalSettings.retryCooldownPerStatus.invalidSeconds', { code: r.code }));
              return;
            }
            if (seenCodes.has(code)) {
              showError(t('setting_index.operationSettings.generalSettings.retryCooldownPerStatus.duplicateStatusCode', { code }));
              return;
            }
            seenCodes.add(code);
          }
          const cooldownObj = {};
          for (const r of cleanRules) {
            cooldownObj[Number(r.code)] = Number(r.secs);
          }
          const newCooldownJson = Object.keys(cooldownObj).length ? JSON.stringify(cooldownObj) : '';

          // 校验通过后再开始 updateOption 序列
          if (originInputs['TopUpLink'] !== inputs.TopUpLink) {
            await updateOption('TopUpLink', inputs.TopUpLink);
          }
          if (originInputs['ChatLink'] !== inputs.ChatLink) {
            await updateOption('ChatLink', inputs.ChatLink);
          }
          if (originInputs['QuotaPerUnit'] !== inputs.QuotaPerUnit) {
            await updateOption('QuotaPerUnit', inputs.QuotaPerUnit);
          }
          if (originInputs['RetryTimes'] !== inputs.RetryTimes) {
            await updateOption('RetryTimes', inputs.RetryTimes);
          }
          if (originInputs['RetryCooldownSeconds'] !== inputs.RetryCooldownSeconds) {
            await updateOption('RetryCooldownSeconds', inputs.RetryCooldownSeconds);
          }
          if ((originInputs['RetryCooldownPerStatus'] || '') !== newCooldownJson) {
            await updateOption('RetryCooldownPerStatus', newCooldownJson);
          }
          if (originInputs['RetryTimeOut'] !== inputs.RetryTimeOut) {
            await updateOption('RetryTimeOut', inputs.RetryTimeOut);
          }
          if (originInputs['ChannelFailErrorWrapEnabled'] !== inputs.ChannelFailErrorWrapEnabled) {
            await updateOption('ChannelFailErrorWrapEnabled', inputs.ChannelFailErrorWrapEnabled);
          }
          if (originInputs['ChannelFailErrorMessage'] !== inputs.ChannelFailErrorMessage) {
            await updateOption('ChannelFailErrorMessage', inputs.ChannelFailErrorMessage);
          }
          if (originInputs['EmptyResponseBillingEnabled'] !== inputs.EmptyResponseBillingEnabled) {
            await updateOption('EmptyResponseBillingEnabled', inputs.EmptyResponseBillingEnabled);
          }
          if (originInputs['UnifiedRequestResponseModelEnabled'] !== inputs.UnifiedRequestResponseModelEnabled) {
            await updateOption('UnifiedRequestResponseModelEnabled', inputs.UnifiedRequestResponseModelEnabled);
          }
          break;
        }
        case 'other':
          if (originInputs['ChatImageRequestProxy'] !== inputs.ChatImageRequestProxy) {
            await updateOption('ChatImageRequestProxy', inputs.ChatImageRequestProxy);
          }

          if (originInputs['CFWorkerImageUrl'] !== inputs.CFWorkerImageUrl) {
            await updateOption('CFWorkerImageUrl', inputs.CFWorkerImageUrl);
          }

          if (originInputs['CFWorkerImageKey'] !== inputs.CFWorkerImageKey) {
            await updateOption('CFWorkerImageKey', inputs.CFWorkerImageKey);
          }

          break;
        case 'payment':
          if (originInputs['PaymentUSDRate'] !== inputs.PaymentUSDRate) {
            await updateOption('PaymentUSDRate', inputs.PaymentUSDRate);
          }
          if (originInputs['PaymentMinAmount'] !== inputs.PaymentMinAmount) {
            await updateOption('PaymentMinAmount', inputs.PaymentMinAmount);
          }
          if (originInputs['RechargeDiscount'] !== inputs.RechargeDiscount) {
            try {
              if (!verifyJSON(inputs.RechargeDiscount)) {
                showError('固定金额充值折扣不是合法的 JSON 字符串');
                return;
              }
              await updateOption('RechargeDiscount', inputs.RechargeDiscount);
            } catch (error) {
              showError('固定金额充值折扣处理失败: ' + error.message);
              return;
            }
          }
          break;
        case 'DisableChannelKeywords':
          if (originInputs.DisableChannelKeywords !== inputs.DisableChannelKeywords) {
            // DisableChannelKeywords 已经是字符串格式，无需解析
            await updateOption('DisableChannelKeywords', inputs.DisableChannelKeywords);
          }
          break;
        case 'safety':
          try {
            if (originInputs.EnableSafe !== inputs.EnableSafe) {
              await updateOption('EnableSafe', inputs.EnableSafe);
            }
            if (originInputs.SafeToolName !== inputs.SafeToolName) {
              await updateOption('SafeToolName', inputs.SafeToolName);
            }
            if (originInputs.SafeKeyWords !== inputs.SafeKeyWords) {
              await updateOption('SafeKeyWords', inputs.SafeKeyWords);
            }
          } catch (error) {
            console.error('安全设置提交错误:', error);
            showError(`安全设置保存失败: ${error.message || '未知错误'}`);
            setLoading(false);
            return;
          }
          break;
        case 'claude':
          if (originInputs.ClaudeBudgetTokensPercentage !== inputs.ClaudeBudgetTokensPercentage) {
            await updateOption('ClaudeBudgetTokensPercentage', inputs.ClaudeBudgetTokensPercentage);
          }
          if (originInputs.ClaudeDefaultMaxTokens !== inputs.ClaudeDefaultMaxTokens) {
            if (!verifyJSON(inputs.ClaudeDefaultMaxTokens)) {
              showError('默认MaxToken数量不是合法的 JSON 字符串');
              return;
            }
            await updateOption('ClaudeDefaultMaxTokens', inputs.ClaudeDefaultMaxTokens);
          }
          break;

        case 'gemini':
          if (originInputs.GeminiOpenThink !== inputs.GeminiOpenThink) {
            if (!verifyJSON(inputs.GeminiOpenThink)) {
              showError('GeminiOpenThink 不是合法的 JSON 字符串');
              return;
            }
            await updateOption('GeminiOpenThink', inputs.GeminiOpenThink);
          }
          break;
      }

      await getOptions();
      await getSafeTools();
      showSuccess('保存成功！');
    } catch (error) {
      showError('保存失败：' + (error.message || '未知错误'));
    } finally {
      setLoading(false);
    }
  };

  const deleteHistoryLogs = async () => {
    try {
      const res = await API.delete(`/api/log/?target_timestamp=${Math.floor(historyTimestamp)}`);
      const { success, message, data } = res.data;
      if (success) {
        showSuccess(`${data} 条日志已清理！`);
        return;
      }
      showError('日志清理失败：' + message);
    } catch (error) {}
  };

  const genInvoiceMonth = async () => {
    try {
      const time = dayjs(invoiceMonth).format('YYYY-MM-DD');
      const res = await API.post(`/api/option/invoice/gen/${time}`);
      const { success, message } = res.data;
      if (success) {
        showSuccess(`账单生成成功！`);
        return;
      }
      showError('账单生成失败：' + message);
    } catch (error) {}
  };
  const updateInvoiceMonth = async () => {
    try {
      const time = dayjs(invoiceMonth).format('YYYY-MM-DD');
      const res = await API.post(`/api/option/invoice/update/${time}`);
      const { success, message } = res.data;
      if (success) {
        showSuccess(`账单更新成功！`);
        return;
      }
      showError('账单更新失败：' + message);
    } catch (error) {}
  };

  return (
    <Stack spacing={2}>
      <SubCard title={t('setting_index.operationSettings.generalSettings.title')}>
        <Grid container spacing={{ xs: 3, sm: 2, md: 4 }}>
          {/* 链接与计费 */}
          <Grid item xs={12}>
            <Typography variant="subtitle1" color="textSecondary" sx={{ mb: 1 }}>
              {t('setting_index.operationSettings.generalSettings.subGroups.linkAndBilling')}
            </Typography>
          </Grid>
          <Grid item xs={12} sm={6} md={4}>
            <FormControl fullWidth>
              <InputLabel htmlFor="TopUpLink">{t('setting_index.operationSettings.generalSettings.topUpLink.label')}</InputLabel>
              <OutlinedInput
                id="TopUpLink"
                name="TopUpLink"
                value={inputs.TopUpLink}
                onChange={handleInputChange}
                label={t('setting_index.operationSettings.generalSettings.topUpLink.label')}
                placeholder={t('setting_index.operationSettings.generalSettings.topUpLink.placeholder')}
                disabled={loading}
              />
            </FormControl>
          </Grid>
          <Grid item xs={12} sm={6} md={4}>
            <FormControl fullWidth>
              <InputLabel htmlFor="ChatLink">{t('setting_index.operationSettings.generalSettings.chatLink.label')}</InputLabel>
              <OutlinedInput
                id="ChatLink"
                name="ChatLink"
                value={inputs.ChatLink}
                onChange={handleInputChange}
                label={t('setting_index.operationSettings.generalSettings.chatLink.label')}
                placeholder={t('setting_index.operationSettings.generalSettings.chatLink.placeholder')}
                disabled={loading}
              />
            </FormControl>
          </Grid>
          <Grid item xs={12} sm={6} md={4}>
            <FormControl fullWidth>
              <InputLabel htmlFor="QuotaPerUnit">{t('setting_index.operationSettings.generalSettings.quotaPerUnit.label')}</InputLabel>
              <OutlinedInput
                id="QuotaPerUnit"
                name="QuotaPerUnit"
                value={inputs.QuotaPerUnit}
                onChange={handleInputChange}
                label={t('setting_index.operationSettings.generalSettings.quotaPerUnit.label')}
                placeholder={t('setting_index.operationSettings.generalSettings.quotaPerUnit.placeholder')}
                disabled={loading}
              />
            </FormControl>
          </Grid>

          {/* 重试策略 */}
          <Grid item xs={12}>
            <Divider sx={{ my: 1 }} />
            <Typography variant="subtitle1" color="textSecondary" sx={{ mb: 1, mt: 2 }}>
              {t('setting_index.operationSettings.generalSettings.subGroups.retry')}
            </Typography>
          </Grid>
          <Grid item xs={12} sm={6} md={4}>
            <FormControl fullWidth>
              <InputLabel htmlFor="RetryTimes">{t('setting_index.operationSettings.generalSettings.retryTimes.label')}</InputLabel>
              <OutlinedInput
                id="RetryTimes"
                name="RetryTimes"
                value={inputs.RetryTimes}
                onChange={handleInputChange}
                label={t('setting_index.operationSettings.generalSettings.retryTimes.label')}
                placeholder={t('setting_index.operationSettings.generalSettings.retryTimes.placeholder')}
                disabled={loading}
              />
            </FormControl>
          </Grid>
          <Grid item xs={12} sm={6} md={4}>
            <FormControl fullWidth>
              <InputLabel htmlFor="RetryCooldownSeconds">
                {t('setting_index.operationSettings.generalSettings.retryCooldownSeconds.label')}
              </InputLabel>
              <OutlinedInput
                id="RetryCooldownSeconds"
                name="RetryCooldownSeconds"
                value={inputs.RetryCooldownSeconds}
                onChange={handleInputChange}
                label={t('setting_index.operationSettings.generalSettings.retryCooldownSeconds.label')}
                placeholder={t('setting_index.operationSettings.generalSettings.retryCooldownSeconds.placeholder')}
                disabled={loading}
                endAdornment={
                  <InputAdornment position="end">
                    <Tooltip title={t('setting_index.operationSettings.generalSettings.retryCooldownPerStatus.openTooltip')}>
                      <IconButton edge="end" size="small" onClick={() => setCooldownDialogOpen(true)} disabled={loading}>
                        {/* Badge 暴露当前 PerStatus 规则条数，避免管理员不知道弹窗里是否已配置。
                            默认尺寸是给文字按钮用的，配 small icon 会盖住一半，所以缩到 14px / fontSize 9。 */}
                        <Badge
                          badgeContent={cooldownRules.filter((r) => String(r.code ?? '').trim() !== '').length}
                          color="primary"
                          overlap="circular"
                          sx={{
                            '& .MuiBadge-badge': {
                              height: 14,
                              minWidth: 14,
                              fontSize: 9,
                              padding: '0 4px'
                            }
                          }}
                        >
                          <TuneIcon fontSize="small" />
                        </Badge>
                      </IconButton>
                    </Tooltip>
                  </InputAdornment>
                }
              />
            </FormControl>
          </Grid>
          <Grid item xs={12} sm={6} md={4}>
            <FormControl fullWidth>
              <InputLabel htmlFor="RetryTimeOut">{t('setting_index.operationSettings.generalSettings.retryTimeOut.label')}</InputLabel>
              <OutlinedInput
                id="RetryTimeOut"
                name="RetryTimeOut"
                value={inputs.RetryTimeOut}
                onChange={handleInputChange}
                label={t('setting_index.operationSettings.generalSettings.retryTimeOut.label')}
                placeholder={t('setting_index.operationSettings.generalSettings.retryTimeOut.placeholder')}
                disabled={loading}
              />
            </FormControl>
          </Grid>

          {/* 上游错误处理 */}
          <Grid item xs={12}>
            <Divider sx={{ my: 1 }} />
            <Typography variant="subtitle1" color="textSecondary" sx={{ mb: 1, mt: 2 }}>
              {t('setting_index.operationSettings.generalSettings.subGroups.upstreamError')}
            </Typography>
          </Grid>
          <Grid item xs={12} sm={12} md={4}>
            <Tooltip
              title={t('setting_index.operationSettings.generalSettings.channelFailErrorWrapEnabledTooltip')}
              placement="top"
              enterDelay={300}
              arrow
            >
              <FormControlLabel
                sx={{ marginLeft: '0px' }}
                label={t('setting_index.operationSettings.generalSettings.channelFailErrorWrapEnabled')}
                control={
                  <Checkbox
                    checked={dataLoaded ? inputs.ChannelFailErrorWrapEnabled === 'true' : false}
                    onChange={handleInputChange}
                    name="ChannelFailErrorWrapEnabled"
                    disabled={!dataLoaded || loading}
                  />
                }
              />
            </Tooltip>
          </Grid>
          <Grid item xs={12} md={8}>
            <FormControl fullWidth>
              <InputLabel htmlFor="ChannelFailErrorMessage">
                {t('setting_index.operationSettings.generalSettings.channelFailErrorMessage.label')}
              </InputLabel>
              <OutlinedInput
                id="ChannelFailErrorMessage"
                name="ChannelFailErrorMessage"
                value={inputs.ChannelFailErrorMessage}
                onChange={handleInputChange}
                label={t('setting_index.operationSettings.generalSettings.channelFailErrorMessage.label')}
                placeholder={t('setting_index.operationSettings.generalSettings.channelFailErrorMessage.placeholder')}
                disabled={!dataLoaded || loading || inputs.ChannelFailErrorWrapEnabled !== 'true'}
              />
            </FormControl>
          </Grid>

          {/* 显示与计费选项 */}
          <Grid item xs={12}>
            <Divider sx={{ my: 1 }} />
            <Typography variant="subtitle1" color="textSecondary" sx={{ mb: 1, mt: 2 }}>
              {t('setting_index.operationSettings.generalSettings.subGroups.displayOptions')}
            </Typography>
          </Grid>
          <Grid item xs={12} sm={6} md={3}>
            <FormControlLabel
              sx={{ marginLeft: '0px' }}
              label={t('setting_index.operationSettings.generalSettings.displayInCurrency')}
              control={
                <Checkbox
                  checked={dataLoaded ? inputs.DisplayInCurrencyEnabled === 'true' : false}
                  onChange={handleInputChange}
                  name="DisplayInCurrencyEnabled"
                  disabled={!dataLoaded || loading}
                />
              }
            />
          </Grid>
          <Grid item xs={12} sm={6} md={3}>
            <FormControlLabel
              sx={{ marginLeft: '0px' }}
              label={t('setting_index.operationSettings.generalSettings.displayTokenStat')}
              control={
                <Checkbox
                  checked={dataLoaded ? inputs.DisplayTokenStatEnabled === 'true' : false}
                  onChange={handleInputChange}
                  name="DisplayTokenStatEnabled"
                  disabled={!dataLoaded || loading}
                />
              }
            />
          </Grid>
          <Grid item xs={12} sm={6} md={3}>
            <FormControlLabel
              sx={{ marginLeft: '0px' }}
              label={t('setting_index.operationSettings.generalSettings.approximateToken')}
              control={
                <Checkbox
                  checked={dataLoaded ? inputs.ApproximateTokenEnabled === 'true' : false}
                  onChange={handleInputChange}
                  name="ApproximateTokenEnabled"
                  disabled={!dataLoaded || loading}
                />
              }
            />
          </Grid>
          <Grid item xs={12} sm={6} md={3}>
            <FormControlLabel
              sx={{ marginLeft: '0px' }}
              label={t('setting_index.operationSettings.generalSettings.emptyResponseBilling')}
              control={
                <Checkbox
                  checked={dataLoaded ? inputs.EmptyResponseBillingEnabled === 'true' : false}
                  onChange={handleInputChange}
                  name="EmptyResponseBillingEnabled"
                  disabled={!dataLoaded || loading}
                />
              }
            />
          </Grid>
          <Grid item xs={12} sm={6} md={3}>
            <Tooltip
              title={t('setting_index.operationSettings.generalSettings.unifiedRequestResponseModelTooltip')}
              placement="top"
              enterDelay={300}
              arrow
            >
              <FormControlLabel
                sx={{ marginLeft: '0px' }}
                label={t('setting_index.operationSettings.generalSettings.unifiedRequestResponseModel')}
                control={
                  <Checkbox
                    checked={dataLoaded ? inputs.UnifiedRequestResponseModelEnabled === 'true' : false}
                    onChange={handleInputChange}
                    name="UnifiedRequestResponseModelEnabled"
                    disabled={!dataLoaded || loading}
                  />
                }
              />
            </Tooltip>
          </Grid>
          <Grid item xs={12} sm={6} md={3}>
            <Tooltip
              title={t('setting_index.operationSettings.generalSettings.modelNameCaseInsensitiveTooltip')}
              placement="top"
              enterDelay={300}
              arrow
            >
              <FormControlLabel
                sx={{ marginLeft: '0px' }}
                label={t('setting_index.operationSettings.generalSettings.modelNameCaseInsensitive')}
                control={
                  <Checkbox
                    checked={dataLoaded ? inputs.ModelNameCaseInsensitiveEnabled === 'true' : false}
                    onChange={handleInputChange}
                    name="ModelNameCaseInsensitiveEnabled"
                    disabled={!dataLoaded || loading}
                  />
                }
              />
            </Tooltip>
          </Grid>

          <Grid item xs={12}>
            <Button
              variant="contained"
              onClick={() => {
                submitConfig('general').then();
              }}
            >
              {t('setting_index.operationSettings.generalSettings.saveButton')}
            </Button>
          </Grid>
        </Grid>
      </SubCard>
      <SubCard title={t('setting_index.operationSettings.otherSettings.title')}>
        <Grid container spacing={{ xs: 3, sm: 2, md: 4 }}>
          <Grid item xs={12} sm={6} md={3}>
            <FormControlLabel
              sx={{ marginLeft: '0px' }}
              label={t('setting_index.operationSettings.otherSettings.mjNotify')}
              control={
                <Checkbox
                  checked={dataLoaded ? inputs.MjNotifyEnabled === 'true' : false}
                  onChange={handleInputChange}
                  name="MjNotifyEnabled"
                  disabled={!dataLoaded || loading}
                />
              }
            />
          </Grid>
          <Grid item xs={12} sm={6} md={3}>
            <FormControlLabel
              sx={{ marginLeft: '0px' }}
              label={t('setting_index.operationSettings.otherSettings.claudeAPIEnabled')}
              control={
                <Checkbox
                  checked={dataLoaded ? inputs.ClaudeAPIEnabled === 'true' : false}
                  onChange={handleInputChange}
                  name="ClaudeAPIEnabled"
                  disabled={!dataLoaded || loading}
                />
              }
            />
          </Grid>
          <Grid item xs={12} sm={6} md={3}>
            <FormControlLabel
              sx={{ marginLeft: '0px' }}
              label={t('setting_index.operationSettings.otherSettings.geminiAPIEnabled')}
              control={
                <Checkbox
                  checked={dataLoaded ? inputs.GeminiAPIEnabled === 'true' : false}
                  onChange={handleInputChange}
                  name="GeminiAPIEnabled"
                  disabled={!dataLoaded || loading}
                />
              }
            />
          </Grid>

          <Grid item xs={12}>
            <Alert severity="info">{t('setting_index.operationSettings.otherSettings.alert')}</Alert>
          </Grid>
          <Grid item xs={12} sm={6} md={4}>
            <FormControl fullWidth>
              <InputLabel htmlFor="ChatImageRequestProxy">
                {t('setting_index.operationSettings.otherSettings.chatImageRequestProxy.label')}
              </InputLabel>
              <OutlinedInput
                id="ChatImageRequestProxy"
                name="ChatImageRequestProxy"
                value={inputs.ChatImageRequestProxy}
                onChange={handleInputChange}
                label={t('setting_index.operationSettings.otherSettings.chatImageRequestProxy.label')}
                placeholder={t('setting_index.operationSettings.otherSettings.chatImageRequestProxy.placeholder')}
                disabled={loading}
              />
            </FormControl>
          </Grid>

          <Grid item xs={12}>
            <Alert severity="info">{t('setting_index.operationSettings.otherSettings.CFWorkerImageUrl.alert')}</Alert>
          </Grid>
          <Grid item xs={12} sm={6} md={6}>
            <FormControl fullWidth>
              <InputLabel htmlFor="CFWorkerImageUrl">
                {t('setting_index.operationSettings.otherSettings.CFWorkerImageUrl.label')}
              </InputLabel>
              <OutlinedInput
                id="CFWorkerImageUrl"
                name="CFWorkerImageUrl"
                value={inputs.CFWorkerImageUrl}
                onChange={handleInputChange}
                label={t('setting_index.operationSettings.otherSettings.CFWorkerImageUrl.label')}
                placeholder={t('setting_index.operationSettings.otherSettings.CFWorkerImageUrl.label')}
                disabled={loading}
              />
            </FormControl>
          </Grid>
          <Grid item xs={12} sm={6} md={6}>
            <FormControl fullWidth>
              <InputLabel htmlFor="CFWorkerImageKey">{t('setting_index.operationSettings.otherSettings.CFWorkerImageUrl.key')}</InputLabel>
              <OutlinedInput
                id="CFWorkerImageKey"
                name="CFWorkerImageKey"
                value={inputs.CFWorkerImageKey}
                onChange={handleInputChange}
                label={t('setting_index.operationSettings.otherSettings.CFWorkerImageUrl.key')}
                placeholder={t('setting_index.operationSettings.otherSettings.CFWorkerImageUrl.key')}
                disabled={loading}
              />
            </FormControl>
          </Grid>

          <Grid item xs={12}>
            <Button
              variant="contained"
              onClick={() => {
                submitConfig('other').then();
              }}
            >
              {t('setting_index.operationSettings.otherSettings.saveButton')}
            </Button>
          </Grid>
        </Grid>
      </SubCard>
      <SubCard title={t('setting_index.operationSettings.logSettings.title')}>
        <Grid container spacing={{ xs: 3, sm: 2, md: 4 }} alignItems="center">
          <Grid item xs={12} sm={6} md={3}>
            <FormControlLabel
              sx={{ marginLeft: '0px' }}
              label={t('setting_index.operationSettings.logSettings.logConsume')}
              control={
                <Checkbox
                  checked={dataLoaded ? inputs.LogConsumeEnabled === 'true' : false}
                  onChange={handleInputChange}
                  name="LogConsumeEnabled"
                  disabled={!dataLoaded || loading}
                />
              }
            />
          </Grid>
          <Grid item xs={12} sm={6} md={3}>
            <FormControlLabel
              sx={{ marginLeft: '0px' }}
              label={t('setting_index.operationSettings.logSettings.autoDelete')}
              control={
                <Checkbox
                  checked={dataLoaded ? inputs.LogAutoDeleteEnabled === 'true' : false}
                  onChange={handleInputChange}
                  name="LogAutoDeleteEnabled"
                  disabled={!dataLoaded || loading}
                />
              }
            />
          </Grid>
          <Grid item xs={12} sm={6} md={4}>
            <FormControl fullWidth>
              <TextField
                type="number"
                label={t('setting_index.operationSettings.logSettings.autoDeleteDays')}
                name="LogAutoDeleteDays"
                value={inputs.LogAutoDeleteDays}
                onChange={handleTextFieldChange}
                onBlur={async () => {
                  if (originInputs['LogAutoDeleteDays'] !== inputs.LogAutoDeleteDays) {
                    try {
                      await updateOption('LogAutoDeleteDays', inputs.LogAutoDeleteDays);
                      showSuccess('设置成功！');
                    } catch (error) {
                      showError(error.message || '设置失败');
                    }
                  }
                }}
                disabled={!dataLoaded || loading}
              />
            </FormControl>
          </Grid>

          <Grid item xs={12} sm={6} md={4}>
            <FormControl fullWidth>
              <LocalizationProvider dateAdapter={AdapterDayjs} adapterLocale={'zh-cn'}>
                <DateTimePicker
                  label={t('setting_index.operationSettings.logSettings.logCleanupTime.label')}
                  placeholder={t('setting_index.operationSettings.logSettings.logCleanupTime.placeholder')}
                  ampm={false}
                  name="historyTimestamp"
                  value={historyTimestamp === null ? null : dayjs.unix(historyTimestamp)}
                  disabled={loading}
                  onChange={(newValue) => {
                    setHistoryTimestamp(newValue === null ? null : newValue.unix());
                  }}
                  slotProps={{
                    textField: { fullWidth: true },
                    actionBar: {
                      actions: ['today', 'clear', 'accept']
                    }
                  }}
                />
              </LocalizationProvider>
            </FormControl>
          </Grid>

          <Grid item xs={12}>
            <Button
              variant="contained"
              onClick={() => {
                deleteHistoryLogs().then();
              }}
            >
              {t('setting_index.operationSettings.logSettings.clearLogs')}
            </Button>
          </Grid>
        </Grid>
      </SubCard>

      {siteInfo.UserInvoiceMonth && (
        <SubCard title={t('setting_index.operationSettings.invoice.title')}>
          <Stack direction="column" justifyContent="flex-start" alignItems="flex-start" spacing={2}>
            <FormControl>
              <LocalizationProvider dateAdapter={AdapterDayjs} adapterLocale={'zh-cn'}>
                <DatePicker
                  label={t('setting_index.operationSettings.invoice.genTime')}
                  placeholder={t('setting_index.operationSettings.invoice.genTime')}
                  name="invoiceMonth"
                  value={invoiceMonth === null ? null : dayjs(invoiceMonth)}
                  disabled={loading}
                  views={['month', 'year']}
                  format="YYYY-MM"
                  onChange={(newValue) => {
                    // Set to the first day of the selected month
                    if (newValue) {
                      const firstDayOfMonth = newValue.startOf('month');
                      setInvoiceMonth(firstDayOfMonth.valueOf());
                    } else {
                      setInvoiceMonth(null);
                    }
                  }}
                  slotProps={{
                    actionBar: {
                      actions: ['clear', 'accept']
                    }
                  }}
                />
              </LocalizationProvider>
            </FormControl>
            <Stack direction="row" spacing={2}>
              <Button
                variant="contained"
                color="success"
                onClick={() => {
                  if (invoiceMonth) {
                    genInvoiceMonth().then();
                  } else {
                    showError('Please select invoice Month');
                  }
                }}
              >
                {t('setting_index.operationSettings.invoice.genMonthInvoice')}
              </Button>
              <Button
                variant="contained"
                color="warning"
                onClick={() => {
                  if (invoiceMonth) {
                    updateInvoiceMonth().then();
                  } else {
                    showError('Please select invoice Month');
                  }
                }}
              >
                {t('setting_index.operationSettings.invoice.updateMonthInvoice')}
              </Button>
            </Stack>
          </Stack>
        </SubCard>
      )}

      <SubCard title={t('setting_index.operationSettings.monitoringSettings.title')}>
        <Grid container spacing={{ xs: 3, sm: 2, md: 4 }}>
          <Grid item xs={12} sm={6} md={6}>
            <FormControl fullWidth>
              <InputLabel htmlFor="ChannelDisableThreshold">
                {t('setting_index.operationSettings.monitoringSettings.channelDisableThreshold.label')}
              </InputLabel>
              <OutlinedInput
                id="ChannelDisableThreshold"
                name="ChannelDisableThreshold"
                type="number"
                value={inputs.ChannelDisableThreshold}
                onChange={handleInputChange}
                label={t('setting_index.operationSettings.monitoringSettings.channelDisableThreshold.label')}
                placeholder={t('setting_index.operationSettings.monitoringSettings.channelDisableThreshold.placeholder')}
                disabled={loading}
              />
            </FormControl>
          </Grid>
          <Grid item xs={12} sm={6} md={6}>
            <QuotaInput
              id="QuotaRemindThreshold"
              name="QuotaRemindThreshold"
              label={t('setting_index.operationSettings.monitoringSettings.quotaRemindThreshold.label')}
              placeholder={t('setting_index.operationSettings.monitoringSettings.quotaRemindThreshold.placeholder')}
              value={inputs.QuotaRemindThreshold}
              onChange={handleInputChange}
              disabled={loading}
            />
          </Grid>

          <Grid item xs={12} sm={6} md={3}>
            <FormControlLabel
              sx={{ marginLeft: '0px' }}
              label={t('setting_index.operationSettings.monitoringSettings.automaticDisableChannel')}
              control={
                <Checkbox
                  checked={dataLoaded ? inputs.AutomaticDisableChannelEnabled === 'true' : false}
                  onChange={handleInputChange}
                  name="AutomaticDisableChannelEnabled"
                  disabled={!dataLoaded || loading}
                />
              }
            />
          </Grid>
          <Grid item xs={12} sm={6} md={3}>
            <FormControlLabel
              sx={{ marginLeft: '0px' }}
              label={t('setting_index.operationSettings.monitoringSettings.automaticEnableChannel')}
              control={
                <Checkbox
                  checked={dataLoaded ? inputs.AutomaticEnableChannelEnabled === 'true' : false}
                  onChange={handleInputChange}
                  name="AutomaticEnableChannelEnabled"
                  disabled={!dataLoaded || loading}
                />
              }
            />
          </Grid>
          <Grid item xs={12} sm={6} md={3}>
            <FormControlLabel
              sx={{ marginLeft: '0px' }}
              label={t('setting_index.operationSettings.monitoringSettings.automaticDisableChannelNotify')}
              control={
                <Checkbox
                  checked={dataLoaded ? inputs.AutomaticDisableChannelNotifyEnabled === 'true' : false}
                  onChange={handleInputChange}
                  name="AutomaticDisableChannelNotifyEnabled"
                  disabled={!dataLoaded || loading}
                />
              }
            />
          </Grid>
          <Grid item xs={12} sm={6} md={3}>
            <FormControlLabel
              sx={{ marginLeft: '0px' }}
              label={t('setting_index.operationSettings.monitoringSettings.quotaRemindEnabled')}
              control={
                <Checkbox
                  checked={dataLoaded ? inputs.QuotaRemindEnabled === 'true' : false}
                  onChange={handleInputChange}
                  name="QuotaRemindEnabled"
                  disabled={!dataLoaded || loading}
                />
              }
            />
          </Grid>

          <Grid item xs={12}>
            <Button
              variant="contained"
              onClick={() => {
                submitConfig('monitor').then();
              }}
            >
              {t('setting_index.operationSettings.monitoringSettings.saveMonitoringSettings')}
            </Button>
          </Grid>
        </Grid>
      </SubCard>
      <SubCard title={t('setting_index.operationSettings.quotaSettings.title')}>
        <Stack justifyContent="flex-start" alignItems="flex-start" spacing={2}>
          <Grid container spacing={2}>
            <Grid item xs={12} sm={6} md={4}>
              <QuotaInput
                id="QuotaForNewUser"
                name="QuotaForNewUser"
                label={t('setting_index.operationSettings.quotaSettings.quotaForNewUser.label')}
                placeholder={t('setting_index.operationSettings.quotaSettings.quotaForNewUser.placeholder')}
                value={inputs.QuotaForNewUser}
                onChange={handleInputChange}
                disabled={loading}
              />
            </Grid>
            <Grid item xs={12} sm={6} md={4}>
              <QuotaInput
                id="PreConsumedQuota"
                name="PreConsumedQuota"
                label={t('setting_index.operationSettings.quotaSettings.preConsumedQuota.label')}
                placeholder={t('setting_index.operationSettings.quotaSettings.preConsumedQuota.placeholder')}
                value={inputs.PreConsumedQuota}
                onChange={handleInputChange}
                disabled={loading}
              />
            </Grid>
            <Grid item xs={12} sm={6} md={4}>
              <QuotaInput
                id="QuotaForInviter"
                name="QuotaForInviter"
                label={t('setting_index.operationSettings.quotaSettings.quotaForInviter.label')}
                placeholder={t('setting_index.operationSettings.quotaSettings.quotaForInviter.placeholder')}
                value={inputs.QuotaForInviter}
                onChange={handleInputChange}
                disabled={loading}
                inputProps={{ autoComplete: 'new-password' }}
              />
            </Grid>
            <Grid item xs={12} sm={6} md={4}>
              <FormControl fullWidth>
                <InputLabel>{t('setting_index.operationSettings.quotaSettings.rechargeRewardType.label')}</InputLabel>
                <Select
                  value={inputs.InviterRewardType}
                  name="InviterRewardType"
                  onChange={handleInputChange}
                  label={t('setting_index.operationSettings.quotaSettings.rechargeRewardType.label')}
                  disabled={loading}
                >
                  <MenuItem value="fixed">{t('setting_index.operationSettings.quotaSettings.rechargeRewardType.fixed')}</MenuItem>
                  <MenuItem value="percentage">{t('setting_index.operationSettings.quotaSettings.rechargeRewardType.percentage')}</MenuItem>
                </Select>
              </FormControl>
            </Grid>
            <Grid item xs={12} sm={6} md={4}>
              {/* percentage 是 0-100 的百分比，无 currency/token 语义，不走 QuotaInput。未来若 QuotaInput 加 mode="percentage" 应一并收编 */}
              {inputs.InviterRewardType === 'percentage' ? (
                <FormControl fullWidth>
                  <InputLabel htmlFor="InviterRewardValue">
                    {t('setting_index.operationSettings.quotaSettings.rechargeRewardValue.label')} (%)
                  </InputLabel>
                  <OutlinedInput
                    id="InviterRewardValue"
                    name="InviterRewardValue"
                    type="number"
                    value={inputs.InviterRewardValue}
                    onChange={handleInputChange}
                    label={t('setting_index.operationSettings.quotaSettings.rechargeRewardValue.label') + ' (%)'}
                    placeholder={t('setting_index.operationSettings.quotaSettings.rechargeRewardValue.percentagePlaceholder')}
                    inputProps={{ min: 0, max: 100, step: 1 }}
                    disabled={loading}
                    endAdornment={<InputAdornment position="end">%</InputAdornment>}
                  />
                </FormControl>
              ) : (
                <QuotaInput
                  id="InviterRewardValue"
                  name="InviterRewardValue"
                  label={t('setting_index.operationSettings.quotaSettings.rechargeRewardValue.label')}
                  placeholder={t('setting_index.operationSettings.quotaSettings.rechargeRewardValue.fixedPlaceholder')}
                  value={inputs.InviterRewardValue}
                  onChange={handleInputChange}
                  disabled={loading}
                />
              )}
            </Grid>
            <Grid item xs={12} sm={6} md={4}>
              <QuotaInput
                id="QuotaForInvitee"
                name="QuotaForInvitee"
                label={t('setting_index.operationSettings.quotaSettings.quotaForInvitee.label')}
                placeholder={t('setting_index.operationSettings.quotaSettings.quotaForInvitee.placeholder')}
                value={inputs.QuotaForInvitee}
                onChange={handleInputChange}
                disabled={loading}
                inputProps={{ autoComplete: 'new-password' }}
              />
            </Grid>
          </Grid>
          <Button
            variant="contained"
            onClick={() => {
              submitConfig('quota').then();
            }}
          >
            {t('setting_index.operationSettings.quotaSettings.saveQuotaSettings')}
          </Button>
        </Stack>
      </SubCard>
      <SubCard title={t('setting_index.operationSettings.paymentSettings.title')}>
        <Stack justifyContent="flex-start" alignItems="flex-start" spacing={2}>
          <Stack justifyContent="flex-start" alignItems="flex-start" spacing={2}>
            <FormControl fullWidth>
              <Alert severity="info">
                <div dangerouslySetInnerHTML={{ __html: t('setting_index.operationSettings.paymentSettings.alert') }} />
              </Alert>
            </FormControl>
            <Stack direction={{ sm: 'column', md: 'row' }} spacing={{ xs: 3, sm: 2, md: 4 }}>
              <FormControl fullWidth>
                <InputLabel htmlFor="PaymentUSDRate">{t('setting_index.operationSettings.paymentSettings.usdRate.label')}</InputLabel>
                <OutlinedInput
                  id="PaymentUSDRate"
                  name="PaymentUSDRate"
                  type="number"
                  value={inputs.PaymentUSDRate}
                  onChange={handleInputChange}
                  label={t('setting_index.operationSettings.paymentSettings.usdRate.label')}
                  placeholder={t('setting_index.operationSettings.paymentSettings.usdRate.placeholder')}
                  disabled={loading}
                />
              </FormControl>
              <FormControl fullWidth>
                <InputLabel htmlFor="PaymentMinAmount">{t('setting_index.operationSettings.paymentSettings.minAmount.label')}</InputLabel>
                <OutlinedInput
                  id="PaymentMinAmount"
                  name="PaymentMinAmount"
                  type="number"
                  value={inputs.PaymentMinAmount}
                  onChange={handleInputChange}
                  label={t('setting_index.operationSettings.paymentSettings.minAmount.label')}
                  placeholder={t('setting_index.operationSettings.paymentSettings.minAmount.placeholder')}
                  disabled={loading}
                />
              </FormControl>
            </Stack>
          </Stack>
          <Stack spacing={2}>
            <Alert severity="info">
              <div dangerouslySetInnerHTML={{ __html: t('setting_index.operationSettings.paymentSettings.discountInfo') }} />
            </Alert>
            <FormControl fullWidth>
              <TextField
                multiline
                maxRows={15}
                id="channel-RechargeDiscount-label"
                label={t('setting_index.operationSettings.paymentSettings.discount.label')}
                value={inputs.RechargeDiscount}
                name="RechargeDiscount"
                onChange={handleTextFieldChange}
                aria-describedby="helper-text-channel-RechargeDiscount-label"
                minRows={5}
                placeholder={t('setting_index.operationSettings.paymentSettings.discount.placeholder')}
                disabled={loading}
              />
            </FormControl>
          </Stack>
          <Button
            variant="contained"
            onClick={() => {
              submitConfig('payment').then();
            }}
          >
            {t('setting_index.operationSettings.paymentSettings.save')}
          </Button>
        </Stack>
      </SubCard>

      <SubCard title={t('setting_index.operationSettings.chatLinkSettings.title')}>
        <Stack spacing={2}>
          <Alert severity="info">
            <div dangerouslySetInnerHTML={{ __html: t('setting_index.operationSettings.chatLinkSettings.info') }} />
          </Alert>
          <Stack justifyContent="flex-start" alignItems="flex-start" spacing={2}>
            <FormControlLabel
              sx={{ marginLeft: '0px' }}
              label={t('setting_index.operationSettings.chatLinkSettings.builtinChatEnabled')}
              control={
                <Checkbox
                  checked={dataLoaded ? inputs.BuiltinChatEnabled === 'true' : false}
                  onChange={handleInputChange}
                  name="BuiltinChatEnabled"
                  disabled={!dataLoaded || loading}
                />
              }
            />
            <ChatLinksDataGrid links={inputs.ChatLinks || '[]'} onChange={handleInputChange} />

            <Button
              variant="contained"
              onClick={() => {
                submitConfig('chatlinks').then();
              }}
            >
              {t('setting_index.operationSettings.chatLinkSettings.save')}
            </Button>
          </Stack>
        </Stack>
      </SubCard>

      <SubCard title={t('setting_index.operationSettings.disableChannelKeywordsSettings.title')}>
        <Stack spacing={2}>
          <Stack justifyContent="flex-start" alignItems="flex-start" spacing={2}>
            <FormControl fullWidth>
              <TextField
                multiline
                maxRows={15}
                id="disableChannelKeywords"
                label={t('setting_index.operationSettings.disableChannelKeywordsSettings.info')}
                value={inputs.DisableChannelKeywords}
                name="DisableChannelKeywords"
                onChange={handleTextFieldChange}
                minRows={5}
                placeholder={t('setting_index.operationSettings.disableChannelKeywordsSettings.info')}
                disabled={loading}
              />
            </FormControl>
            <Button
              variant="contained"
              onClick={() => {
                submitConfig('DisableChannelKeywords').then();
              }}
            >
              {t('setting_index.operationSettings.disableChannelKeywordsSettings.save')}
            </Button>
          </Stack>
        </Stack>
      </SubCard>

      <SubCard title={t('setting_index.operationSettings.claudeSettings.title')}>
        <Stack spacing={2}>
          <Stack justifyContent="flex-start" alignItems="flex-start" spacing={2}>
            <FormControl fullWidth>
              <InputLabel htmlFor="ClaudeBudgetTokensPercentage">
                {t('setting_index.operationSettings.claudeSettings.budgetTokensPercentage.label')}
              </InputLabel>
              <OutlinedInput
                id="ClaudeBudgetTokensPercentage"
                name="ClaudeBudgetTokensPercentage"
                type="number"
                value={inputs.ClaudeBudgetTokensPercentage}
                onChange={handleInputChange}
                label={t('setting_index.operationSettings.claudeSettings.budgetTokensPercentage.label')}
                placeholder={t('setting_index.operationSettings.claudeSettings.budgetTokensPercentage.placeholder')}
                disabled={loading}
              />
            </FormControl>

            <FormControl fullWidth>
              <TextField
                multiline
                maxRows={15}
                id="ClaudeDefaultMaxTokens"
                label={t('setting_index.operationSettings.claudeSettings.defaultMaxTokens.label')}
                value={inputs.ClaudeDefaultMaxTokens}
                name="ClaudeDefaultMaxTokens"
                onChange={handleTextFieldChange}
                minRows={5}
                placeholder={t('setting_index.operationSettings.claudeSettings.defaultMaxTokens.placeholder')}
                disabled={loading}
              />
            </FormControl>

            <Button
              variant="contained"
              onClick={() => {
                submitConfig('claude').then();
              }}
            >
              {t('setting_index.operationSettings.claudeSettings.save')}
            </Button>
          </Stack>
        </Stack>
      </SubCard>

      <SubCard title={t('setting_index.operationSettings.geminiSettings.title')}>
        <Stack spacing={2}>
          <Stack justifyContent="flex-start" alignItems="flex-start" spacing={2}>
            <FormControl fullWidth>
              <TextField
                multiline
                maxRows={15}
                id="GeminiOpenThink"
                label={t('setting_index.operationSettings.geminiSettings.geminiOpenThink.label')}
                value={inputs.GeminiOpenThink}
                name="GeminiOpenThink"
                onChange={handleTextFieldChange}
                minRows={5}
                placeholder={t('setting_index.operationSettings.geminiSettings.geminiOpenThink.placeholder')}
                disabled={loading}
              />
            </FormControl>

            <Button
              variant="contained"
              onClick={() => {
                submitConfig('gemini').then();
              }}
            >
              {t('setting_index.operationSettings.geminiSettings.save')}
            </Button>
          </Stack>
        </Stack>
      </SubCard>

      <SubCard title={t('setting_index.operationSettings.safetySettings.title')}>
        <Stack spacing={2}>
          <Stack justifyContent="flex-start" alignItems="flex-start" spacing={2}>
            <FormControlLabel
              label={
                <Stack direction="row" alignItems="center" spacing={1}>
                  <span>{t('setting_index.operationSettings.safetySettings.enableSafe')}</span>
                  <Chip
                    label="Beta"
                    size="small"
                    color="error"
                    sx={{
                      height: '20px',
                      fontSize: '0.75rem',
                      fontWeight: 'bold',
                      backgroundColor: 'red',
                      color: 'white'
                    }}
                  />
                </Stack>
              }
              control={
                <Checkbox
                  checked={dataLoaded ? inputs.EnableSafe === 'true' : false}
                  disabled={!dataLoaded || loading}
                  onChange={(e) => {
                    console.log('Checkbox changed:', e.target.checked);
                    const newValue = e.target.checked ? 'true' : 'false';
                    console.log('Setting EnableSafe to:', newValue);
                    setInputs((prev) => ({
                      ...prev,
                      EnableSafe: newValue
                    }));
                  }}
                />
              }
            />

            <FormControl fullWidth>
              <InputLabel htmlFor="SafeToolName">{t('setting_index.operationSettings.safetySettings.safeToolName.label')}</InputLabel>
              <Select
                id="SafeToolName"
                name="SafeToolName"
                value={inputs.SafeToolName || ''}
                label={t('setting_index.operationSettings.safetySettings.safeToolName.label')}
                disabled={loading || safeToolsLoading}
                onChange={(e) => {
                  setInputs((prev) => ({
                    ...prev,
                    SafeToolName: e.target.value
                  }));
                }}
              >
                {safeToolsLoading && <MenuItem value="">加载中...</MenuItem>}
                {!safeToolsLoading && (!inputs.safeTools || inputs.safeTools.length === 0) && <MenuItem value="">暂无安全工具</MenuItem>}
                {inputs.safeTools &&
                  inputs.safeTools.map((tool) => (
                    <MenuItem key={tool} value={tool}>
                      {tool}
                    </MenuItem>
                  ))}
              </Select>
            </FormControl>

            <FormControl fullWidth>
              <TextField
                multiline
                maxRows={15}
                id="SafeKeyWords"
                label={t('setting_index.operationSettings.safetySettings.safeKeyWords.label')}
                value={Array.isArray(inputs.SafeKeyWords) ? inputs.SafeKeyWords.join('\n') : inputs.SafeKeyWords}
                name="SafeKeyWords"
                onChange={handleTextFieldChange}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' && !e.shiftKey) {
                    e.stopPropagation();
                  }
                }}
                minRows={5}
                placeholder={t('setting_index.operationSettings.safetySettings.safeKeyWords.placeholder')}
                disabled={loading}
              />
            </FormControl>

            <Button
              variant="contained"
              onClick={() => {
                submitConfig('safety').then();
              }}
            >
              {t('setting_index.operationSettings.safetySettings.save')}
            </Button>
          </Stack>
        </Stack>
      </SubCard>

      {/* 按状态码自定义冻结时间：弹窗承载，避免在主表单堆放大量动态行 */}
      <Dialog open={cooldownDialogOpen} onClose={() => setCooldownDialogOpen(false)} maxWidth="sm" fullWidth>
        <DialogTitle>{t('setting_index.operationSettings.generalSettings.retryCooldownPerStatus.title')}</DialogTitle>
        <DialogContent dividers>
          <Alert severity="info" sx={{ mb: 2 }}>
            {t('setting_index.operationSettings.generalSettings.retryCooldownPerStatus.description')}
          </Alert>
          <Typography variant="caption" color="text.secondary" sx={{ mb: 2, display: 'block' }}>
            {t('setting_index.operationSettings.generalSettings.retryCooldownPerStatus.customHelper')}
          </Typography>
          {/* 规则区可在弹窗内自然滚动（DialogContent 默认 overflow:auto） */}
          <Stack spacing={1}>
            {cooldownRules.map((rule) => (
              <Stack key={rule.rid} direction="row" spacing={1} alignItems="center">
                <TextField
                  label={t('setting_index.operationSettings.generalSettings.retryCooldownPerStatus.statusCodeLabel')}
                  size="small"
                  type="number"
                  value={rule.code}
                  onChange={(e) => setCooldownRules((rs) => rs.map((r) => (r.rid === rule.rid ? { ...r, code: e.target.value } : r)))}
                  sx={{ width: 140 }}
                  disabled={loading}
                />
                <TextField
                  label={t('setting_index.operationSettings.generalSettings.retryCooldownPerStatus.secondsLabel')}
                  size="small"
                  type="number"
                  value={rule.secs}
                  onChange={(e) => setCooldownRules((rs) => rs.map((r) => (r.rid === rule.rid ? { ...r, secs: e.target.value } : r)))}
                  sx={{ width: 160 }}
                  disabled={loading}
                />
                <Button
                  color="error"
                  size="small"
                  onClick={() => setCooldownRules((rs) => rs.filter((r) => r.rid !== rule.rid))}
                  disabled={loading}
                >
                  {t('setting_index.operationSettings.generalSettings.retryCooldownPerStatus.delete')}
                </Button>
              </Stack>
            ))}
            <Box>
              <Button
                variant="outlined"
                size="small"
                onClick={() => setCooldownRules((rs) => [...rs, { rid: nextRid(), code: '', secs: '' }])}
                disabled={loading}
              >
                {t('setting_index.operationSettings.generalSettings.retryCooldownPerStatus.addRule')}
              </Button>
            </Box>
          </Stack>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setCooldownDialogOpen(false)}>
            {t('setting_index.operationSettings.generalSettings.retryCooldownPerStatus.close')}
          </Button>
        </DialogActions>
      </Dialog>
    </Stack>
  );
};

export default OperationSetting;
