import PropTypes from 'prop-types';
import * as Yup from 'yup';
import { Formik } from 'formik'; // 1. 导入 useFormikContext
import { useTheme } from '@mui/material/styles';
import { useState, useEffect } from 'react';
import { useSelector } from 'react-redux';
import dayjs from 'dayjs';
import ModelLimitSelector from './ModelLimitSelector';
import {
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Button,
  Divider,
  Alert,
  FormControl,
  InputLabel,
  OutlinedInput,
  Switch,
  FormControlLabel,
  FormHelperText,
  Select,
  MenuItem,
  Typography,
  Grid,
  TextField,
  ListItemText,
  Box,
  IconButton,
  Tooltip
} from '@mui/material';
import { Icon } from '@iconify/react';
import RatioBadge from 'ui-component/RatioBadge';

import { AdapterDayjs } from '@mui/x-date-pickers/AdapterDayjs';
import { LocalizationProvider } from '@mui/x-date-pickers/LocalizationProvider';
import { DateTimePicker } from '@mui/x-date-pickers/DateTimePicker';
import { copy, showSuccess, showError, useIsReliable } from 'utils/common';
import { API } from 'utils/api';
import UnknownType from 'assets/images/icons/unknown_type.svg';
import QuotaInput from 'ui-component/QuotaInput';
import { useTranslation } from 'react-i18next';
import 'dayjs/locale/zh-cn';

const validationSchema = Yup.object().shape({
  is_edit: Yup.boolean(),
  name: Yup.string().required('名称 不能为空'),
  remain_quota: Yup.number().min(0, '必须大于等于0'),
  expired_time: Yup.number(),
  unlimited_quota: Yup.boolean(),
  setting: Yup.object().shape({
    heartbeat: Yup.object().shape({
      enabled: Yup.boolean(),
      timeout_seconds: Yup.number().when('enabled', {
        is: true,
        then: () => Yup.number().min(30, '时间 必须大于等于30秒').max(90, '时间 必须小于等于90秒').required('时间 不能为空'),
        otherwise: () => Yup.number()
      })
    }),
    limits: Yup.object().shape({
      limits_ip_setting: Yup.object().shape({
        enabled: Yup.boolean(),
        whitelist: Yup.array().of(Yup.string())
      })
    })
  })
});

const originInputs = {
  is_edit: false,
  name: '',
  remain_quota: 0,
  expired_time: -1,
  unlimited_quota: true,
  group: '',
  backup_group: '',
  setting: {
    heartbeat: {
      enabled: false,
      timeout_seconds: 30
    },
    limits: {
      limit_model_setting: {
        enabled: false,
        models: []
      },
      limits_ip_setting: {
        enabled: false,
        whitelist: []
      }
    }
  }
};

