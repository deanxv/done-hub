import PropTypes from 'prop-types';
import { useMemo, useState, useEffect } from 'react';
import Decimal from 'decimal.js';
import { Box, FormControl, FormHelperText, IconButton, InputAdornment, InputLabel, OutlinedInput, Tooltip } from '@mui/material';
import SwapHorizIcon from '@mui/icons-material/SwapHoriz';
import { useTranslation } from 'react-i18next';

export const QUOTA_UNIT_CURRENCY = 'currency';
export const QUOTA_UNIT_TOKEN = 'token';

// 默认 500000 对齐后端 common/config/constants.go:QuotaPerUnit；后端调整时本地需同步
const getQuotaPerUnit = () => {
  const raw = localStorage.getItem('quota_per_unit');
  const n = parseFloat(raw);
  return Number.isFinite(n) && n > 0 ? n : 500000;
};

const stripTrailingZeros = (s) => s.replace(/\.0+$|(\.\d*?)0+$/, '$1');

const getDefaultUnit = () => {
  const flag = localStorage.getItem('display_in_currency');
  return flag === 'true' ? QUOTA_UNIT_CURRENCY : QUOTA_UNIT_TOKEN;
};

const quotaToCurrencyString = (quota, digits = 6) => {
  const q = Number(quota);
  if (!Number.isFinite(q)) return '';
  return stripTrailingZeros(new Decimal(q).div(getQuotaPerUnit()).toFixed(digits));
};

const currencyToQuota = (money) => {
  const m = Number(money);
  if (!Number.isFinite(m)) return 0;
  return Number(new Decimal(m).mul(getQuotaPerUnit()).toFixed(0));
};

const formatTokens = (n) => {
  const v = Number(n);
  if (!Number.isFinite(v)) return '0';
  return v.toLocaleString('en-US');
};

const formatCurrency = (n) => {
  const v = Number(n);
  if (!Number.isFinite(v)) return '$0.00';
  const abs = Math.abs(v);
  const digits = abs > 0 && abs < 0.01 ? 6 : 2;
  const sign = v < 0 ? '-' : '';
  let body = abs.toFixed(digits);
  if (digits > 2) body = stripTrailingZeros(body);
  return `${sign}$${body}`;
};

