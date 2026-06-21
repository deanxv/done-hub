import { useCallback, useEffect, useState } from 'react';
import { showError, showInfo, showSuccess, trims } from 'utils/common';
import AdminContainer from 'ui-component/AdminContainer';

import { useTheme } from '@mui/material/styles';
import Table from '@mui/material/Table';
import TableBody from '@mui/material/TableBody';
import TableContainer from '@mui/material/TableContainer';

import TablePagination from '@mui/material/TablePagination';
import LinearProgress from '@mui/material/LinearProgress';
import ButtonGroup from '@mui/material/ButtonGroup';
import Toolbar from '@mui/material/Toolbar';
import useMediaQuery from '@mui/material/useMediaQuery';
import Alert from '@mui/material/Alert';
import Collapse from '@mui/material/Collapse';

import { Box, Button, Card, Container, Divider, IconButton, Stack, Typography } from '@mui/material';
import ChannelTableRow from './component/TableRow';
import KeywordTableHead from 'ui-component/TableHead';
import { API } from 'utils/api';
import EditeModal from './component/EditModal';
import { getPageSize, PAGE_SIZE_OPTIONS, savePageSize, getTableSort, saveTableSort } from 'constants';
import TableToolBar from './component/TableToolBar';
import BatchModal from './component/BatchModal';
import { useTranslation } from 'react-i18next';

import { useBoolean } from 'hooks/use-boolean';
import useStickyShadow from 'hooks/useStickyShadow';
import ConfirmDialog from 'ui-component/confirm-dialog';
import FilterCollapse from 'ui-component/FilterCollapse';
import { Icon } from '@iconify/react';

const originalKeyword = {
  type: 0,
  status: 0,
  name: '',
  group: 'all',
  models: '',
  key: '',
  test_model: '',
  other: '',
  filter_tag: 0,
  tag: 'all',
  base_url: ''
};

export async function fetchChannelData(page, rowsPerPage, keyword, order, orderBy) {
  try {
    if (orderBy) {
      orderBy = order === 'desc' ? '-' + orderBy : orderBy;
    }
    const res = await API.get(`/api/channel/`, {
      params: {
        page: page + 1,
        size: rowsPerPage,
        order: orderBy,
        ...keyword
      }
    });
    const { success, message, data } = res.data;
    if (success) {
      return data;
    } else {
      showError(message);
    }
  } catch (error) {
    console.error(error);
  }

  return false;
}