const EditModal = ({ open, tokenId, onCancel, onOk, userGroupOptions, adminMode = false }) => {
  const { t } = useTranslation();
  const theme = useTheme();
  const userIsReliable = useIsReliable();
  const { user, userGroup } = useSelector((state) => state.account);
  const [inputs, setInputs] = useState(originInputs);
  const [createdKey, setCreatedKey] = useState(null);

  // admin 模式编辑别人的 token 时，当前 redux 里的 user 是管理员自己，不能代表 token 所属用户的「跟随分组」
  const followingGroup = !adminMode && user?.group ? userGroup?.[user.group] : null;

  // 当前值是已不可用的分组时，临时插入一个 disabled 兜底项用于回填显示，避免 MUI Select value 失配显示空白
  const optionsWithFallback = (currentValue) => {
    if (!currentValue || userGroupOptions.some((o) => o.value === currentValue)) {
      return userGroupOptions;
    }
    const g = userGroup?.[currentValue];
    return [
      ...userGroupOptions,
      {
        value: currentValue,
        name: g?.name || currentValue,
        ratio: g?.ratio,
        desc: g?.description || '',
        disabled: true,
        inaccessible: true
      }
    ];
  };

  const renderGroupValue = (selected, placeholder, followingRatio) => {
    if (selected === '' || selected === '-1') {
      return (
        <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', width: '100%', gap: 1 }}>
          <span>{placeholder}</span>
          {followingRatio !== undefined && followingRatio !== null && <RatioBadge ratio={followingRatio} />}
        </Box>
      );
    }
    const opt = optionsWithFallback(selected).find((o) => o.value === selected);
    if (!opt) return selected;
    return (
      <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', width: '100%', gap: 1 }}>
        <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
          {opt.name}
          {opt.inaccessible ? ' (不可用)' : ''}
        </span>
        <RatioBadge ratio={opt.ratio} />
      </Box>
    );
  };

  const renderGroupMenuItem = (option) => (
    <Box sx={{ display: 'flex', alignItems: 'center', width: '100%', gap: 1 }}>
      <ListItemText
        sx={{ my: 0, flex: 1, minWidth: 0 }}
        primary={option.name + (option.inaccessible ? ' (不可用)' : '')}
        secondary={option.desc || null}
        primaryTypographyProps={{
          sx: { overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }
        }}
        secondaryTypographyProps={{
          sx: { fontSize: '0.7rem', whiteSpace: 'normal', lineHeight: 1.2 }
        }}
      />
      <RatioBadge ratio={option.ratio} />
    </Box>
  );
  const [modelOptions, setModelOptions] = useState([]);
  const [ownedByIcons, setOwnedByIcons] = useState({});
  const fetchOwnedByIcons = async () => {
    try {
      const res = await API.get('/api/model_ownedby/');
      const { success, data } = res.data;
      if (success) {
        const iconMap = {};
        data.forEach((provider) => {
          iconMap[provider.name] = provider.icon;
        });
        setOwnedByIcons(iconMap);
      }
    } catch (error) {
      console.error('获取模型提供商图标失败:', error);
    }
  };

  const fetchModelOptions = async () => {
    try {
      const res = await API.get('/api/available_model');
      const { success, data } = res.data;
      if (success) {
        const models = Object.keys(data).map((modelId) => ({
          id: modelId,
          name: modelId,
          owned_by: data[modelId].owned_by,
          groups: data[modelId].groups,
          price: data[modelId].price
        }));
        setModelOptions(models);
      }
    } catch (error) {
      console.error('获取模型列表失败:', error);
    }
  };

  const getModelIcon = (ownedBy) => {
    return ownedByIcons[ownedBy] || UnknownType;
  };

  const submit = async (values, { setErrors, setStatus, setSubmitting }) => {
    setSubmitting(true);
    values.remain_quota = parseInt(values.remain_quota);
    values.setting.heartbeat.timeout_seconds = parseInt(values.setting.heartbeat.timeout_seconds);

    // 过滤掉空的 IP 行
    if (values.setting?.limits?.limits_ip_setting?.whitelist) {
      values.setting.limits.limits_ip_setting.whitelist = values.setting.limits.limits_ip_setting.whitelist.filter(ip => ip.trim() !== '');
    }
    let res;
    try {
      if (values.is_edit) {
        // 管理员模式使用管理员专用接口
        const apiPath = adminMode ? `/api/token/admin` : `/api/token/`;
        const payload = { ...values, id: parseInt(tokenId) };
        // 管理员模式下传递 user_id
        if (adminMode && values.user_id) {
          payload.user_id = parseInt(values.user_id);
        }
        res = await API.put(apiPath, payload);
      } else {
        res = await API.post(`/api/token/`, values);
      }
      const { success, message, data } = res.data;
      if (success) {
        setSubmitting(false);
        setStatus({ success: true });
        if (values.is_edit) {
          showSuccess('令牌更新成功！');
          onOk(true);
        } else if (data?.key) {
          // 创建成功，停留在 Dialog 内显示完整 key 供用户复制
          setCreatedKey(data.key);
        } else {
          showSuccess('令牌创建成功！');
          onOk(true);
        }
      } else {
        showError(message);
        setErrors({ submit: message });
      }
    } catch (error) {
      return;
    }
  };

  const handleCreatedDone = () => {
    // 不在这里清 createdKey：Dialog 关闭动画期间 children 仍挂载，立即清会让内容闪回 Formik 表单。
    // 清理放在 useEffect 里、下次 open=true 时执行。
    onOk(true);
  };

  const handleClose = () => {
    if (createdKey) {
      handleCreatedDone();
    } else {
      onCancel();
    }
  };

  const loadToken = async () => {
    try {
      let res;
      if (adminMode) {
        // 管理员模式使用搜索接口通过token_id查询
        res = await API.get(`/api/token/admin/search`, {
          params: { token_id: tokenId, page: 1, size: 1 }
        });
      } else {
        res = await API.get(`/api/token/${tokenId}`);
      }
      const { success, message, data } = res.data;
      if (success) {
        // 管理员搜索接口返回的是分页数据，取第一条
        const tokenData = adminMode ? data.data[0] : data;
        if (!tokenData) {
          showError('令牌不存在');
          return;
        }
        tokenData.is_edit = true;
        if (!tokenData.setting) tokenData.setting = originInputs.setting;
        if (!tokenData.setting.limits) tokenData.setting.limits = originInputs.setting.limits;
        if (!tokenData.setting.limits.limit_model_setting)
          tokenData.setting.limits.limit_model_setting = originInputs.setting.limits.limit_model_setting;
        if (!tokenData.setting.limits.limits_ip_setting) tokenData.setting.limits.limits_ip_setting = originInputs.setting.limits.limits_ip_setting;
        if (!tokenData.setting.limits.limit_model_setting.models) tokenData.setting.limits.limit_model_setting.models = [];
        if (!tokenData.setting.limits.limits_ip_setting.whitelist) tokenData.setting.limits.limits_ip_setting.whitelist = [];
        setInputs(tokenData);
      } else {
        showError(message);
      }
    } catch (error) {
      return;
    }
  };

  useEffect(() => {
    if (open) {
      // 打开新对话框时重置上一次的创建结果（放在打开时清而非关闭时清，
      // 是为了避免 Dialog 退出动画期间内容从 success view 闪回 Formik 表单）
      setCreatedKey(null);
      fetchOwnedByIcons();
      fetchModelOptions();
    }
  }, [open]);

  useEffect(() => {
    if (tokenId) {
      loadToken().then();
    } else {
      setInputs(originInputs);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tokenId, adminMode]);

  return (
    <Dialog open={open} onClose={handleClose} fullWidth maxWidth={'md'}>
      <DialogTitle sx={{ margin: '0px', fontWeight: 700, lineHeight: '1.55556', padding: '24px', fontSize: '1.125rem' }}>
        {createdKey ? t('token_index.createdSuccessTitle') : tokenId ? t('token_index.editToken') : t('token_index.createToken')}
      </DialogTitle>
      <Divider />
      <DialogContent>
        {createdKey ? (
          <Box>
            <Alert severity="warning" sx={{ mb: 2 }}>
              {t('token_index.createdSuccessTip')}
            </Alert>
            <Box
              sx={{
                display: 'flex',
                alignItems: 'center',
                gap: 1,
                p: 1.5,
                bgcolor: 'action.hover',
                borderRadius: 1,
                border: '1px solid',
                borderColor: 'divider'
              }}
            >
              <Box
                component="code"
                sx={{
                  flex: 1,
                  fontFamily: 'monospace',
                  fontSize: '0.875rem',
                  wordBreak: 'break-all',
                  userSelect: 'all'
                }}
              >
                {`sk-${createdKey}`}
              </Box>
              <Tooltip title={t('token_index.copy')} placement="top" arrow>
                <IconButton
                  size="small"
                  sx={{ color: 'primary.main', flexShrink: 0 }}
                  onClick={() => copy(`sk-${createdKey}`, t('token_index.token'))}
                >
                  <Icon icon="solar:copy-bold-duotone" width={20} />
                </IconButton>
              </Tooltip>
            </Box>
            <DialogActions sx={{ px: 0, pt: 3 }}>
              <Button variant="contained" color="primary" onClick={handleCreatedDone}>
                {t('token_index.done')}
              </Button>
            </DialogActions>
          </Box>
        ) : (
          <Formik initialValues={inputs} enableReinitialize validationSchema={validationSchema} onSubmit={submit}>
            {({ errors, handleBlur, handleChange, handleSubmit, touched, values, setFieldError, setFieldValue, isSubmitting }) => (
              <form noValidate onSubmit={handleSubmit}>
                {/* 管理员模式下显示用户转移字段 */}
                {adminMode && values.is_edit && (
                  <>
                    <Alert severity="warning" sx={{ mb: 2 }}>
                      {t('token_index.adminEditWarning')}
                    </Alert>
                    <FormControl fullWidth sx={{ ...theme.typography.otherInput }}>
                      <InputLabel htmlFor="token-user-id-label">{t('token_index.transferToUser')}</InputLabel>
                      <OutlinedInput
                        id="token-user-id-label"
                        label={t('token_index.transferToUser')}
                        type="number"
                        value={values.user_id || ''}
                        name="user_id"
                        onBlur={handleBlur}
                        onChange={handleChange}
                        inputProps={{ autoComplete: 'off' }}
                        aria-describedby="helper-text-token-user-id-label"
                      />
                      <FormHelperText id="helper-text-token-user-id-label">{t('token_index.transferToUserHelper')}</FormHelperText>
                    </FormControl>
                  </>
                )}
                <FormControl fullWidth error={Boolean(touched.name && errors.name)} sx={{ ...theme.typography.otherInput }}>
                  <InputLabel htmlFor="channel-name-label">{t('token_index.name')}</InputLabel>
                  <OutlinedInput
                    id="channel-name-label"
                    label={t('token_index.name')}
                    type="text"
                    value={values.name}
                    name="name"
                    onBlur={handleBlur}
                    onChange={handleChange}
                    inputProps={{ autoComplete: 'name' }}
                    aria-describedby="helper-text-channel-name-label"
                  />
                  {touched.name && errors.name && (
                    <FormHelperText error id="helper-tex-channel-name-label">
                      {errors.name}
                    </FormHelperText>
                  )}
                </FormControl>
                <FormControlLabel
                  control={
                    <Switch
                      checked={values.expired_time === -1}
                      onClick={() => {
                        if (values.expired_time === -1) {
                          setFieldValue('expired_time', Math.floor(Date.now() / 1000));
                        } else {
                          setFieldValue('expired_time', -1);
                        }
                      }}
                    />
                  }
                  label={t('token_index.neverExpires')}
                />
                {values.expired_time !== -1 && (
                  <FormControl fullWidth error={Boolean(touched.expired_time && errors.expired_time)} sx={{ ...theme.typography.otherInput }}>
                    <LocalizationProvider dateAdapter={AdapterDayjs} adapterLocale={'zh-cn'}>
                      <DateTimePicker
                        label={t('token_index.expiryTime')}
                        ampm={false}
                        value={dayjs.unix(values.expired_time)}
                        onError={(newError) => {
                          if (newError === null) {
                            setFieldError('expired_time', null);
                          } else {
                            setFieldError('expired_time', t('token_index.invalidDate'));
                          }
                        }}
                        onChange={(newValue) => {
                          setFieldValue('expired_time', newValue.unix());
                        }}
                        slotProps={{
                          actionBar: {
                            actions: ['today', 'accept']
                          }
                        }}
                      />
                    </LocalizationProvider>
                    {errors.expired_time && (
                      <FormHelperText error id="helper-tex-channel-expired_time-label">
                        {errors.expired_time}
                      </FormHelperText>
                    )}
                  </FormControl>
                )}
                <FormControl fullWidth>
                  <FormControlLabel
                    control={
                      <Switch
                        checked={values.unlimited_quota === true}
                        onClick={() => {
                          setFieldValue('unlimited_quota', !values.unlimited_quota);
                        }}
                      />
                    }
                    label={t('token_index.unlimitedQuota')}
                  />
                </FormControl>
                <QuotaInput
                  id="channel-remain_quota-label"
                  name="remain_quota"
                  label={t('token_index.quota')}
                  value={values.remain_quota}
                  onChange={handleChange}
                  onBlur={handleBlur}
                  disabled={values.unlimited_quota}
                  error={Boolean(touched.remain_quota && errors.remain_quota)}
                  helperText={touched.remain_quota && errors.remain_quota ? errors.remain_quota : ''}
                  sx={{ ...theme.typography.otherInput }}
                />
                <Alert severity="info">{t('token_index.quotaNote')}</Alert>
                <Divider sx={{ margin: '16px 0px' }} />
                <Typography variant="h4">{t('token_index.heartbeat')}</Typography>
                <Typography variant="caption">{t('token_index.heartbeatTip')}</Typography>

                <FormControl fullWidth>
                  <FormControlLabel
                    control={
                      <Switch
                        checked={values?.setting?.heartbeat?.enabled === true}
                        onClick={() => {
                          setFieldValue('setting.heartbeat.enabled', !values.setting?.heartbeat?.enabled);
                        }}
                      />
                    }
                    label={t('token_index.heartbeat')}
                  />
                </FormControl>

                {values?.setting?.heartbeat?.enabled && (
                  <FormControl fullWidth>
                    <InputLabel>{t('token_index.heartbeatTimeout')}</InputLabel>
                    <OutlinedInput
                      id="channel-heartbeat-timeout-label"
                      label={t('token_index.heartbeatTimeout')}
                      type="number"
                      value={values?.setting?.heartbeat?.timeout_seconds}
                      onChange={(e) => {
                        setFieldValue('setting.heartbeat.timeout_seconds', e.target.value);
                      }}
                    />

                    {touched.setting?.heartbeat?.timeout_seconds && errors.setting?.heartbeat?.timeout_seconds ? (
                      <FormHelperText error id="helper-tex-channel-heartbeat-timeout-label">
                        {errors.setting?.heartbeat?.timeout_seconds}
                      </FormHelperText>
                    ) : (
                      <FormHelperText id="helper-tex-channel-heartbeat-timeout-label">
                        {t('token_index.heartbeatTimeoutHelperText')}
                      </FormHelperText>
                    )}
                  </FormControl>
                )}

                <Divider sx={{ margin: '16px 0px' }} />
                <Typography variant="h4">{t('token_index.selectGroup')}</Typography>
                <Typography variant="caption">{t('token_index.selectGroupInfo')}</Typography>
                <Grid container spacing={2} mt={2}>
                  <Grid item xs={12} md={6}>
                    <FormControl fullWidth>
                      <InputLabel>{t('token_index.userGroup')}</InputLabel>
                      <Select
                        label={t('token_index.userGroup')}
                        name="group"
                        value={values.group || '-1'}
                        onChange={(e) => {
                          const value = e.target.value === '-1' ? '' : e.target.value;
                          setFieldValue('group', value);
                          if (values.backup_group === value && value !== '') {
                            setFieldValue('backup_group', '');
                          }
                        }}
                        variant={'outlined'}
                        renderValue={(selected) => renderGroupValue(selected, '跟随用户分组', followingGroup?.ratio)}
                      >
                        <MenuItem value="-1">
                          <Box sx={{ display: 'flex', alignItems: 'center', width: '100%', gap: 1 }}>
                            <ListItemText
                              sx={{ my: 0, flex: 1, minWidth: 0 }}
                              primary="跟随用户分组"
                              secondary={followingGroup ? `当前：${followingGroup.name}` : null}
                              primaryTypographyProps={{
                                sx: { overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }
                              }}
                              secondaryTypographyProps={{
                                sx: { fontSize: '0.7rem', whiteSpace: 'normal', lineHeight: 1.2 }
                              }}
                            />
                            {followingGroup && <RatioBadge ratio={followingGroup.ratio} />}
                          </Box>
                        </MenuItem>
                        {optionsWithFallback(values.group).map((option) => (
                          <MenuItem key={option.value} value={option.value} disabled={option.disabled}>
                            {renderGroupMenuItem(option)}
                          </MenuItem>
                        ))}
                      </Select>
                    </FormControl>
                  </Grid>
                  <Grid item xs={12} md={6}>
                    <FormControl fullWidth>
                      <InputLabel>{t('token_index.userBackupGroup')}</InputLabel>
                      <Select
                        label={t('token_index.userBackupGroup')}
                        name="backup_group"
                        value={values.backup_group || '-1'}
                        onChange={(e) => {
                          const value = e.target.value === '-1' ? '' : e.target.value;
                          setFieldValue('backup_group', value);
                        }}
                        variant={'outlined'}
                        renderValue={(selected) => renderGroupValue(selected, '无备用分组')}
                      >
                        <MenuItem value="-1">无备用分组</MenuItem>
                        {optionsWithFallback(values.backup_group).map((option) => (
                          <MenuItem
                            key={option.value}
                            value={option.value}
                            disabled={option.disabled || (values.group === option.value && values.group !== '')}
                          >
                            {renderGroupMenuItem(option)}
                          </MenuItem>
                        ))}
                      </Select>
                    </FormControl>
                  </Grid>
                </Grid>

                {/*令牌限制设置*/}
                <Divider sx={{ margin: '16px 0px' }} />
                <Typography variant="h4">{t('token_index.limits')}</Typography>
                <Typography variant="caption">{t('token_index.limits_info')}</Typography>

                {/*是否开启限制*/}
                <FormControl fullWidth>
                  <FormControlLabel
                    control={
                      <Switch
                        checked={values?.setting?.limits?.limit_model_setting?.enabled === true}
                        onClick={() => {
                          const newEnabledState = !values.setting?.limits?.limit_model_setting?.enabled;
                          setFieldValue('setting.limits.limit_model_setting.enabled', newEnabledState);
                          if (!newEnabledState) {
                            setFieldValue('setting.limits.limit_model_setting.models', []);
                          }
                        }}
                      />
                    }
                    label={t('token_index.limits_models_switch')}
                  />
                </FormControl>
                {values?.setting?.limits?.limit_model_setting?.enabled && (
                  <ModelLimitSelector modelOptions={modelOptions} getModelIcon={getModelIcon} />
                )}


                {/* IP 白名单限制 */}
                <Divider sx={{ margin: '16px 0px' }} />
                <Typography variant="caption">{t('token_index.limits_ip_whitelist_info')}</Typography>

                <FormControl fullWidth>
                  <FormControlLabel
                    control={
                      <Switch
                        checked={values?.setting?.limits?.limits_ip_setting?.enabled === true}
                        onClick={() => {
                          const newEnabledState = !values.setting?.limits?.limits_ip_setting?.enabled;
                          setFieldValue('setting.limits.limits_ip_setting.enabled', newEnabledState);
                          if (!newEnabledState) {
                            setFieldValue('setting.limits.limits_ip_setting.whitelist', []);
                          }
                        }}
                      />
                    }
                    label={t('token_index.limits_ip_whitelist_switch')}
                  />
                </FormControl>

                {values?.setting?.limits?.limits_ip_setting?.enabled && (
                  <FormControl fullWidth sx={{ ...theme.typography.otherInput }}>
                    <TextField
                      label={t('token_index.limits_ip_whitelist_input')}
                      multiline
                      rows={6}
                      value={values?.setting?.limits?.limits_ip_setting?.whitelist?.join('\n') || ''}
                      onChange={(e) => {
                        const lines = e.target.value.split('\n');
                        setFieldValue('setting.limits.limits_ip_setting.whitelist', lines);
                      }}
                      placeholder="192.168.1.1&#10;10.0.0.0/8&#10;172.16.0.0/12"
                      helperText={t('token_index.limits_ip_whitelist_helper')}
                    />
                  </FormControl>
                )}

                {/* 费用标签 - 仅可信用户及以上可见 */}
                {userIsReliable && (
                  <>
                    <Divider sx={{ margin: '16px 0px' }} />
                    <Typography variant="h4" color="primary">
                      {t('token_index.billingTag')}
                    </Typography>
                    <Typography variant="caption">{t('token_index.billingTagInfo')}</Typography>
                    <Grid container spacing={2} mt={2}>
                      <Grid item xs={12} md={6}>
                        <FormControl fullWidth>
                          <InputLabel>{t('token_index.billingTagLabel')}</InputLabel>
                          <Select
                            label={t('token_index.billingTagLabel')}
                            name="setting.billing_tag"
                            value={values?.setting?.billing_tag || ''}
                            onChange={(e) => {
                              const value = e.target.value === '' ? null : e.target.value;
                              setFieldValue('setting.billing_tag', value);
                            }}
                            variant={'outlined'}
                            renderValue={(selected) => renderGroupValue(selected, '-')}
                          >
                            <MenuItem value="">-</MenuItem>
                            {optionsWithFallback(values?.setting?.billing_tag).map((option) => (
                              <MenuItem key={option.value} value={option.value} disabled={option.disabled}>
                                {renderGroupMenuItem(option)}
                              </MenuItem>
                            ))}
                          </Select>
                          <FormHelperText>{t('token_index.billingTagHelper')}</FormHelperText>
                        </FormControl>
                      </Grid>
                    </Grid>
                  </>
                )}

                <DialogActions>
                  <Button onClick={onCancel}>{t('token_index.cancel')}</Button>
                  <Button disableElevation disabled={isSubmitting} type="submit" variant="contained" color="primary">
                    {t('token_index.submit')}
                  </Button>
                </DialogActions>
              </form>
            )}
          </Formik>
        )}
      </DialogContent>
    </Dialog>
  );
};

export default EditModal;

EditModal.propTypes = {
  open: PropTypes.bool,
  tokenId: PropTypes.number,
  onCancel: PropTypes.func,
  onOk: PropTypes.func,
  userGroupOptions: PropTypes.array,
  adminMode: PropTypes.bool
};