const QuotaInput = ({
  id,
  name,
  label,
  value,
  onChange,
  onBlur,
  disabled,
  error,
  helperText,
  placeholder,
  fullWidth = true,
  allowToggle = true,
  defaultUnit,
  maxDeduct,
  inputProps: extraInputProps,
  sx
}) => {
  const { t } = useTranslation();
  const [unit, setUnit] = useState(() => defaultUnit || getDefaultUnit());
  // ''/null/undefined → 空串，让 placeholder 可见；prev === '-' 保护负号过渡；currency 模式下若 prev 与 q 等价则保留用户正在输入的小数尾巴
  const [displayValue, setDisplayValue] = useState(() => {
    if (value === '' || value == null) return '';
    const q = Number(value);
    if (!Number.isFinite(q)) return '';
    return unit === QUOTA_UNIT_CURRENCY ? quotaToCurrencyString(q) : String(q);
  });

  useEffect(() => {
    if (value === '' || value == null) {
      setDisplayValue((prev) => (prev === '-' ? prev : ''));
      return;
    }
    const q = Number(value);
    if (!Number.isFinite(q)) return;
    const next = unit === QUOTA_UNIT_CURRENCY ? quotaToCurrencyString(q) : String(q);
    setDisplayValue((prev) => {
      if (prev === '-') return prev;
      if (prev === '') return next;
      // 两种模式都判断 prev 能否还原回 q，避免在用户输入 "5." 或 "0.50" 等中间态时被 effect 覆写
      if (unit === QUOTA_UNIT_CURRENCY && currencyToQuota(prev) === q) return prev;
      if (unit === QUOTA_UNIT_TOKEN) {
        const pn = Number(prev);
        if (Number.isFinite(pn) && Math.trunc(pn) === q) return prev;
      }
      return next;
    });
  }, [value, unit]);

  const handleToggleUnit = () => {
    setUnit((prev) => (prev === QUOTA_UNIT_CURRENCY ? QUOTA_UNIT_TOKEN : QUOTA_UNIT_CURRENCY));
  };

  const handleInput = (e) => {
    const raw = e.target.value;
    // 两种模式都保留 raw 字符串供 display；token 仅在 emit 时 trunc，避免用户输入过程中 "5." 被立即压成 "5"
    setDisplayValue(raw);
    if (raw === '' || raw === '-') {
      onChange?.({ target: { name, value: '' } });
      return;
    }
    if (unit === QUOTA_UNIT_CURRENCY) {
      const quota = currencyToQuota(raw);
      onChange?.({ target: { name, value: Number.isFinite(quota) ? quota : 0 } });
    } else {
      const truncated = Math.trunc(Number(raw));
      const safe = Number.isFinite(truncated) ? truncated : 0;
      onChange?.({ target: { name, value: safe } });
    }
  };

  const equivalentLabel = useMemo(() => {
    if (unit === QUOTA_UNIT_CURRENCY) {
      // currency 模式跟 precisionHint 同基于 displayValue 算，避免"≈ 1,000,000 / 将四舍五入为 1,000,000"两口径分裂
      if (displayValue === '' || displayValue === '-') {
        return t('common.quotaInput.equivalentTokens', { tokens: '0' });
      }
      const m = Number(displayValue);
      const tokens = Number.isFinite(m) ? new Decimal(m).mul(getQuotaPerUnit()).toFixed(0) : '0';
      return t('common.quotaInput.equivalentTokens', { tokens: formatTokens(tokens) });
    }
    const q = Number(value) || 0;
    return t('common.quotaInput.equivalentMoney', {
      amount: formatCurrency(Number(quotaToCurrencyString(q, 6)))
    });
  }, [value, unit, displayValue, t]);

  const precisionHint = useMemo(() => {
    if (unit !== QUOTA_UNIT_CURRENCY) return '';
    if (displayValue === '' || displayValue === '-') return '';
    const m = Number(displayValue);
    if (!Number.isFinite(m)) return '';
    const exact = new Decimal(m).mul(getQuotaPerUnit());
    const rounded = exact.toFixed(0);
    if (!exact.equals(rounded)) {
      return t('common.quotaInput.precisionHint', { tokens: formatTokens(rounded) });
    }
    return '';
  }, [displayValue, unit, t]);

  // 仅当 value 为负且 |value| > maxDeduct 时报警；加额（正数）不受 maxDeduct 限制
  const overLimit = useMemo(() => {
    if (maxDeduct == null) return false;
    const q = Number(value);
    if (!Number.isFinite(q) || q >= 0) return false;
    return Math.abs(q) > Math.abs(Number(maxDeduct));
  }, [value, maxDeduct]);

  const overLimitText = useMemo(() => {
    if (!overLimit) return '';
    const maxQ = Math.abs(Number(maxDeduct));
    const maxStr = unit === QUOTA_UNIT_CURRENCY ? formatCurrency(Number(quotaToCurrencyString(maxQ, 6))) : `${formatTokens(maxQ)} tokens`;
    return t('common.quotaInput.overDeductLimit', { max: maxStr });
  }, [overLimit, maxDeduct, unit, t]);

  const isError = Boolean(error) || overLimit;

  // 两种单位默认都加 min:0 防误输负数（UX 对称）；除非父组件传 maxDeduct，表示明确允许负值扣减
  const numberInputProps =
    unit === QUOTA_UNIT_CURRENCY ? { step: 0.01, ...(maxDeduct == null && { min: 0 }) } : { step: 1, ...(maxDeduct == null && { min: 0 }) };

  const startAdornment = <InputAdornment position="start">{unit === QUOTA_UNIT_CURRENCY ? '$' : 'T'}</InputAdornment>;

  const endAdornment = allowToggle ? (
    <InputAdornment position="end">
      <Tooltip title={t(unit === QUOTA_UNIT_CURRENCY ? 'common.quotaInput.toggleToToken' : 'common.quotaInput.toggleToCurrency')}>
        <span>
          <IconButton edge="end" size="small" onClick={handleToggleUnit} disabled={disabled} aria-label="toggle quota unit">
            <SwapHorizIcon fontSize="small" />
          </IconButton>
        </span>
      </Tooltip>
    </InputAdornment>
  ) : null;

  // helperText 分两行：主行汇集调用方 helperText / precisionHint / overLimit 警示——
  // 超限时仍保留余额信息（即"当前余额：X"），用户最需要的对比恰好就在这一刻
  const primaryParts = [];
  if (overLimit) primaryParts.push(overLimitText);
  if (helperText) primaryParts.push(helperText);
  if (precisionHint) primaryParts.push(precisionHint);
  const hasPrimary = primaryParts.length > 0;

  return (
    <FormControl fullWidth={fullWidth} error={isError} sx={sx}>
      <InputLabel htmlFor={id || name}>{label}</InputLabel>
      <OutlinedInput
        id={id || name}
        name={name}
        label={label}
        type="number"
        inputProps={{ ...numberInputProps, ...extraInputProps }}
        value={displayValue}
        onChange={handleInput}
        onBlur={onBlur}
        disabled={disabled}
        placeholder={placeholder}
        startAdornment={startAdornment}
        endAdornment={endAdornment}
      />
      <FormHelperText error={isError} component="div">
        {hasPrimary && (
          <Box>
            {primaryParts.map((part, i) => (
              <span key={`p-${i}`}>
                {i > 0 && ' · '}
                {part}
              </span>
            ))}
          </Box>
        )}
        <Box sx={{ color: isError ? 'inherit' : 'text.secondary' }}>{equivalentLabel}</Box>
      </FormHelperText>
    </FormControl>
  );
};

QuotaInput.propTypes = {
  id: PropTypes.string,
  name: PropTypes.string,
  label: PropTypes.string,
  value: PropTypes.oneOfType([PropTypes.number, PropTypes.string]),
  onChange: PropTypes.func,
  onBlur: PropTypes.func,
  disabled: PropTypes.bool,
  error: PropTypes.bool,
  helperText: PropTypes.node,
  placeholder: PropTypes.string,
  fullWidth: PropTypes.bool,
  allowToggle: PropTypes.bool,
  defaultUnit: PropTypes.oneOf([QUOTA_UNIT_CURRENCY, QUOTA_UNIT_TOKEN]),
  maxDeduct: PropTypes.oneOfType([PropTypes.number, PropTypes.string]),
  inputProps: PropTypes.object,
  sx: PropTypes.oneOfType([PropTypes.object, PropTypes.array, PropTypes.func])
};

export default QuotaInput;