// ----------------------------------------------------------------------
// CHANNEL_OPTIONS,
export default function ChannelList() {
  const { t } = useTranslation();
  const stickyShadowRef = useStickyShadow();
  const [page, setPage] = useState(0);
  const [order, setOrder] = useState(() => getTableSort('channel').order);
  const [orderBy, setOrderBy] = useState(() => getTableSort('channel').orderBy);
  const [rowsPerPage, setRowsPerPage] = useState(() => getPageSize('channel'));
  const [listCount, setListCount] = useState(0);
  const [searching, setSearching] = useState(false);
  const [channels, setChannels] = useState([]);
  const [refreshFlag, setRefreshFlag] = useState(false);
  const [tags, setTags] = useState([]);
  const [modelOptions, setModelOptions] = useState([]);

  const confirm = useBoolean();
  const [confirmTitle, setConfirmTitle] = useState('');
  const [confirmConfirm, setConfirmConfirm] = useState(() => {});

  const [groupOptions, setGroupOptions] = useState([]);
  const [toolBarValue, setToolBarValue] = useState(originalKeyword);
  const [searchKeyword, setSearchKeyword] = useState(originalKeyword);

  const theme = useTheme();
  const matchUpMd = useMediaQuery(theme.breakpoints.up('sm'));
  const [openModal, setOpenModal] = useState(false);
  const [editChannelId, setEditChannelId] = useState(0);
  const [openBatchModal, setOpenBatchModal] = useState(false);
  const [prices, setPrices] = useState([]);

  // 批量删除相关状态
  const [selectedChannels, setSelectedChannels] = useState([]);
  const [batchDeleteConfirm, setBatchDeleteConfirm] = useState(false);

  // 提示框展开状态
  const [alertExpanded, setAlertExpanded] = useState(false);

  const handleSort = (event, id) => {
    const isAsc = orderBy === id && order === 'asc';
    if (id !== '') {
      const newOrder = isAsc ? 'desc' : 'asc';
      setOrder(newOrder);
      setOrderBy(id);
      saveTableSort('channel', newOrder, id);
    }
  };

  const handleChangePage = (event, newPage) => {
    setPage(newPage);
  };

  const handleChangeRowsPerPage = (event) => {
    const newRowsPerPage = parseInt(event.target.value, 10);
    setPage(0);
    setRowsPerPage(newRowsPerPage);
    savePageSize('channel', newRowsPerPage);
  };

  const fetchPrices = useCallback(async () => {
    try {
      const res = await API.get('/api/prices');
      const { success, message, data } = res.data;
      if (success) {
        setPrices(data);
      } else {
        showError(message);
      }
    } catch (error) {
      console.error(error);
    }
  }, []);

  const searchChannels = async () => {
    // 如果正在搜索中，防止重复提交
    if (searching) {
      return;
    }

    setPage(0);
    // 使用时间戳来确保即使搜索条件相同也能触发重新搜索
    const searchPayload = {
      ...toolBarValue,
      _timestamp: Date.now()
    };
    setSearchKeyword(searchPayload);
  };

  const handleToolBarValue = (event) => {
    setToolBarValue({ ...toolBarValue, [event.target.name]: event.target.value });
  };

  const manageChannel = async (id, action, value, tag = false) => {
    let url = '/api/channel/';
    if (tag) {
      url = '/api/channel_tag/';
    }

    let data = { id };
    let res;

    try {
      switch (action) {
        case 'copy': {
          let oldRes = await API.get(`/api/channel/${id}`);
          const { success, message, data } = oldRes.data;
          if (!success) {
            showError(message);
            return { success: false, message };
          }
          // 删除 data.id
          delete data.id;
          delete data.test_time;
          delete data.balance_updated_time;
          delete data.used_quota;
          delete data.response_time;
          data.name = data.name + '_copy';
          res = await API.post(`/api/channel/`, { ...data });
          break;
        }
        case 'delete':
          if (tag) {
            res = await API.delete(url + encodeURIComponent(id));
          } else {
            res = await API.delete(`${url}${id}`);
          }
          break;
        case 'delete_tag':
          res = await API.delete(url + id + '/tag');
          break;
        case 'status':
          res = await API.put(url, {
            ...data,
            status: value
          });
          break;
        case 'priority':
        case 'weight':
        case 'cost_ratio':
          if (value === '') {
            return { success: false, message: '值不能为空' };
          }

          if (!tag) {
            res = await API.put(url, {
              ...data,
              [action]: Number(value)
            });
          } else {
            // 整组统一设置优先级/权重/成本倍率，复用同一端点，由 type 区分
            res = await API.put(`${url + encodeURIComponent(id)}/priority`, {
              type: action,
              value: Number(value)
            });
          }
          break;
        case 'test':
          res = await API.get(url + `test/${id}`, {
            params: { model: value }
          });
          break;
        case 'batch_delete':
          res = await API.delete('/api/channel/batch', {
            data: {
              value: 'batch_delete',
              ids: value
            }
          });
          break;
        case 'tag_change_status':
          res = await API.put(`/api/channel_tag/${id}/status/${value}`);
          break;
        default:
          showError('无效操作');
          return { success: false, message: '无效操作' };
      }
      const { success, message } = res.data;
      if (success) {
        // batch_delete 有专门成功消息；priority/weight/cost_ratio 由调用方弹更具体的提示
        // （区分整组/单渠道），此处不再弹通用消息，避免一次操作出现两个提示
        if (!['batch_delete', 'priority', 'weight', 'cost_ratio'].includes(action)) {
          showSuccess(t('userPage.operationSuccess'));
        }
        if (action === 'delete' || action === 'copy' || action == 'delete_tag' || action === 'batch_delete') {
          await handleRefresh(false);
        }
      } else {
        showError(message);
      }

      return res.data;
    } catch (error) {
      return { success: false, message: error.message };
    }
  };

  // 处理刷新
  const handleRefresh = async (reset) => {
    if (reset) {
      setOrderBy('id');
      setOrder('desc');
      saveTableSort('channel', 'desc', 'id');
      setToolBarValue(originalKeyword);
      setSearchKeyword(originalKeyword);
    }
    setRefreshFlag(!refreshFlag);
  };

  const handlePopoverOpen = useCallback(
    (title, onConfirm) => {
      setConfirmTitle(title);
      setConfirmConfirm(() => onConfirm);
      confirm.onTrue();
    },
    [confirm]
  );

  // 处理测试所有启用渠道
  const testAllChannels = async () => {
    try {
      const res = await API.get(`/api/channel/test`);
      const { success, message } = res.data;
      if (success) {
        showInfo(t('channel_row.testAllChannel'));
      } else {
        showError(message);
      }
    } catch (error) {}
  };

  // 处理删除所有禁用渠道
  const deleteAllDisabledChannels = async () => {
    try {
      const res = await API.delete(`/api/channel/disabled`);
      const { success, message, data } = res.data;
      if (success) {
        showSuccess(t('channel_row.delChannelCount', { count: data }));
        await handleRefresh();
      } else {
        showError(message);
      }
    } catch (error) {}
  };

  // 处理更新所有启用渠道余额
  const updateAllChannelsBalance = async () => {
    setSearching(true);
    try {
      const res = await API.get(`/api/channel/update_balance`);
      const { success, message } = res.data;
      if (success) {
        showInfo(t('channel_row.updateChannelBalance'));
      } else {
        showError(message);
      }
    } catch (error) {
      console.log(error);
    }

    setSearching(false);
  };

  const handleOpenModal = (channelId) => {
    setEditChannelId(channelId);
    setOpenModal(true);
  };

  const handleCloseModal = () => {
    setOpenModal(false);
    setEditChannelId(0);
  };

  const handleOkModal = (status) => {
    if (status === true) {
      handleCloseModal();
      handleRefresh(false);
    }
  };

  const fetchData = async (page, rowsPerPage, keyword, order, orderBy) => {
    setSearching(true);
    keyword = trims(keyword);

    // 移除仅用于触发状态更新的时间戳字段
    if (keyword._timestamp) {
      delete keyword._timestamp;
    }

    // 将 group 和 tag 的 'all' 转换为空字符串
    if (keyword.group === 'all') {
      keyword.group = '';
    }
    if (keyword.tag === 'all') {
      keyword.tag = '';
    }

    const data = await fetchChannelData(page, rowsPerPage, keyword, order, orderBy);

    if (data) {
      setListCount(data.total_count);
      setChannels(data.data);
    }
    setSearching(false);
  };

  const fetchGroups = async () => {
    try {
      let res = await API.get(`/api/group/`);
      const groups = Array.isArray(res.data.data) ? [...res.data.data].sort((a, b) => String(a).localeCompare(String(b))) : [];
      setGroupOptions(groups);
    } catch (error) {
      showError(error.message);
    }
  };

  const fetchTags = async () => {
    try {
      let res = await API.get(`/api/channel_tag/_all`);
      const { success, data } = res.data;
      if (success) {
        setTags(data);
      }
    } catch (error) {
      showError(error.message);
    }
  };

  const fetchModels = async () => {
    try {
      let res = await API.get(`/api/channel/models`);
      const { data } = res.data;
      // 先对data排序
      data.sort((a, b) => {
        const ownedByComparison = a.owned_by.localeCompare(b.owned_by);
        if (ownedByComparison === 0) {
          return a.id.localeCompare(b.id);
        }
        return ownedByComparison;
      });
      setModelOptions(
        data.map((model) => {
          return {
            id: model.id,
            group: model.owned_by
          };
        })
      );
    } catch (error) {
      showError(error.message);
    }
  };

  useEffect(() => {
    fetchData(page, rowsPerPage, searchKeyword, order, orderBy);
  }, [page, rowsPerPage, searchKeyword, order, orderBy, refreshFlag]);

  useEffect(() => {
    fetchGroups().then();
    fetchTags().then();
    fetchModels().then();
    fetchPrices().then();
  }, [fetchPrices]);

  // 处理批量删除
  const handleBatchDelete = () => {
    if (selectedChannels.length === 0) {
      showError(t('channel_index.pleaseSelectChannels'));
      return;
    }
    setBatchDeleteConfirm(true);
  };

  const confirmBatchDelete = async () => {
    try {
      const { success, message } = await manageChannel(null, 'batch_delete', selectedChannels);
      if (success) {
        showSuccess(t('channel_index.batchDeleteChannelsSuccess', { count: selectedChannels.length }));
        setSelectedChannels([]);
      } else {
        showError(message);
      }
    } catch (error) {
      showError(error.message);
    }
    setBatchDeleteConfirm(false);
  };

  // 处理全选/取消全选
  const handleSelectAll = () => {
    if (selectedChannels.length === channels.length) {
      setSelectedChannels([]);
    } else {
      setSelectedChannels(channels.map((channel) => channel.id));
    }
  };

  // 处理单个选择
  const handleSelectChannel = (channelId) => {
    const selectedIndex = selectedChannels.indexOf(channelId);
    let newSelected = [];

    if (selectedIndex === -1) {
      newSelected = newSelected.concat(selectedChannels, channelId);
    } else if (selectedIndex === 0) {
      newSelected = newSelected.concat(selectedChannels.slice(1));
    } else if (selectedIndex === selectedChannels.length - 1) {
      newSelected = newSelected.concat(selectedChannels.slice(0, -1));
    } else if (selectedIndex > 0) {
      newSelected = newSelected.concat(selectedChannels.slice(0, selectedIndex), selectedChannels.slice(selectedIndex + 1));
    }

    setSelectedChannels(newSelected);
  };

  return (
    <AdminContainer>
      <style>
        {`
          @keyframes spin {
            from { transform: rotate(0deg); }
            to { transform: rotate(360deg); }
          }
        `}
      </style>
      <Stack direction="row" alignItems="center" justifyContent="space-between" mb={5}>
        <Stack direction="column" spacing={1}>
          <Typography variant="h2">{t('channel_index.channel')}</Typography>
          <Typography variant="subtitle1" color="text.secondary">
            Channel
          </Typography>
        </Stack>

        <ButtonGroup variant="contained" aria-label="outlined small primary button group">
          <Button color="primary" startIcon={<Icon icon="solar:add-circle-line-duotone" />} onClick={() => handleOpenModal(0)}>
            {t('channel_index.newChannel')}
          </Button>
          <Button color="primary" startIcon={<Icon icon="solar:menu-dots-bold-duotone" />} onClick={() => setOpenBatchModal(true)}>
            {t('channel_index.batchProcessing')}
          </Button>
        </ButtonGroup>
      </Stack>
      <Stack mb={2}>
        <Alert
          severity="info"
          action={
            <IconButton
              size="small"
              onClick={(e) => {
                e.stopPropagation();
                setAlertExpanded(!alertExpanded);
              }}
              sx={{ ml: 1 }}
            >
              <Icon icon={alertExpanded ? 'solar:alt-arrow-up-line-duotone' : 'solar:alt-arrow-down-line-duotone'} width={18} />
            </IconButton>
          }
          sx={{ cursor: 'pointer' }}
          onClick={() => setAlertExpanded(!alertExpanded)}
        >
          <Typography variant="body2" component="span">
            {t('channel_index.priorityWeightExplanation')}
          </Typography>
          <Collapse in={alertExpanded} timeout="auto">
            <Box sx={{ mt: 1 }}>
              {t('channel_index.description1')}
              <br />
              {t('channel_index.description2')}
              <br />
              {t('channel_index.description3')}
              <br />
              {t('channel_index.description4')}
            </Box>
          </Collapse>
        </Alert>
      </Stack>
      <Card>
        <FilterCollapse>
          <Box component="form" noValidate>
            <TableToolBar
              filterName={toolBarValue}
              handleFilterName={handleToolBarValue}
              groupOptions={groupOptions}
              tags={tags}
              onSearch={searchChannels}
            />
          </Box>
        </FilterCollapse>

        <Toolbar
          sx={{
            textAlign: 'right',
            height: 50,
            display: 'flex',
            justifyContent: 'space-between',
            p: (theme) => theme.spacing(0, 1, 0, 3),
            minWidth: 0
          }}
        >
          {/* 左侧删除渠道按钮 */}
          {matchUpMd && (
            <Button
              variant="outlined"
              onClick={handleBatchDelete}
              disabled={selectedChannels.length === 0}
              startIcon={<Icon icon="solar:trash-bin-2-bold-duotone" width={18} />}
              color="error"
              sx={{
                minWidth: 'auto',
                whiteSpace: 'nowrap',
                flexShrink: 0
              }}
            >
              {t('channel_index.deleteChannels')} ({selectedChannels.length})
            </Button>
          )}

          <Box sx={{ flex: 1, overflow: 'hidden', minWidth: 0, display: 'flex', justifyContent: 'flex-end', ml: 2 }}>
            {matchUpMd ? (
              <Box
                sx={{
                  overflow: 'auto',
                  maxWidth: '100%',
                  scrollBehavior: 'smooth',
                  '&::-webkit-scrollbar': {
                    height: '4px'
                  },
                  '&::-webkit-scrollbar-thumb': {
                    backgroundColor: 'rgba(0,0,0,0.2)',
                    borderRadius: '2px'
                  }
                }}
              >
                <ButtonGroup
                  variant="outlined"
                  aria-label="outlined small primary button group"
                  sx={{
                    flexWrap: 'nowrap',
                    minWidth: 'max-content',
                    display: 'flex'
                  }}
                >
                  <Button
                    onClick={() => handleRefresh(true)}
                    startIcon={<Icon icon="solar:refresh-circle-bold-duotone" width={18} />}
                    sx={{
                      whiteSpace: 'nowrap',
                      minWidth: 'auto',
                      px: 1.5
                    }}
                  >
                    {t('channel_index.refreshClearSearchConditions')}
                  </Button>
                  <Button
                    onClick={searchChannels}
                    startIcon={
                      searching ? (
                        <Icon
                          icon="solar:refresh-bold-duotone"
                          width={18}
                          style={{
                            animation: 'spin 1s linear infinite',
                            color: '#1976d2'
                          }}
                        />
                      ) : (
                        <Icon icon="solar:magnifer-bold-duotone" width={18} />
                      )
                    }
                    sx={{
                      whiteSpace: 'nowrap',
                      minWidth: 'auto',
                      px: 1.5,
                      ...(searching && {
                        bgcolor: 'action.hover',
                        color: 'primary.main',
                        '&:hover': {
                          bgcolor: 'action.selected'
                        }
                      })
                    }}
                  >
                    {searching ? '搜索中...' : t('channel_index.search')}
                  </Button>
                  <Button
                    onClick={() => handlePopoverOpen(t('channel_index.testAllChannels'), testAllChannels)}
                    startIcon={<Icon icon="solar:test-tube-bold-duotone" width={18} />}
                    sx={{
                      whiteSpace: 'nowrap',
                      minWidth: 'auto',
                      px: 1.5
                    }}
                  >
                    {t('channel_index.testAllChannels')}
                  </Button>
                  <Button
                    onClick={() => handlePopoverOpen(t('channel_index.updateEnabledBalance'), updateAllChannelsBalance)}
                    startIcon={<Icon icon="solar:dollar-minimalistic-bold-duotone" width={18} />}
                    sx={{
                      whiteSpace: 'nowrap',
                      minWidth: 'auto',
                      px: 1.5
                    }}
                  >
                    {t('channel_index.updateEnabledBalance')}
                  </Button>
                  <Button
                    onClick={() => handlePopoverOpen(t('channel_index.deleteDisabledChannels'), deleteAllDisabledChannels)}
                    startIcon={<Icon icon="solar:trash-bin-trash-bold-duotone" width={18} />}
                    sx={{
                      whiteSpace: 'nowrap',
                      minWidth: 'auto',
                      px: 1.5
                    }}
                  >
                    {t('channel_index.deleteDisabledChannels')}
                  </Button>
                </ButtonGroup>
              </Box>
            ) : (
              <Container maxWidth="xl">
                <Stack
                  direction="row"
                  spacing={1}
                  divider={<Divider orientation="vertical" flexItem />}
                  justifyContent="space-around"
                  alignItems="center"
                >
                  <IconButton onClick={() => handleRefresh(true)} size="small">
                    <Icon icon="solar:refresh-circle-bold-duotone" width={18} />
                  </IconButton>
                  <IconButton
                    onClick={searchChannels}
                    size="small"
                    sx={{
                      ...(searching && {
                        bgcolor: 'action.hover',
                        color: 'primary.main'
                      })
                    }}
                  >
                    {searching ? (
                      <Icon
                        icon="solar:refresh-bold-duotone"
                        width={18}
                        style={{
                          animation: 'spin 1s linear infinite',
                          color: '#1976d2'
                        }}
                      />
                    ) : (
                      <Icon icon="solar:magnifer-bold-duotone" width={18} />
                    )}
                  </IconButton>
                  <IconButton onClick={() => handlePopoverOpen(t('channel_index.testAllChannels'), testAllChannels)} size="small">
                    <Icon icon="solar:test-tube-bold-duotone" width={18} />
                  </IconButton>
                  <IconButton
                    onClick={() => handlePopoverOpen(t('channel_index.updateEnabledBalance'), updateAllChannelsBalance)}
                    size="small"
                  >
                    <Icon icon="solar:dollar-minimalistic-bold-duotone" width={18} />
                  </IconButton>
                  <IconButton
                    onClick={() => handlePopoverOpen(t('channel_index.deleteDisabledChannels'), deleteAllDisabledChannels)}
                    size="small"
                  >
                    <Icon icon="solar:trash-bin-trash-bold-duotone" width={18} />
                  </IconButton>
                  <IconButton onClick={handleBatchDelete} disabled={selectedChannels.length === 0} size="small" color="error">
                    <Icon icon="solar:trash-bin-2-bold-duotone" width={18} />
                  </IconButton>
                </Stack>
              </Container>
            )}
          </Box>
        </Toolbar>
        {searching && <LinearProgress />}
        <TableContainer ref={stickyShadowRef}>
          <Table sx={{ minWidth: 800 }}>
            <KeywordTableHead
              order={order}
              orderBy={orderBy}
              onRequestSort={handleSort}
              numSelected={selectedChannels.length}
              rowCount={channels.length}
              onSelectAllClick={handleSelectAll}
              headLabel={[
                { id: 'select', label: '', disableSort: true, width: '50px' },
                { id: 'id', label: 'ID', disableSort: false, width: '80px' },
                { id: 'name', label: t('channel_index.name'), disableSort: false },
                { id: 'group', label: t('channel_index.group'), disableSort: true },
                { id: 'type', label: t('channel_index.type'), disableSort: false },
                { id: 'status', label: t('channel_index.status'), disableSort: false },
                { id: 'response_time', label: t('channel_index.responseTime'), disableSort: false },
                // { id: 'balance', label: '余额', disableSort: false },
                { id: 'used_quota', label: t('channel_index.usedBalance'), disableSort: false },
                { id: 'priority', label: t('channel_index.priority'), disableSort: false, width: '80px' },
                { id: 'weight', label: t('channel_index.weight'), disableSort: false, width: '80px' },
                { id: 'cost_ratio', label: t('channel_index.costRatio'), disableSort: false, width: '90px' },
                { id: 'action', label: t('channel_index.actions'), disableSort: true, sticky: true }
              ]}
            />
            <TableBody>
              {channels.map((row) => {
                const isSelected = selectedChannels.indexOf(row.id) !== -1;
                return (
                  <ChannelTableRow
                    item={row}
                    manageChannel={manageChannel}
                    key={row.id}
                    // handleOpenModal={handleOpenModal}
                    // setModalChannelId={setEditChannelId}
                    groupOptions={groupOptions}
                    onRefresh={handleRefresh}
                    onTagStatsRefresh={fetchTags}
                    modelOptions={modelOptions}
                    prices={prices}
                    selected={isSelected}
                    onSelect={() => handleSelectChannel(row.id)}
                    tags={tags}
                  />
                );
              })}
            </TableBody>
          </Table>
        </TableContainer>
        <TablePagination
          page={page}
          component="div"
          count={listCount}
          rowsPerPage={rowsPerPage}
          onPageChange={handleChangePage}
          rowsPerPageOptions={PAGE_SIZE_OPTIONS}
          onRowsPerPageChange={handleChangeRowsPerPage}
          showFirstButton
          showLastButton
        />
      </Card>
      <EditeModal
        open={openModal}
        onCancel={handleCloseModal}
        onOk={handleOkModal}
        channelId={editChannelId}
        groupOptions={groupOptions}
        modelOptions={modelOptions}
        prices={prices}
        tags={tags}
      />
      <BatchModal open={openBatchModal} setOpen={setOpenBatchModal} groupOptions={groupOptions} modelOptions={modelOptions} />

      <ConfirmDialog
        open={confirm.value}
        onClose={confirm.onFalse}
        title={confirmTitle}
        content={t('common.execute', { title: confirmTitle })}
        action={
          <Button
            variant="contained"
            onClick={() => {
              confirmConfirm();
              confirm.onFalse();
            }}
          >
            {t('common.executeConfirm')}
          </Button>
        }
      />

      {/* 批量删除确认对话框 */}
      <ConfirmDialog
        open={batchDeleteConfirm}
        onClose={() => setBatchDeleteConfirm(false)}
        title={t('channel_index.batchDeleteChannels')}
        content={t('channel_index.batchDeleteChannelsConfirm', { count: selectedChannels.length })}
        action={
          <Button variant="contained" color="error" onClick={confirmBatchDelete}>
            {t('common.delete')}
          </Button>
        }
      />
    </AdminContainer>
  );
}
