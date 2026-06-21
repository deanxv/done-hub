import PropTypes from 'prop-types';
import { useCallback, useEffect, useRef, useState } from 'react';

import { copy, renderQuota, showError, showInfo, showSuccess } from 'utils/common';
import { API } from 'utils/api';
import { CHANNEL_OPTIONS } from 'constants/ChannelConstants';
import { useTranslation } from 'react-i18next';
import { useBoolean } from 'src/hooks/use-boolean';
import ConfirmDialog from 'ui-component/confirm-dialog';
import EditeModal from './EditModal';
import { usePopover } from 'hooks/use-popover';

import {
  Box,
  Button,
  Checkbox,
  CircularProgress,
  Collapse,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  Grid,
  IconButton,
  InputAdornment,
  LinearProgress,
  Menu,
  MenuItem,
  MenuList,
  Popover,
  Stack,
  Switch,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TablePagination,
  TableRow,
  TextField,
  Tooltip,
  Typography
} from '@mui/material';

import Label from 'ui-component/Label';
// import TableSwitch from 'ui-component/Switch';
import ResponseTimeLabel from './ResponseTimeLabel';
import GroupLabel from './GroupLabel';

import { alpha, styled } from '@mui/material/styles';
import { Icon } from '@iconify/react';
import KeyboardArrowDownIcon from '@mui/icons-material/KeyboardArrowDown';
import KeyboardArrowUpIcon from '@mui/icons-material/KeyboardArrowUp';
import { ChannelCheck } from './ChannelCheck';
import { getPageSize, PAGE_SIZE_OPTIONS, savePageSize } from 'constants';
import { stickyCellSx } from 'ui-component/stickyCellSx';
import KeywordTableHead from 'ui-component/TableHead';

const StyledMenu = styled((props) => (
  <Menu
    elevation={0}
    anchorOrigin={{
      vertical: 'bottom',
      horizontal: 'right'
    }}
    transformOrigin={{
      vertical: 'top',
      horizontal: 'right'
    }}
    {...props}
  />
))(({ theme }) => ({
  '& .MuiPaper-root': {
    borderRadius: 6,
    marginTop: theme.spacing(1),
    minWidth: 180,
    color: theme.palette.mode === 'light' ? 'rgb(55, 65, 81)' : theme.palette.grey[300],
    boxShadow:
      'rgb(255, 255, 255) 0px 0px 0px 0px, rgba(0, 0, 0, 0.05) 0px 0px 0px 1px, rgba(0, 0, 0, 0.1) 0px 10px 15px -3px, rgba(0, 0, 0, 0.05) 0px 4px 6px -2px',
    '& .MuiMenu-list': {
      padding: '4px 0'
    },
    '& .MuiMenuItem-root': {
      '& .MuiSvgIcon-root': {
        fontSize: 18,
        color: theme.palette.text.secondary,
        marginRight: theme.spacing(1.5)
      },
      '&:active': {
        backgroundColor: alpha(theme.palette.primary.main, theme.palette.action.selectedOpacity)
      }
    }
  }
}));

function statusInfo(t, status) {
  switch (status) {
    case 1:
      return t('channel_index.enabled');
    case 2:
      return t('channel_row.manual');
    case 3:
      return t('channel_row.auto');
    default:
      return t('common.unknown');
  }
}

