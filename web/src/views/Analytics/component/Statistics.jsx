import { useEffect, useRef, useState } from 'react';
import { Grid } from '@mui/material';
import DataCard from 'ui-component/cards/DataCard';
import { gridSpacing } from 'store/constant';
import { renderQuota, renderNumber, showError } from 'utils/common';
import { API } from 'utils/api';
import { useTranslation } from 'react-i18next';
import { useSelector } from 'react-redux';
import dayjs from 'dayjs';
import 'dayjs/locale/zh-cn';
import 'dayjs/locale/ja';
import 'dayjs/locale/en';

export default function Overview() {
  const { t, i18n } = useTranslation();
  const siteInfo = useSelector((state) => state.siteInfo);
  const [userLoading, setUserLoading] = useState(true);
  const [channelLoading, setChannelLoading] = useState(true);
  const [rechargeLoading, setRechargeLoading] = useState(true);
  const [rpmTpmLoading, setRpmTpmLoading] = useState(true);

  const [userStatistics, setUserStatistics] = useState({});
  const [channelStatistics, setChannelStatistics] = useState({
    active: 0,
    disabled: 0,
    test_disabled: 0,
    total: 0
  });
  const [rechargeStatistics, setRechargeStatistics] = useState({
    total: 0,
    Redemption: 0,
    Oder: 0,
    OderContent: ''
  });
  const [rpmTpmStatistics, setRpmTpmStatistics] = useState({
    rpm: 0,
    tpm: 0,
    cpm: 0,
    ppm: 0
  });

  const [rechargeTimeFilter, setRechargeTimeFilter] = useState('month');
  // 实时流量卡片自动刷新开关，默认开启
  const [realtimeAutoRefresh, setRealtimeAutoRefresh] = useState(true);

  const timeFilterOptions = [
    { value: 'day', label: t('analytics_index.timeFilter.day') },
    { value: 'week', label: t('analytics_index.timeFilter.week') },
    { value: 'month', label: t('analytics_index.timeFilter.month') },
    { value: 'year', label: t('analytics_index.timeFilter.year') },
    { value: 'all', label: t('analytics_index.timeFilter.all') }
  ];

  // 获取dayjs locale
  const getDayjsLocale = () => {
    const currentLang = i18n.language || 'zh_CN';
    if (currentLang === 'en_US') return 'en';
    if (currentLang === 'ja_JP') return 'ja';
    return 'zh-cn'; // 默认中文locale（周一开始）
  };

  // 获取时间范围 - 保留给其他功能使用
  const getTimeRange = (filterType) => {
    const now = dayjs().locale(getDayjsLocale());
    switch (filterType) {
      case 'year':
        return { start: now.startOf('year').unix(), end: now.endOf('year').unix() };
      case 'month':
        return { start: now.startOf('month').unix(), end: now.endOf('month').unix() };
      case 'week':
        return { start: now.startOf('week').unix(), end: now.endOf('week').unix() };
      case 'day':
        return { start: now.startOf('day').unix(), end: now.endOf('day').unix() };
      default:
        return null;
    }
  };

  // 处理用户统计数据
  const processUserStatistics = (data) => {
    setUserStatistics({
      ...data,
      total_quota: renderQuota(data.total_quota),
      total_used_quota: renderQuota(data.total_used_quota),
      total_direct_user: data.total_user - data.total_inviter_user
    });
  };

  // 处理通道统计数据
  const processChannelStatistics = (data) => {
    const channelData = { active: 0, disabled: 0, test_disabled: 0, total: 0 };
    data.forEach((item) => {
      channelData.total += item.total_channels;
      if (item.status === 1) channelData.active = item.total_channels;
      else if (item.status === 2) channelData.disabled = item.total_channels;
      else if (item.status === 3) channelData.test_disabled = item.total_channels;
    });
    setChannelStatistics(channelData);
  };

  // 处理新接口返回的充值统计数据
  const processRechargeStatistics = (data) => {
    const rechargeData = {
      total: renderQuota(data.total || 0),
      Redemption: renderQuota(data.redemption_amount || 0),
      Oder: renderQuota(data.order_amount || 0),
      OderContent: data.order_currency_info || ''
    };
    setRechargeStatistics(rechargeData);
  };

  // 获取充值统计数据 - 使用新的专用接口
  const fetchRechargeStatistics = async (timeFilter) => {
    setRechargeLoading(true);
    try {
      const res = await API.get('/api/analytics/recharge', {
        params: { time_range: timeFilter }
      });
      const { success, message, data } = res.data;

      if (success && data) {
        processRechargeStatistics(data);
      } else {
        showError(message);
      }
    } catch (error) {
      console.log(error);
      showError('获取充值统计数据失败');
    } finally {
      setRechargeLoading(false);
    }
  };

  // 获取基础统计数据
  const fetchBasicStatistics = async () => {
    try {
      const res = await API.get('/api/analytics/statistics');
      const { success, message, data } = res.data;

      if (success) {
        if (data.user_statistics) processUserStatistics(data.user_statistics);
        if (data.channel_statistics) processChannelStatistics(data.channel_statistics);
        if (data.rpm_tpm_statistics) setRpmTpmStatistics(data.rpm_tpm_statistics);

        setUserLoading(false);
        setChannelLoading(false);
        setRpmTpmLoading(false);
      } else {
        showError(message);
      }
    } catch (error) {
      console.log(error);
    }
  };

  // 仅获取实时流量数据，供自动刷新使用，避免顺带重取用户/通道统计
  const fetchRpmTpmStatistics = async () => {
    try {
      const res = await API.get('/api/analytics/rpm');
      const { success, data } = res.data;
      if (success && data) setRpmTpmStatistics(data);
    } catch (error) {
      console.log(error);
    }
  };

  // 处理时间过滤器变化
  const handleRechargeTimeFilterChange = (event) => {
    const newFilter = event.target.value;
    setRechargeTimeFilter(newFilter);
    fetchRechargeStatistics(newFilter);
  };

  useEffect(() => {
    fetchBasicStatistics();
    fetchRechargeStatistics(rechargeTimeFilter);
  }, []);

  // 首屏基础接口已带回实时流量数据，故挂载时跳过立即刷新，仅启动轮询；用户手动重新开启时才立即刷新
  const realtimeMounted = useRef(false);
  useEffect(() => {
    if (!realtimeAutoRefresh) return;
    if (realtimeMounted.current) fetchRpmTpmStatistics();
    else realtimeMounted.current = true;
    const timer = setInterval(fetchRpmTpmStatistics, 60000);
    return () => clearInterval(timer);
  }, [realtimeAutoRefresh]);

  return (
    <Grid container spacing={gridSpacing}>
      <Grid item lg={2.4} md={4} xs={12}>
        <DataCard
          isLoading={userLoading}
          title={t('analytics_index.totalUserBalance')}
          content={userStatistics?.total_quota || '0'}
          subContent={
            <>
              {t('analytics_index.totalUsedQuota')}：{userStatistics?.total_used_quota || '0'} <br />
              {t('analytics_index.totalRequestCount')}：{(userStatistics?.total_request_count || 0).toLocaleString()} <br />
              {t('analytics_index.totalTokens')}：{renderNumber(userStatistics?.total_tokens || 0)}
            </>
          }
        />
      </Grid>
      <Grid item lg={2.4} md={4} xs={12}>
        <DataCard
          isLoading={userLoading}
          title={t('analytics_index.totalUsers')}
          content={userStatistics?.total_user || '0'}
          subContent={
            <>
              {t('analytics_index.directRegistration')}：{userStatistics?.total_direct_user || '0'} <br />
              {t('analytics_index.invitationRegistration')}：{userStatistics?.total_inviter_user || '0'}
            </>
          }
        />
      </Grid>
      <Grid item lg={2.4} md={4} xs={12}>
        <DataCard
          isLoading={channelLoading}
          title={t('analytics_index.channelCount')}
          content={channelStatistics.total}
          subContent={
            <>
              {t('analytics_index.active')}：{channelStatistics.active} / {t('analytics_index.disabled')}：{channelStatistics.disabled}{' '}
              <br />
              {t('analytics_index.testDisabled')}：{channelStatistics.test_disabled}
            </>
          }
        />
      </Grid>
      <Grid item lg={2.4} md={4} xs={12}>
        <DataCard
          isLoading={rechargeLoading}
          title={t('analytics_index.rechargeStatistics')}
          content={rechargeStatistics.total}
          subContent={
            <>
              {t('analytics_index.redemptionCode')}: {rechargeStatistics.Redemption}
              <br /> {t('analytics_index.order')}: {rechargeStatistics.Oder} / {rechargeStatistics.OderContent}
            </>
          }
          showFilter={true}
          filterValue={rechargeTimeFilter}
          filterOptions={timeFilterOptions}
          onFilterChange={handleRechargeTimeFilterChange}
        />
      </Grid>
      <Grid item lg={2.4} md={4} xs={12}>
        <DataCard
          isLoading={rpmTpmLoading}
          title={t('analytics_index.realTimeTraffic')}
          content={`${rpmTpmStatistics.rpm} RPM`}
          showSwitch={true}
          switchChecked={realtimeAutoRefresh}
          switchLabel={t('analytics_index.autoRefresh')}
          onSwitchChange={() => setRealtimeAutoRefresh((v) => !v)}
          subContent={
            <>
              {t('analytics_index.tpmDescription')}: {rpmTpmStatistics.tpm.toLocaleString()} <br />
              {t('analytics_index.cpmDescription')}: ${rpmTpmStatistics.cpm.toFixed(4)}{' '}
              {siteInfo.PaymentUSDRate ? ` / ¥${(rpmTpmStatistics.cpm * siteInfo.PaymentUSDRate).toFixed(4)}` : ''}
              <br />
              {t('analytics_index.ppmDescription')}: ${rpmTpmStatistics.ppm.toFixed(4)}{' '}
              {siteInfo.PaymentUSDRate ? ` / ¥${(rpmTpmStatistics.ppm * siteInfo.PaymentUSDRate).toFixed(4)}` : ''}
            </>
          }
        />
      </Grid>
    </Grid>
  );
}