export default function ChannelTableRow({
  item,
  manageChannel,
  onRefresh,
  onTagStatsRefresh,
  groupOptions,
  modelOptions,
  prices,
  selected,
  onSelect,
  tags
}) {
  const { t } = useTranslation();
  const popover = usePopover();
  const confirmDelete = useBoolean();
  const check = useBoolean();
  const updateBalanceOption = useBoolean();

  const [openTest, setOpenTest] = useState(false);
  // const [openDelete, setOpenDelete] = useState(false);
  const [openCheck, setOpenCheck] = useState(false);
  const [statusSwitch, setStatusSwitch] = useState(item.status);
  const [deleting, setDeleting] = useState(false);

  const [priority, setPriority] = useState(item.priority);
  const [weight, setWeight] = useState(item.weight);
  const [costRatio, setCostRatio] = useState(item.cost_ratio);
  const tagDeleteConfirm = useBoolean();
  const quickEdit = useBoolean();
  // 子渠道完整编辑：复用主 EditModal（isTag=false），可单独编辑某个标签成员的全部配置
  const subEdit = useBoolean();
  const [subEditChannelId, setSubEditChannelId] = useState(0);
  const [totalTagChannels, setTotalTagChannels] = useState(0);
  const [isTagChannelsLoading, setIsTagChannelsLoading] = useState(false);
  const [tagChannels, setTagChannels] = useState([]);
  const [selectedChannels, setSelectedChannels] = useState([]);
  const [currentTestingChannel, setCurrentTestingChannel] = useState(null);
  const [tagPage, setTagPage] = useState(0);
  const [tagRowsPerPage, setTagRowsPerPage] = useState(() => getPageSize('channelTag'));
  const [tagOrder, setTagOrder] = useState('asc');
  const [tagOrderBy, setTagOrderBy] = useState('');
  const tagModelPopover = usePopover();

  const batchConfirm = useBoolean();

  const tagStatusConfirm = useBoolean();
  const [statusChangeAction, setStatusChangeAction] = useState('');

  const [responseTimeData, setResponseTimeData] = useState({
    test_time: item.test_time,
    response_time: item.response_time
  });
  const [itemBalance, setItemBalance] = useState(item.balance);

  const [openRow, setOpenRow] = useState(false);

  // 展开面板用 sticky 钉在主表格滚动视口内，宽度跟随主滚动容器(.MuiTableContainer-root)的 clientWidth。
  // 否则展开行 colSpan 会撑到主表格的 scrollWidth(~1400)，子表格(minWidth 1180)在其中根本不溢出，
  // 其「操作」列的 position:sticky right:0 便无处可钉 —— 这正是子表格操作列无法冻结的根因。
  const expandPanelRef = useRef(null);
  const [expandViewportWidth, setExpandViewportWidth] = useState(null);
  useEffect(() => {
    if (!openRow) return undefined;
    const node = expandPanelRef.current;
    if (!node) return undefined;
    // closest 只向上找祖先，主表格容器才会命中（子表格自己的 TableContainer 是后代，不会误伤）
    const scroller = node.closest('.MuiTableContainer-root');
    if (!scroller) return undefined;
    const update = () => setExpandViewportWidth(scroller.clientWidth);
    update();
    const ro = new ResizeObserver(update);
    ro.observe(scroller);
    return () => ro.disconnect();
  }, [openRow]);

  let modelMap = [];
  modelMap = item.models.split(',');
  modelMap.sort();

  // 标签组统计（来自 _all）：代表行据此显示组状态汇总与「混合」徽标
  const tagStat = item.tag ? tags?.find((tg) => tg.tag === item.tag) : null;
  const isMixedType = !!tagStat && tagStat.type_count > 1;
  const isMixedGroup = !!tagStat && tagStat.group_count > 1;

  const fetchTagChannels = useCallback(async () => {
    if (!item.tag) return;

    try {
      setIsTagChannelsLoading(true);
      const response = await API.get(`/api/channel_tag/${item.tag}/list`, {
        params: {
          page: tagPage + 1,
          size: tagRowsPerPage,
          order: tagOrderBy ? (tagOrder === 'desc' ? '-' + tagOrderBy : tagOrderBy) : undefined
        }
      });
      if (response.data.success) {
        const result = response.data.data || {};
        const list = result.data || [];
        setTagChannels(list);
        setTotalTagChannels(result.total_count || 0);
        // 记录每个子渠道的优先级/权重/成本倍率原值，用于行内编辑的「无变化则不提交」判断，避免失焦时无谓请求与提示
        tagChannelOriginalsRef.current = Object.fromEntries(
          list.map((c) => [c.id, { priority: c.priority, weight: c.weight, cost_ratio: c.cost_ratio }])
        );
      } else {
        showError(t('channel_row.getTagChannelsError', { message: response.data.message }));
      }
    } catch (error) {
      showError(t('channel_row.getTagChannelsErrorTip', { message: error.message }));
    } finally {
      setIsTagChannelsLoading(false);
    }
  }, [item.tag, tagPage, tagRowsPerPage, tagOrder, tagOrderBy, t]);

  const handleChangeTagPage = (event, newPage) => {
    setTagPage(newPage);
  };

  // 子表格表头排序：服务端排序（GetChannelsTagList 走 allowedChannelOrderFields），切换排序回到首页
  const handleTagSort = (event, id) => {
    const isAsc = tagOrderBy === id && tagOrder === 'asc';
    setTagOrder(isAsc ? 'desc' : 'asc');
    setTagOrderBy(id);
    setTagPage(0);
  };

  const handleChangeTagRowsPerPage = (event) => {
    const newRowsPerPage = parseInt(event.target.value, 10);
    setTagRowsPerPage(newRowsPerPage);
    setTagPage(0);
    savePageSize('channelTag', newRowsPerPage);
  };

  const handleToggleChannel = (channelId) => {
    setSelectedChannels((prev) => (prev.includes(channelId) ? prev.filter((id) => id !== channelId) : [...prev, channelId]));
  };

  const handleToggleAll = () => {
    if (selectedChannels.length === tagChannels.length) {
      setSelectedChannels([]);
    } else {
      setSelectedChannels(tagChannels.map((channel) => channel.id));
    }
  };

  const handleTagChannelStatus = async (channelId, currentStatus) => {
    const newStatus = currentStatus === 1 ? 2 : 1;
    const { success } = await manageChannel(channelId, 'status', newStatus);
    if (success) {
      // 更新本地状态
      setTagChannels((prev) =>
        prev.map((channel) =>
          channel.id === channelId
            ? {
                ...channel,
                status: newStatus
              }
            : channel
        )
      );
      // 刷新标签聚合统计，使代表行「共 N · X 启用 · Y 禁用」即时更新
      onTagStatsRefresh?.();
    }
  };

  // 处理子渠道的成本倍率变更（成本倍率为逐行字段，可对组内各渠道单独设置）
  const handleTagChannelCostRatioChange = (channelId, value) => {
    setTagChannels((prev) => prev.map((c) => (c.id === channelId ? { ...c, cost_ratio: value } : c)));
  };

  // 处理子渠道的优先级变更
  const handleTagChannelPriorityChange = (channelId, value) => {
    // 更新本地UI状态
    setTagChannels((prev) => prev.map((c) => (c.id === channelId ? { ...c, priority: value } : c)));
  };

  // 处理子渠道的权重变更（权重为逐行字段，可对组内各渠道单独设置）
  const handleTagChannelWeightChange = (channelId, value) => {
    setTagChannels((prev) => prev.map((c) => (c.id === channelId ? { ...c, weight: value } : c)));
  };

  // 代表行行内编辑（优先级/权重/成本倍率）在标签模式下会批量覆盖整组，提交前需二次确认。
  // 用 useBoolean 单独控制显隐，payload 不在确认/取消时清空——否则关闭过渡动画那一帧会读到空值，导致文案里的数值闪烁消失
  const [tagFieldConfirm, setTagFieldConfirm] = useState(null);
  const tagFieldConfirmOpen = useBoolean();

  // 主行优先级提交：回车 / 失焦 / 点击 ✓ 共用一份逻辑，ref 去重避免同一次操作触发重复请求
  const committingPriorityRef = useRef(false);
  const commitMainPriority = () => {
    if (committingPriorityRef.current) return;
    if (priority === item.priority) return;
    const isTag = !!item.tag;
    const exec = () => {
      committingPriorityRef.current = true;
      const channelId = isTag ? item.tag : item.id;
      manageChannel(channelId, 'priority', priority, isTag)
        .then(({ success }) => {
          if (success) {
            item.priority = priority;
            showInfo(t('channel_row.priorityUpdateSuccess'));
          }
        })
        .catch((error) => {
          showError(t('channel_row.priorityUpdateError', { message: error.message }));
        })
        .finally(() => {
          committingPriorityRef.current = false;
        });
    };
    // 标签代表行：批量覆盖整组，先二次确认
    if (isTag) {
      setTagFieldConfirm({ field: t('channel_index.priority'), value: priority, exec });
      tagFieldConfirmOpen.onTrue();
      return;
    }
    exec();
  };

  // 权重提交：标签代表行作用于整组、普通行作用于单渠道，由 item.tag 区分；回车 / 失焦 / 点击 ✓ 共用，ref 去重
  const committingWeightRef = useRef(false);
  const commitWeight = () => {
    if (committingWeightRef.current) return;
    if (weight === item.weight) return;
    const isTag = !!item.tag;
    const exec = () => {
      committingWeightRef.current = true;
      manageChannel(isTag ? item.tag : item.id, 'weight', weight, isTag)
        .then(({ success }) => {
          if (success) {
            item.weight = weight;
            showInfo(t('channel_row.weightUpdateSuccess'));
          }
        })
        .catch((error) => {
          showError(t('channel_row.weightUpdateError', { message: error.message }));
        })
        .finally(() => {
          committingWeightRef.current = false;
        });
    };
    if (isTag) {
      setTagFieldConfirm({ field: t('channel_index.weight'), value: weight, exec });
      tagFieldConfirmOpen.onTrue();
      return;
    }
    exec();
  };

  // 成本倍率提交：标签代表行作用于整组、普通行作用于单渠道，由 item.tag 区分
  const committingCostRatioRef = useRef(false);
  const commitCostRatio = () => {
    if (committingCostRatioRef.current) return;
    if (costRatio === item.cost_ratio) return;
    const isTag = !!item.tag;
    const exec = () => {
      committingCostRatioRef.current = true;
      manageChannel(isTag ? item.tag : item.id, 'cost_ratio', costRatio, isTag)
        .then(({ success }) => {
          if (success) {
            item.cost_ratio = costRatio;
            showInfo(t('channel_row.costRatioUpdateSuccess'));
          }
        })
        .catch((error) => {
          showError(t('channel_row.costRatioUpdateError', { message: error.message }));
        })
        .finally(() => {
          committingCostRatioRef.current = false;
        });
    };
    if (isTag) {
      setTagFieldConfirm({ field: t('channel_index.costRatio'), value: costRatio, exec });
      tagFieldConfirmOpen.onTrue();
      return;
    }
    exec();
  };

  // 子渠道行内编辑原值快照（priority/weight），用于「无变化不提交」判断
  const tagChannelOriginalsRef = useRef({});

  // 子渠道优先级提交：回车 / 失焦 / 点击共用，按渠道 id 去重；值未变则跳过
  const committingTagPriorityRef = useRef({});
  const commitTagChannelPriority = (channel) => {
    if (committingTagPriorityRef.current[channel.id]) return;
    const orig = tagChannelOriginalsRef.current[channel.id];
    if (orig && channel.priority === orig.priority) return;
    committingTagPriorityRef.current[channel.id] = true;
    manageChannel(channel.id, 'priority', channel.priority)
      .then(({ success }) => {
        if (success) {
          if (orig) orig.priority = channel.priority;
          showSuccess(t('channel_row.priorityUpdateSuccess'));
        }
      })
      .catch((error) => {
        showError(t('channel_row.priorityUpdateError', { message: error.message }));
      })
      .finally(() => {
        committingTagPriorityRef.current[channel.id] = false;
      });
  };

  // 子渠道权重提交：回车 / 失焦 / 点击共用，按渠道 id 去重；值未变则跳过
  const committingTagWeightRef = useRef({});
  const commitTagChannelWeight = (channel) => {
    if (committingTagWeightRef.current[channel.id]) return;
    const orig = tagChannelOriginalsRef.current[channel.id];
    if (orig && channel.weight === orig.weight) return;
    committingTagWeightRef.current[channel.id] = true;
    manageChannel(channel.id, 'weight', channel.weight)
      .then(({ success }) => {
        if (success) {
          if (orig) orig.weight = channel.weight;
          showSuccess(t('channel_row.weightUpdateSuccess'));
        }
      })
      .catch((error) => {
        showError(t('channel_row.weightUpdateError', { message: error.message }));
      })
      .finally(() => {
        committingTagWeightRef.current[channel.id] = false;
      });
  };

  // 子渠道成本倍率提交：回车 / 失焦 / 点击共用，按渠道 id 去重；值未变则跳过
  const committingTagCostRatioRef = useRef({});
  const commitTagChannelCostRatio = (channel) => {
    if (committingTagCostRatioRef.current[channel.id]) return;
    const orig = tagChannelOriginalsRef.current[channel.id];
    if (orig && channel.cost_ratio === orig.cost_ratio) return;
    committingTagCostRatioRef.current[channel.id] = true;
    manageChannel(channel.id, 'cost_ratio', channel.cost_ratio)
      .then(({ success }) => {
        if (success) {
          if (orig) orig.cost_ratio = channel.cost_ratio;
          showSuccess(t('channel_row.costRatioUpdateSuccess'));
        }
      })
      .catch((error) => {
        showError(t('channel_row.costRatioUpdateError', { message: error.message }));
      })
      .finally(() => {
        committingTagCostRatioRef.current[channel.id] = false;
      });
  };

  // 服务端分页：翻页或修改每页条数后清空选择，保证"全选 / 批量删除"范围始终等于当前可见页
  useEffect(() => {
    setSelectedChannels([]);
  }, [tagPage, tagRowsPerPage]);

  const handleTagChannelTest = async (channel) => {
    const models = channel.models.split(',');
    if (models.length === 1) {
      // 如果只有一个模型，直接测速
      const testModel = models[0];
      const { success, time } = await manageChannel(channel.id, 'test', testModel);
      if (success) {
        showInfo(t('channel_row.modelTestSuccess', { channel: channel.name, model: testModel, time: time.toFixed(2) }));
        // 更新本地状态
        setTagChannels((prev) =>
          prev.map((c) =>
            c.id === channel.id
              ? {
                  ...c,
                  test_time: Date.now() / 1000,
                  response_time: time * 1000
                }
              : c
          )
        );
      }
    } else {
      // 多个模型：仅记录当前渠道，弹窗由 onClick 带 event 锚点打开（避免无锚点重复弹出/闪烁）
      setCurrentTestingChannel(channel);
    }
  };

  const handleBatchDelete = async () => {
    if (!selectedChannels.length) {
      showError(t('channel_row.batchAddIDRequired'));
      return;
    }

    batchConfirm.onTrue();
  };

  const executeBatchDelete = async () => {
    try {
      // 这里需要实现批量删除的API调用
      const { success, message } = await manageChannel(0, 'batch_delete', selectedChannels, false);
      if (success) {
        showInfo(t('channel_row.batchDeleteSuccess'));
        setSelectedChannels([]);
        fetchTagChannels(); // 重新获取数据
        onTagStatsRefresh?.(); // 刷新标签聚合（总数/启用数）
        onRefresh(false); // 刷新父组件数据
      } else {
        showError(t('channel_row.batchDeleteError', { message }));
      }
    } catch (error) {
      showError(t('channel_row.batchDeleteErrorTip', { message: error.message }));
    }
  };

  useEffect(() => {
    if (openRow && item.tag) {
      fetchTagChannels();
    }
  }, [openRow, item.tag, fetchTagChannels]);

  const handleTestModel = (event) => {
    setOpenTest(event.currentTarget);
  };

  const handleDeleteRow = useCallback(async () => {
    if (deleting) return;

    setDeleting(true);
    try {
      await manageChannel(item.id, 'delete', '');
    } finally {
      setDeleting(false);
    }
  }, [manageChannel, item.id, deleting]);

  const handleStatus = async () => {
    const switchVlue = statusSwitch === 1 ? 2 : 1;
    const { success } = await manageChannel(item.id, 'status', switchVlue);
    if (success) {
      setStatusSwitch(switchVlue);
    }
  };

  const handleResponseTime = async (modelName) => {
    setOpenTest(null);

    if (typeof modelName !== 'string') {
      modelName = item.test_model;
    }

    if (modelName == '') {
      showError(t('channel_row.modelTestTip'));
      return;
    }
    const { success, time } = await manageChannel(item.id, 'test', modelName);
    if (success) {
      setResponseTimeData({ test_time: Date.now() / 1000, response_time: time * 1000 });
      showInfo(t('channel_row.modelTestSuccess', { channel: item.name, model: modelName, time: time.toFixed(2) }));
    }
  };

  const updateChannelBalance = async () => {
    try {
      const res = await API.get(`/api/channel/update_balance/${item.id}`);
      const { success, message, balance } = res.data;
      if (success) {
        setItemBalance(balance);

        showInfo(t('channel_row.updateOk'));
      } else {
        showError(message);
      }
    } catch (error) {}
  };

  useEffect(() => {
    setStatusSwitch(item.status);
    setPriority(item.priority);
    setWeight(item.weight);
    setCostRatio(item.cost_ratio);
    setItemBalance(item.balance);
    setResponseTimeData({ test_time: item.test_time, response_time: item.response_time });
  }, [item]);

  return (
    <>
      <TableRow tabIndex={item.id}>
        <TableCell sx={{ minWidth: 50, textAlign: 'center' }}>
          {!item.tag && (
            <Checkbox
              checked={selected}
              onChange={onSelect}
              inputProps={{
                'aria-labelledby': `channel-${item.id}`
              }}
            />
          )}
          {item.tag && (
            <Label color="primary" variant="soft" sx={{ fontSize: '0.75rem', fontWeight: 600 }}>
              {t('channel_row.tag')}
            </Label>
          )}
        </TableCell>
        <TableCell sx={{ minWidth: 80, textAlign: 'center' }}>
          {!item.tag && (
            <Typography variant="body2" id={`channel-${item.id}`}>
              {item.id}
            </Typography>
          )}
          {item.tag && (
            <Tooltip title={openRow ? t('channel_row.collapseGroup') : t('channel_row.expandGroup')} placement="top" arrow>
              <IconButton aria-label="expand row" size="small" onClick={() => setOpenRow(!openRow)}>
                {openRow ? <KeyboardArrowUpIcon /> : <KeyboardArrowDownIcon />}
              </IconButton>
            </Tooltip>
          )}
        </TableCell>
        <TableCell sx={{ minWidth: 100, maxWidth: item.tag ? 400 : 220, overflow: 'hidden' }}>
          {item.tag ? (
            <Tooltip title={openRow ? t('channel_row.collapseGroup') : t('channel_row.expandGroup')} placement="top" arrow>
              <Stack
                direction="row"
                alignItems="center"
                spacing={0.75}
                onClick={() => setOpenRow(!openRow)}
                sx={{ cursor: 'pointer', minWidth: 0, '&:hover .tag-name': { textDecoration: 'underline' } }}
              >
                <Typography className="tag-name" variant="subtitle2" noWrap sx={{ color: 'primary.main', fontWeight: 600, minWidth: 0 }}>
                  {item.tag}
                </Typography>
                {tagStat && (
                  <Typography variant="caption" noWrap sx={{ color: 'text.secondary', flexShrink: 0 }}>
                    {t('channel_row.groupCount', { count: tagStat.count })}
                    {' · '}
                    <Box component="span" sx={{ color: 'success.main', fontWeight: 600 }}>
                      {t('channel_row.groupEnabled', { count: tagStat.enabled })}
                    </Box>
                    {tagStat.count - tagStat.enabled > 0 && (
                      <>
                        {' · '}
                        <Box component="span" sx={{ color: 'error.main', fontWeight: 600 }}>
                          {t('channel_row.groupDisabled', { count: tagStat.count - tagStat.enabled })}
                        </Box>
                      </>
                    )}
                  </Typography>
                )}
              </Stack>
            </Tooltip>
          ) : (
            item.name
          )}
        </TableCell>

        <TableCell>
          {isMixedGroup ? (
            <Tooltip title={t('channel_row.mixedTip')} placement="top" arrow>
              <Label color="warning" variant="soft">
                {t('channel_row.mixed')}
              </Label>
            </Tooltip>
          ) : (
            <GroupLabel group={item.group} />
          )}
        </TableCell>

        <TableCell>
          {isMixedType ? (
            <Tooltip title={t('channel_row.mixedTip')} placement="top" arrow>
              <Label color="warning" variant="soft">
                {t('channel_row.mixed')}
              </Label>
            </Tooltip>
          ) : !CHANNEL_OPTIONS[item.type] ? (
            <Label color="error" variant="outlined">
              {t('common.unknown')}
            </Label>
          ) : (
            <Label color={CHANNEL_OPTIONS[item.type].color} variant="outlined">
              {CHANNEL_OPTIONS[item.type].text}
            </Label>
          )}
        </TableCell>

        <TableCell align="center" sx={{ minWidth: 90 }}>
          {!item.tag && (
            <Stack direction="column" alignItems="center" spacing={0.5}>
              <Switch checked={statusSwitch === 1} onChange={handleStatus} size="small" />
              <Typography
                variant="caption"
                sx={{
                  fontWeight: statusSwitch === 1 ? 600 : 400,
                  color: statusSwitch === 1 ? 'success.main' : 'text.secondary'
                }}
              >
                {statusInfo(t, statusSwitch)}
              </Typography>
            </Stack>
          )}
          {item.tag && (
            <Stack direction="row" spacing={1} justifyContent="center">
              <Tooltip title={t('channel_row.enableAllChannels')} placement="top">
                <IconButton
                  size="small"
                  onClick={() => {
                    setStatusChangeAction('enable');
                    tagStatusConfirm.onTrue();
                  }}
                  sx={{ color: 'success.main' }}
                >
                  <Icon icon="mdi:power" />
                </IconButton>
              </Tooltip>
              <Tooltip title={t('channel_row.disableAllChannels')} placement="top">
                <IconButton
                  size="small"
                  onClick={() => {
                    setStatusChangeAction('disable');
                    tagStatusConfirm.onTrue();
                  }}
                  sx={{ color: 'error.main' }}
                >
                  <Icon icon="mdi:power-off" />
                </IconButton>
              </Tooltip>
            </Stack>
          )}
        </TableCell>

        <TableCell sx={{ minWidth: 90, textAlign: 'center' }}>
          {!item.tag && (
            <ResponseTimeLabel
              test_time={responseTimeData.test_time}
              response_time={responseTimeData.response_time}
              handle_action={handleResponseTime}
            />
          )}
          {item.tag && tagStat && (
            <Stack spacing={0.25} alignItems="center">
              <Typography variant="caption" sx={{ color: 'text.secondary' }}>
                {t('channel_row.avgLabel')}
              </Typography>
              <ResponseTimeLabel test_time={0} response_time={Math.round(tagStat.response_time || 0)} />
            </Stack>
          )}
        </TableCell>
        {/* <TableCell>

        </TableCell> */}
        <TableCell>
          {!item.tag && (
            <Stack spacing={0.5} alignItems="center">
              <Typography variant="body1">{renderQuota(item.used_quota)}</Typography>
              <Typography
                variant="caption"
                sx={{
                  color: 'success.main',
                  fontWeight: 600,
                  cursor: 'pointer',
                  '&:hover': { textDecoration: 'underline' }
                }}
                onClick={updateChannelBalance}
              >
                {renderBalance(item.type, itemBalance)}
              </Typography>
            </Stack>
          )}
          {item.tag && tagStat && (
            <Stack spacing={0.25} alignItems="center">
              <Typography variant="caption" sx={{ color: 'text.secondary' }}>
                {t('channel_row.sumLabel')}
              </Typography>
              <Typography variant="body1">{renderQuota(tagStat.used_quota)}</Typography>
              <Typography variant="caption" sx={{ color: 'success.main', fontWeight: 600 }}>
                {renderBalance(item.type, tagStat.balance || 0)}
              </Typography>
            </Stack>
          )}
        </TableCell>

        <TableCell>
          <GroupInlineEditor
            label={item.tag ? t('channel_row.groupPriorityLabel') : t('channel_index.priority')}
            tip={item.tag ? t('channel_row.groupPriorityTip') : ''}
            value={priority}
            min="0"
            onChange={(v) => setPriority(v)}
            onCommit={commitMainPriority}
            disabled={priority === item.priority}
          />
        </TableCell>

        <TableCell>
          <GroupInlineEditor
            label={item.tag ? t('channel_row.groupWeightLabel') : t('channel_index.weight')}
            tip={item.tag ? t('channel_row.groupWeightTip') : ''}
            value={weight}
            min="1"
            onChange={(v) => setWeight(v)}
            onCommit={commitWeight}
            disabled={weight === item.weight}
          />
        </TableCell>

        <TableCell>
          <GroupInlineEditor
            label={item.tag ? t('channel_row.groupCostRatioLabel') : t('channel_index.costRatio')}
            tip={item.tag ? t('channel_row.groupCostRatioTip') : ''}
            value={costRatio}
            min="0"
            step="0.1"
            onChange={(v) => setCostRatio(v)}
            onCommit={commitCostRatio}
            disabled={costRatio === item.cost_ratio}
          />
        </TableCell>

        <TableCell sx={stickyCellSx}>
          <Stack direction="row" justifyContent="right" alignItems="center" spacing={1}>
            {!item.tag && (
              <IconButton
                size="small"
                onClick={handleTestModel}
                aria-controls={openTest ? 'test-model-menu' : undefined}
                aria-haspopup="true"
                aria-expanded={openTest ? 'true' : undefined}
                sx={{ color: 'info.main' }}
              >
                <Icon icon="mdi:speedometer" />
              </IconButton>
            )}

            <Tooltip title={t('common.edit')} placement="top" arrow>
              <IconButton onClick={quickEdit.onTrue} size="small">
                <Icon icon="solar:pen-bold" />
              </IconButton>
            </Tooltip>
            {!item.tag && (
              <IconButton onClick={popover.onOpen} size="small">
                <Icon icon="eva:more-vertical-fill" />
              </IconButton>
            )}

            {item.tag && (
              <Tooltip title={t('channel_row.deleteTagAndChannels')} placement="top">
                <IconButton sx={{ color: 'error.main' }} onClick={tagDeleteConfirm.onTrue} size="small">
                  <Icon icon="solar:trash-bin-trash-bold" />
                </IconButton>
              </Tooltip>
            )}
          </Stack>
        </TableCell>
      </TableRow>

      <Popover
        open={popover.open}
        anchorEl={popover.anchorEl}
        onClose={() => {
          popover.onClose();
          // 如果在关闭后没有进一步操作，重置当前渠道
          if (!check.value && !confirmDelete.value && !updateBalanceOption.value) {
            setCurrentTestingChannel(null);
          }
        }}
        anchorOrigin={{ vertical: 'top', horizontal: 'left' }}
        transformOrigin={{ vertical: 'top', horizontal: 'right' }}
        PaperProps={{
          sx: { minWidth: 140 }
        }}
      >
        <MenuItem
          onClick={() => {
            popover.onClose();
            manageChannel(currentTestingChannel ? currentTestingChannel.id : item.id, 'copy');
          }}
        >
          <Icon icon="solar:copy-bold-duotone" style={{ marginRight: '16px' }} />
          {t('token_index.copy')}
        </MenuItem>
        <MenuItem
          onClick={() => {
            setOpenCheck(true);
            popover.onClose();
          }}
        >
          <Icon icon="solar:checklist-minimalistic-bold" style={{ marginRight: '16px' }} />
          {t('channel_row.check')}
        </MenuItem>

        {CHANNEL_OPTIONS[currentTestingChannel ? currentTestingChannel?.type : item.type]?.url && (
          <MenuItem
            onClick={() => {
              popover.onClose();
              window.open(CHANNEL_OPTIONS[currentTestingChannel ? currentTestingChannel?.type : item.type].url);
            }}
          >
            <Icon icon="solar:global-line-duotone" style={{ marginRight: '16px' }} />
            {t('channel_row.channelWeb')}
          </MenuItem>
        )}

        {currentTestingChannel && (
          <MenuItem
            onClick={() => {
              popover.onClose();
              manageChannel(currentTestingChannel.id, 'delete_tag', '');
            }}
            sx={{ color: 'error.main' }}
          >
            <Icon icon="solar:trash-bin-trash-bold-duotone" style={{ marginRight: '16px' }} />
            {t('channel_row.delTag')}
          </MenuItem>
        )}
        <MenuItem
          onClick={() => {
            popover.onClose();
            confirmDelete.onTrue();
          }}
          sx={{ color: 'error.main' }}
        >
          <Icon icon="solar:trash-bin-trash-bold-duotone" style={{ marginRight: '16px' }} />
          {t('common.delete')}
        </MenuItem>
      </Popover>

      <StyledMenu
        id="test-model-menu"
        MenuListProps={{
          'aria-labelledby': 'test-model-button'
        }}
        anchorEl={openTest}
        open={!!openTest}
        onClose={() => {
          setOpenTest(null);
        }}
      >
        {modelMap.map((model) => (
          <MenuItem
            key={'test_model-' + model}
            onClick={() => {
              handleResponseTime(model);
            }}
          >
            {model}
          </MenuItem>
        ))}
      </StyledMenu>
      <TableRow
        sx={{
          '&:hover': {
            backgroundColor: 'transparent !important'
          },
          '&.MuiTableRow-hover:hover': {
            backgroundColor: 'transparent !important'
          }
        }}
      >
        <TableCell style={{ paddingBottom: 0, paddingTop: 0, paddingLeft: 0, paddingRight: 0, textAlign: 'left' }} colSpan={20}>
          <Collapse in={openRow} timeout="auto" unmountOnExit>
            {/* sticky 面板：钉在主滚动视口内、宽度=主容器 clientWidth，使内部子表格成为独立的视口宽横向滚动区 */}
            <Box
              ref={expandPanelRef}
              sx={{ position: 'sticky', left: 0, width: expandViewportWidth ? `${expandViewportWidth}px` : '100%' }}
            >
              <Grid container spacing={1} sx={{ py: 2, mx: 0, width: '100%' }}>
                <Grid item xs={12}>
                  <Box
                    sx={{
                      display: 'flex',
                      flexWrap: 'wrap',
                      gap: '10px',
                      m: 1,
                      p: 0,
                      bgcolor: 'background.neutral',
                      borderRadius: 1,
                      alignItems: 'center'
                    }}
                  >
                    <Typography
                      variant="body1"
                      component="div"
                      sx={{
                        fontWeight: 600,
                        color: 'text.secondary',
                        // mr: 1,
                        display: 'flex',
                        alignItems: 'center'
                      }}
                    >
                      <Icon icon="mdi:cube-outline" sx={{ mr: 0.5 }} /> {t('channel_row.canModels')}
                    </Typography>
                    {modelMap.map((model) => (
                      <Label
                        variant="soft"
                        color="primary"
                        key={model}
                        sx={{
                          // py: 0.75,
                          // px: 1.5,
                          fontSize: '0.75rem',
                          cursor: 'pointer'
                          // '&:hover': { opacity: 0.8 }
                        }}
                        onClick={() => {
                          copy(model, t('channel_index.modelName'));
                        }}
                      >
                        {model}
                      </Label>
                    ))}
                  </Box>
                </Grid>

                {item.test_model && (
                  <Grid item xs={12}>
                    <Box
                      sx={{
                        display: 'flex',
                        flexWrap: 'wrap',
                        gap: '10px',
                        m: 1,
                        px: 1,
                        py: 0.5,
                        bgcolor: 'background.neutral',
                        borderRadius: 1,
                        alignItems: 'center'
                      }}
                    >
                      <Typography
                        variant="body1"
                        component="div"
                        sx={{
                          fontWeight: 600,
                          color: 'text.secondary',
                          display: 'flex',
                          alignItems: 'center'
                        }}
                      >
                        <Icon icon="mdi:speedometer" sx={{ mr: 0.5 }} /> {t('channel_row.testModels') + ':'}
                      </Typography>
                      <Label
                        variant="soft"
                        color="info"
                        key={item.test_model}
                        sx={{ fontSize: '0.75rem', cursor: 'pointer' }}
                        onClick={() => {
                          copy(item.test_model, t('channel_row.testModels'));
                        }}
                      >
                        {item.test_model}
                      </Label>
                    </Box>
                  </Grid>
                )}

                {item.proxy && (
                  <Grid item xs={12}>
                    <Box
                      sx={{
                        display: 'flex',
                        flexWrap: 'wrap',
                        gap: '10px',
                        m: 1,
                        px: 1,
                        py: 0.5,
                        bgcolor: 'background.neutral',
                        borderRadius: 1,
                        alignItems: 'center'
                      }}
                    >
                      <Typography
                        variant="body1"
                        component="div"
                        sx={{
                          fontWeight: 600,
                          color: 'text.secondary',
                          display: 'flex',
                          alignItems: 'center'
                        }}
                      >
                        <Icon icon="mdi:web" sx={{ mr: 0.5 }} /> {t('channel_row.proxy')}
                      </Typography>
                      <Typography variant="body2">{item.proxy}</Typography>
                    </Box>
                  </Grid>
                )}

                {item.other && (
                  <Grid item xs={12}>
                    <Box
                      sx={{
                        display: 'flex',
                        flexWrap: 'wrap',
                        gap: '10px',
                        m: 1,
                        px: 1,
                        py: 0.5,
                        bgcolor: 'background.neutral',
                        borderRadius: 1,
                        alignItems: 'center'
                      }}
                    >
                      <Typography
                        variant="body1"
                        component="div"
                        sx={{
                          fontWeight: 600,
                          color: 'text.secondary',
                          display: 'flex',
                          alignItems: 'center'
                        }}
                      >
                        <Icon icon="mdi:cog-outline" sx={{ mr: 0.5 }} /> {t('channel_row.otherArg')}
                      </Typography>
                      <Label
                        variant="soft"
                        color="default"
                        key={item.other}
                        sx={{ fontSize: '0.75rem', cursor: 'pointer' }}
                        onClick={() => {
                          copy(item.other, t('channel_row.otherArg'));
                        }}
                      >
                        {item.other}
                      </Label>
                    </Box>
                  </Grid>
                )}
                {item.tag && (
                  <Grid item xs={12}>
                    {/* 面板已被 sticky 钉在视口内，子表格在此独立横向滚动；不再加横向 margin，
                      让子表格右缘与面板右缘（即主表格「操作」列冻结右缘）对齐 */}
                    <Box sx={{ mt: 2, mb: 1 }}>
                      <Stack direction="row" justifyContent="space-between" alignItems="center" mb={2}>
                        <Stack direction="row" alignItems="center" spacing={1}>
                          <Typography
                            variant="subtitle1"
                            fontWeight="bold"
                            sx={{
                              borderLeft: '3px solid',
                              borderColor: 'primary.main',
                              pl: 1.5,
                              py: 0.5
                            }}
                          >
                            {t('channel_row.tagChannelList')} ({totalTagChannels})
                          </Typography>
                          <Tooltip title={t('channel_row.refreshList')} placement="top">
                            <IconButton size="small" color="primary" disabled={isTagChannelsLoading} onClick={() => fetchTagChannels()}>
                              <Icon icon="mdi:refresh" width={18} height={18} />
                            </IconButton>
                          </Tooltip>
                        </Stack>

                        {selectedChannels.length > 0 && (
                          <Button
                            variant="contained"
                            color="error"
                            startIcon={<Icon icon="solar:trash-bin-trash-bold" />}
                            onClick={handleBatchDelete}
                            size="small"
                          >
                            {t('channel_row.batchDelete')} ({selectedChannels.length})
                          </Button>
                        )}
                      </Stack>

                      {tagChannels.length === 0 && isTagChannelsLoading ? (
                        <Box sx={{ display: 'flex', justifyContent: 'center', py: 3 }}>
                          <CircularProgress size={24} />
                        </Box>
                      ) : tagChannels.length === 0 ? (
                        <Typography variant="body2" sx={{ py: 2, textAlign: 'center', color: 'text.secondary' }}>
                          {t('channel_row.noTagChannels')}
                        </Typography>
                      ) : (
                        <Box>
                          {/* 刷新/排序时保持表格挂载，仅在顶部叠加进度条，避免内容被替换为居中 spinner 导致的高度塌陷与「下滑闪烁」 */}
                          <Box
                            sx={{
                              position: 'relative',
                              border: '1px solid',
                              borderColor: 'divider',
                              borderRadius: 1,
                              overflow: 'hidden',
                              boxShadow: '0 0 8px rgba(0,0,0,0.05)'
                            }}
                          >
                            {isTagChannelsLoading && (
                              <LinearProgress sx={{ position: 'absolute', top: 0, left: 0, right: 0, zIndex: 3, height: 2 }} />
                            )}
                            {/* 用原生滚动的 TableContainer（与主表格一致），保证「操作」列 position:sticky 冻结生效；
                              PerfectScrollbar 的 overflow:hidden + JS 滚动会让 sticky 失效，导致操作列无法冻结 */}
                            <TableContainer sx={{ maxHeight: 400 }}>
                              <Table size="small" sx={{ minWidth: 1180, '& .MuiTableCell-root': { py: 1, px: 1.5 } }}>
                                <KeywordTableHead
                                  order={tagOrder}
                                  orderBy={tagOrderBy}
                                  onRequestSort={handleTagSort}
                                  numSelected={selectedChannels.length}
                                  rowCount={tagChannels.length}
                                  onSelectAllClick={handleToggleAll}
                                  headLabel={[
                                    { id: 'select', label: '', align: 'center', disableSort: true, width: '40px' },
                                    { id: 'id', label: 'ID', align: 'center', width: '70px' },
                                    { id: 'name', label: t('channel_index.name'), align: 'center', minWidth: 150 },
                                    { id: 'group', label: t('channel_index.group'), align: 'center', disableSort: true, minWidth: 110 },
                                    { id: 'type', label: t('channel_index.type'), align: 'center', minWidth: 100 },
                                    { id: 'status', label: t('channel_index.status'), align: 'center', minWidth: 110 },
                                    { id: 'used_quota', label: t('channel_index.usedBalance'), align: 'center', minWidth: 120 },
                                    { id: 'response_time', label: t('channel_index.responseTime'), align: 'center', minWidth: 110 },
                                    { id: 'priority', label: t('channel_index.priority'), align: 'center', minWidth: 100 },
                                    { id: 'weight', label: t('channel_index.weight'), align: 'center', minWidth: 100 },
                                    { id: 'cost_ratio', label: t('channel_index.costRatio'), align: 'center', minWidth: 100 },
                                    {
                                      id: 'action',
                                      label: t('channel_index.actions'),
                                      align: 'center',
                                      disableSort: true,
                                      sticky: true,
                                      minWidth: 130
                                    }
                                  ]}
                                />
                                <TableBody>
                                  {tagChannels.map((channel) => (
                                    <TableRow key={channel.id} hover>
                                      <TableCell padding="checkbox" sx={{ pl: 1, textAlign: 'center' }}>
                                        <Checkbox
                                          checked={selectedChannels.includes(channel.id)}
                                          onChange={() => handleToggleChannel(channel.id)}
                                          size="small"
                                        />
                                      </TableCell>
                                      <TableCell sx={{ textAlign: 'center' }}>
                                        <Typography variant="body2">{channel.id}</Typography>
                                      </TableCell>
                                      <TableCell sx={{ textAlign: 'center' }}>
                                        <Typography variant="body2" noWrap title={channel.name} sx={{ fontWeight: 500 }}>
                                          {channel.name}
                                        </Typography>
                                      </TableCell>
                                      <TableCell sx={{ textAlign: 'center' }}>
                                        <Box sx={{ display: 'flex', justifyContent: 'center' }}>
                                          <GroupLabel group={channel.group ?? ''} />
                                        </Box>
                                      </TableCell>
                                      <TableCell sx={{ textAlign: 'center' }}>
                                        {CHANNEL_OPTIONS[channel.type] ? (
                                          <Label color={CHANNEL_OPTIONS[channel.type].color} variant="outlined">
                                            {CHANNEL_OPTIONS[channel.type].text}
                                          </Label>
                                        ) : (
                                          <Label color="error" variant="outlined">
                                            {t('common.unknown')}
                                          </Label>
                                        )}
                                      </TableCell>
                                      <TableCell sx={{ textAlign: 'center' }}>
                                        <Stack direction="row" alignItems="center" spacing={0.5} justifyContent="center">
                                          <Switch
                                            checked={channel.status === 1}
                                            onChange={() => handleTagChannelStatus(channel.id, channel.status)}
                                            size="small"
                                          />
                                          <Typography
                                            variant="caption"
                                            sx={{
                                              fontWeight: channel.status === 1 ? 600 : 400,
                                              color: channel.status === 1 ? 'success.main' : 'text.secondary'
                                            }}
                                          >
                                            {statusInfo(t, channel.status)}
                                            {/* {CHANNEL_STATUS_MAP[channel.status]?.label || '未知'} */}
                                          </Typography>
                                        </Stack>
                                      </TableCell>
                                      <TableCell sx={{ textAlign: 'center' }}>
                                        <Tooltip title={t('channel_row.clickUpdateQuota')} placement="top">
                                          <Box sx={{ cursor: 'pointer' }} onClick={() => manageChannel(channel.id, 'update_balance')}>
                                            <Stack direction="column" spacing={0.5} alignItems="center" justifyContent="center">
                                              <Typography
                                                variant="body2"
                                                sx={{
                                                  fontSize: '0.8rem',
                                                  fontWeight: 500,
                                                  '&:hover': { textDecoration: 'underline' }
                                                }}
                                              >
                                                {renderQuota(channel.used_quota)}
                                              </Typography>
                                              <Typography
                                                variant="caption"
                                                sx={{
                                                  color: 'success.main',
                                                  fontWeight: 600
                                                }}
                                              >
                                                {renderBalance(channel.type, channel.balance)}
                                              </Typography>
                                            </Stack>
                                          </Box>
                                        </Tooltip>
                                      </TableCell>
                                      <TableCell sx={{ textAlign: 'center' }}>
                                        <ResponseTimeLabel test_time={channel.test_time} response_time={channel.response_time} />
                                      </TableCell>

                                      <TableCell sx={{ textAlign: 'center' }}>
                                        <Box sx={{ display: 'flex', justifyContent: 'center' }}>
                                          <GroupInlineEditor
                                            label={t('channel_index.priority')}
                                            value={channel.priority ?? 0}
                                            min="0"
                                            onChange={(v) => handleTagChannelPriorityChange(channel.id, v)}
                                            onCommit={() => commitTagChannelPriority(channel)}
                                            disabled={
                                              channel.priority ===
                                              (tagChannelOriginalsRef.current[channel.id]?.priority ?? channel.priority)
                                            }
                                          />
                                        </Box>
                                      </TableCell>
                                      <TableCell sx={{ textAlign: 'center' }}>
                                        <Box sx={{ display: 'flex', justifyContent: 'center' }}>
                                          <GroupInlineEditor
                                            label={t('channel_index.weight')}
                                            value={channel.weight ?? 1}
                                            min="1"
                                            onChange={(v) => handleTagChannelWeightChange(channel.id, v)}
                                            onCommit={() => commitTagChannelWeight(channel)}
                                            disabled={
                                              channel.weight === (tagChannelOriginalsRef.current[channel.id]?.weight ?? channel.weight)
                                            }
                                          />
                                        </Box>
                                      </TableCell>
                                      <TableCell sx={{ textAlign: 'center' }}>
                                        <Box sx={{ display: 'flex', justifyContent: 'center' }}>
                                          <GroupInlineEditor
                                            label={t('channel_index.costRatio')}
                                            value={channel.cost_ratio ?? 0}
                                            min="0"
                                            step="0.1"
                                            onChange={(v) => handleTagChannelCostRatioChange(channel.id, v)}
                                            onCommit={() => commitTagChannelCostRatio(channel)}
                                            disabled={
                                              channel.cost_ratio ===
                                              (tagChannelOriginalsRef.current[channel.id]?.cost_ratio ?? channel.cost_ratio)
                                            }
                                          />
                                        </Box>
                                      </TableCell>
                                      <TableCell align="center" sx={stickyCellSx}>
                                        <Stack direction="row" spacing={1} justifyContent="center">
                                          <Tooltip title={t('channel_row.testModels')} placement="top">
                                            <IconButton
                                              size="small"
                                              sx={{ p: 0.5, color: 'info.main' }}
                                              onClick={(event) => {
                                                handleTagChannelTest(channel);
                                                // 记录点击位置用于弹出模型列表
                                                if (channel.models.split(',').length > 1) {
                                                  tagModelPopover.onOpen(event);
                                                }
                                              }}
                                            >
                                              <Icon icon="mdi:speedometer" width={18} height={18} />
                                            </IconButton>
                                          </Tooltip>

                                          <Tooltip title={t('common.edit')} placement="top">
                                            <IconButton
                                              size="small"
                                              sx={{ p: 0.5, color: 'primary.main' }}
                                              onClick={() => {
                                                setSubEditChannelId(channel.id);
                                                subEdit.onTrue();
                                              }}
                                            >
                                              <Icon icon="solar:pen-bold" width={18} height={18} />
                                            </IconButton>
                                          </Tooltip>

                                          <Tooltip title={t('channel_index.actions')} placement="top">
                                            <IconButton
                                              size="small"
                                              sx={{ p: 0.5 }}
                                              onClick={(event) => {
                                                // 设置当前操作的渠道
                                                setCurrentTestingChannel(channel);
                                                // 打开更多操作菜单
                                                popover.onOpen(event);
                                              }}
                                            >
                                              <Icon icon="eva:more-vertical-fill" width={18} height={18} />
                                            </IconButton>
                                          </Tooltip>
                                        </Stack>
                                      </TableCell>
                                    </TableRow>
                                  ))}
                                </TableBody>
                              </Table>
                            </TableContainer>
                          </Box>
                          <Box sx={{ display: 'flex', justifyContent: 'flex-end', pt: 1 }}>
                            <TablePagination
                              component="div"
                              count={totalTagChannels}
                              page={tagPage}
                              onPageChange={handleChangeTagPage}
                              rowsPerPage={tagRowsPerPage}
                              onRowsPerPageChange={handleChangeTagRowsPerPage}
                              rowsPerPageOptions={PAGE_SIZE_OPTIONS}
                              labelRowsPerPage={t('channel_row.rowsPerPage')}
                              labelDisplayedRows={({ from, to, count }) => t('channel_row.paginationDisplayedRows', { from, to, count })}
                              sx={{
                                '.MuiTablePagination-toolbar': {
                                  minHeight: '40px',
                                  pl: 1
                                },
                                '.MuiTablePagination-selectLabel, .MuiTablePagination-displayedRows': {
                                  fontSize: '0.75rem'
                                },
                                '.MuiTablePagination-select': {
                                  padding: '0 8px'
                                }
                              }}
                            />
                          </Box>
                        </Box>
                      )}
                    </Box>
                  </Grid>
                )}
              </Grid>
            </Box>
          </Collapse>
        </TableCell>
      </TableRow>
      <Dialog open={confirmDelete.value} onClose={confirmDelete.onFalse}>
        <DialogTitle>{t('channel_row.delChannel')}</DialogTitle>
        <DialogContent>
          <DialogContentText>
            {t('common.deleteConfirm', { title: currentTestingChannel ? currentTestingChannel.name : item.name })}
          </DialogContentText>
        </DialogContent>
        <DialogActions>
          <Button onClick={confirmDelete.onFalse}>{t('token_index.close')}</Button>
          <Button
            onClick={() => {
              if (currentTestingChannel) {
                // 处理子渠道删除
                manageChannel(currentTestingChannel.id, 'delete', '')
                  .then(({ success }) => {
                    if (success) {
                      // 从本地列表中移除
                      setTagChannels((prev) => prev.filter((c) => c.id !== currentTestingChannel.id));
                      // 减少总数
                      setTotalTagChannels((prev) => prev - 1);
                      // 重置当前选中的渠道
                      setCurrentTestingChannel(null);
                      onTagStatsRefresh?.(); // 刷新标签聚合（总数/启用数）
                      showSuccess(t('common.deleteSuccess'));
                    }
                  })
                  .catch((error) => {
                    showError(t('common.deleteError', { message: error.message }));
                  });
              } else {
                // 处理主渠道删除
                handleDeleteRow();
              }
              confirmDelete.onFalse();
            }}
            sx={{ color: 'error.main' }}
            disabled={deleting}
            autoFocus
          >
            {deleting ? t('channel_index.deleting') : t('common.delete')}
          </Button>
        </DialogActions>
      </Dialog>
      <ChannelCheck item={currentTestingChannel || item} open={openCheck} onClose={() => setOpenCheck(false)} />

      <ConfirmDialog
        open={tagDeleteConfirm.value}
        onClose={tagDeleteConfirm.onFalse}
        title={t('channel_row.deleteTag')}
        content={t('channel_row.deleteTagConfirm', { tag: item.tag })}
        action={
          <Button
            variant="contained"
            color="error"
            onClick={() => {
              manageChannel(item.tag, 'delete', '', true)
                .then(({ success }) => {
                  if (success) {
                    showInfo(t('channel_row.deleteTagSuccess', { tag: item.tag }));
                    onRefresh(false); // 刷新父组件数据
                  }
                })
                .catch((error) => {
                  showError(t('channel_row.deleteTagError', { message: error.message }));
                });
              tagDeleteConfirm.onFalse();
            }}
          >
            {t('common.delete')}
          </Button>
        }
      />

      <ConfirmDialog
        open={tagStatusConfirm.value}
        onClose={tagStatusConfirm.onFalse}
        title={statusChangeAction === 'enable' ? t('channel_row.enableTagChannels') : t('channel_row.disableTagChannels')}
        content={t('channel_row.tagChannelsConfirm', {
          action: statusChangeAction === 'enable' ? t('channel_row.enable') : t('channel_row.disable'),
          tag: item.tag
        })}
        action={
          <Button
            variant="contained"
            color={statusChangeAction === 'enable' ? 'success' : 'error'}
            onClick={() => {
              manageChannel(item.tag, 'tag_change_status', statusChangeAction, false)
                .then(({ success }) => {
                  if (success) {
                    showInfo(
                      t('channel_row.tagChannelsSuccess', {
                        action: statusChangeAction === 'enable' ? t('channel_row.enable') : t('channel_row.disable')
                      })
                    );
                    if (openRow) fetchTagChannels(); // 子表格展开时刷新成员状态
                    onTagStatsRefresh?.(); // 刷新标签聚合（启用/禁用数）
                    onRefresh(false); // 刷新父组件数据
                  }
                })
                .catch((error) => {
                  showError(
                    t('channel_row.tagChannelsError', {
                      action: statusChangeAction === 'enable' ? t('channel_row.enable') : t('channel_row.disable'),
                      message: error.message
                    })
                  );
                });
              tagStatusConfirm.onFalse();
            }}
          >
            {t('common.submit')}
          </Button>
        }
      />

      {/* 代表行行内编辑优先级/权重/成本倍率：批量覆盖整组前二次确认 */}
      <ConfirmDialog
        open={tagFieldConfirmOpen.value}
        onClose={tagFieldConfirmOpen.onFalse}
        title={t('channel_edit.tagBulkFieldConfirmTitle')}
        content={t('channel_edit.tagBulkFieldConfirmContent', {
          tag: item.tag,
          count: tagStat?.count ?? 0,
          field: tagFieldConfirm?.field,
          value: tagFieldConfirm?.value
        })}
        action={
          <Button
            variant="contained"
            color="warning"
            onClick={() => {
              tagFieldConfirmOpen.onFalse();
              tagFieldConfirm?.exec();
            }}
          >
            {t('common.submit')}
          </Button>
        }
      />

      <EditeModal
        open={quickEdit.value}
        onCancel={quickEdit.onFalse}
        onOk={() => {
          onRefresh(false);
          if (item.tag) onTagStatsRefresh?.();
          quickEdit.onFalse();
        }}
        channelId={item.tag ? item.tag : item.id}
        groupOptions={groupOptions}
        isTag={!!item.tag}
        modelOptions={modelOptions}
        prices={prices}
        tags={tags}
      />

      <Popover
        open={tagModelPopover.open}
        anchorEl={tagModelPopover.anchorEl}
        onClose={tagModelPopover.onClose}
        anchorOrigin={{ vertical: 'top', horizontal: 'left' }}
        transformOrigin={{ vertical: 'top', horizontal: 'right' }}
        PaperProps={{
          sx: { minWidth: 140 }
        }}
      >
        <MenuList>
          {currentTestingChannel &&
            currentTestingChannel.models.split(',').map((model) => (
              <MenuItem
                key={`tag-test-model-${model}`}
                onClick={() => {
                  manageChannel(currentTestingChannel.id, 'test', model).then(({ success, time }) => {
                    if (success) {
                      showSuccess(
                        t('channel_row.modelTestSuccess', {
                          channel: currentTestingChannel.name,
                          model,
                          time: time.toFixed(2)
                        })
                      );
                      // 更新本地状态
                      setTagChannels((prev) =>
                        prev.map((c) =>
                          c.id === currentTestingChannel.id
                            ? {
                                ...c,
                                test_time: Date.now() / 1000,
                                response_time: time * 1000
                              }
                            : c
                        )
                      );
                    }
                  });
                  tagModelPopover.onClose();
                }}
              >
                {model}
              </MenuItem>
            ))}
        </MenuList>
      </Popover>

      <ConfirmDialog
        open={batchConfirm.value}
        onClose={batchConfirm.onFalse}
        title={t('channel_row.batchDelete')}
        content={t('channel_row.batchDeleteConfirm', { count: selectedChannels.length })}
        action={
          <Button
            variant="contained"
            color="error"
            onClick={() => {
              executeBatchDelete();
              batchConfirm.onFalse();
            }}
          >
            {t('common.delete')}
          </Button>
        }
      />

      {/* 子渠道完整编辑：复用主 EditModal（isTag=false），可单独修改该标签成员的全部配置；
          保存只作用于该渠道（/api/channel/），共享字段会在下次分组统一编辑时被覆盖（弹窗内有提示）。 */}
      <EditeModal
        open={subEdit.value}
        onCancel={subEdit.onFalse}
        onOk={(status) => {
          subEdit.onFalse();
          if (status === true) {
            fetchTagChannels(); // 刷新子表格
            onTagStatsRefresh?.(); // 刷新标签聚合（类型/分组/启用数等）
            onRefresh(false); // 刷新父列表代表行
          }
        }}
        channelId={subEditChannelId}
        groupOptions={groupOptions}
        isTag={false}
        modelOptions={modelOptions}
        prices={prices}
        tags={tags}
      />
    </>
  );
}

ChannelTableRow.propTypes = {
  item: PropTypes.object,
  manageChannel: PropTypes.func,
  onRefresh: PropTypes.func,
  onTagStatsRefresh: PropTypes.func,
  groupOptions: PropTypes.array,
  modelOptions: PropTypes.array,
  prices: PropTypes.array,
  selected: PropTypes.bool,
  onSelect: PropTypes.func,
  tags: PropTypes.array
};

function renderBalance(type, balance) {
  // balance 可能为 null（渠道从未更新过余额），统一兜底为 0 并保留两位小数，避免 toFixed 抛错
  const value = Number(balance) || 0;
  switch (type) {
    case 28: // Deepseek
    case 45: // Deepseek
      return <>¥{value.toFixed(2)}</>;
    default:
      return <>${value.toFixed(2)}</>;
  }
}

// 行内数字编辑器：outlined + 浮动标签，普通渠道行与标签代表行共用同一控件保证样式一致。
// tip 非空时（标签代表行）整体包一层 Tooltip 说明「作用于整组」，并由 label 后缀「·组」标明范围；普通行字段名由表头说明，无需 tip。
function GroupInlineEditor({ label, tip, value, min, step, onChange, onCommit, disabled }) {
  const field = (
    <Box sx={{ display: 'flex' }}>
      <TextField
        type="number"
        label={label}
        variant="outlined"
        size="small"
        value={value ?? ''}
        onChange={(e) => onChange(Number(e.target.value))}
        onKeyDown={(e) => {
          if (e.key === 'Enter') {
            e.preventDefault();
            onCommit();
          }
        }}
        onBlur={onCommit}
        inputProps={{ min, step }}
        sx={{ width: '90px' }}
        InputProps={{
          endAdornment: (
            <InputAdornment position="end">
              <IconButton size="small" color="primary" disabled={disabled} onClick={onCommit}>
                <Icon icon="mdi:check" />
              </IconButton>
            </InputAdornment>
          )
        }}
      />
    </Box>
  );
  return tip ? (
    <Tooltip title={tip} placement="top" arrow>
      {field}
    </Tooltip>
  ) : (
    field
  );
}

GroupInlineEditor.propTypes = {
  label: PropTypes.string,
  tip: PropTypes.string,
  value: PropTypes.oneOfType([PropTypes.number, PropTypes.string]),
  min: PropTypes.string,
  step: PropTypes.string,
  onChange: PropTypes.func,
  onCommit: PropTypes.func,
  disabled: PropTypes.bool
};
