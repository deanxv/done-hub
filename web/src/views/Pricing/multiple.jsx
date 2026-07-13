import PropTypes from 'prop-types';
import { useState, useEffect, useMemo } from 'react';
import { showError, showSuccess } from 'utils/common';

import Table from '@mui/material/Table';
import TableBody from '@mui/material/TableBody';
import TableContainer from '@mui/material/TableContainer';
import PerfectScrollbar from 'react-perfect-scrollbar';
import {
  Box,
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  TextField,
  MenuItem,
  FormControl,
  InputLabel,
  Select,
  Paper,
  TablePagination,
  InputAdornment,
  IconButton,
  Card
} from '@mui/material';
import SearchIcon from '@mui/icons-material/Search';
import { Icon } from '@iconify/react';
import PricesTableRow from './component/TableRow';
import KeywordTableHead from 'ui-component/TableHead';
import { API } from 'utils/api';
import { useTranslation } from 'react-i18next';
import { getPageSize, savePageSize, PAGE_SIZE_OPTIONS } from 'constants';
import EditModal from './component/EditModal';
import ToggleButtonGroup from 'ui-component/ToggleButton';
import useStickyShadow from 'hooks/useStickyShadow';

// ----------------------------------------------------------------------
export default function Multiple({ prices, reloadData, ownedby, noPriceModels }) {
  const { t } = useTranslation();
  const stickyShadowRef = useStickyShadow();
  const [rows, setRows] = useState([]);
  const [selectedRow, setSelectedRow] = useState(null);
  const [editRow, setEditRow] = useState(null);
  const [searchTerm, setSearchTerm] = useState('');
  const [filterType, setFilterType] = useState('all');
  const [page, setPage] = useState(0);
  // 旧版本可能已在 localStorage 持久化了不在 PAGE_SIZE_OPTIONS 内的页大小（如 20），夹逼回合法值避免 TablePagination 下拉越界告警
  const savedPageSize = getPageSize('pricing_multiple', 10);
  const [rowsPerPage, setRowsPerPage] = useState(PAGE_SIZE_OPTIONS.includes(savedPageSize) ? savedPageSize : 10);
  const [channelFilter, setChannelFilter] = useState('all');
  const [lockFilter, setLockFilter] = useState('all');
  const [unit, setUnit] = useState('M');

  const unitOptions = [
    { value: 'K', label: 'K' },
    { value: 'M', label: 'M' }
  ];

  // 处理刷新
  const handleRefresh = async () => {
    reloadData();
  };

  const handleModalOK = () => {
    reloadData();
    setEditRow(null);
  };

  // 删除确认对话框
  const handleDeleteClick = (row) => {
    setSelectedRow(row);
  };

  const handleClose = () => {
    setSelectedRow(null);
  };

  const handleConfirmDelete = async () => {
    await managePrices(selectedRow, 'delete');
    setSelectedRow(null);
  };

  // 编辑对话框
  const handleEditClick = (row) => {
    setEditRow(row);
  };

  const handleEditClose = () => {
    setEditRow(null);
  };

  const handleUnitChange = (event, newUnit) => {
    if (newUnit !== null) {
      setUnit(newUnit);
    }
  };

  useEffect(() => {
    const grouped = prices.reduce((acc, item, index) => {
      // 需要保证 extra_ratios、long_context 和 locked 字段也相同才能合并
      const extraRatiosStr = item.extra_ratios ? JSON.stringify(item.extra_ratios) : '';
      const longContextStr = item.long_context ? JSON.stringify(item.long_context) : '';
      const key = `${item.type}-${item.channel_type}-${item.input}-${item.output}-${extraRatiosStr}-${longContextStr}-${item.locked}`;

      if (!acc[key]) {
        acc[key] = {
          ...item,
          models: [item.model],
          id: index + 1
        };
      } else {
        acc[key].models.push(item.model);
      }
      return acc;
    }, {});

    setRows(Object.values(grouped));
  }, [prices]);

  const managePrices = async (item, action) => {
    let res;
    try {
      switch (action) {
        case 'delete':
          res = await API.put('/api/prices/multiple/delete', {
            models: item.models
          });
          break;
      }
      const { success, message } = res.data;
      if (success) {
        showSuccess(t('userPage.operationSuccess'));
        if (action === 'delete') {
          await handleRefresh();
        }
      } else {
        showError(message);
      }

      return res.data;
    } catch (error) {
      return;
    }
  };

  // 筛选和分页
  const filteredRows = useMemo(() => {
    return rows.filter((row) => {
      // 搜索过滤 - 支持模型名称、渠道类型搜索
      const searchMatch =
        searchTerm === '' ||
        row.models.some((model) => model.toLowerCase().includes(searchTerm.toLowerCase())) ||
        ownedby
          .find((o) => o.value === row.channel_type)
          ?.label.toLowerCase()
          .includes(searchTerm.toLowerCase());

      // 类型过滤
      let typeMatch = true;
      if (filterType !== 'all') {
        typeMatch = row.type === filterType;
      }

      // 渠道过滤
      let channelMatch = true;
      if (channelFilter !== 'all') {
        channelMatch = row.channel_type === channelFilter;
      }

      // 锁定状态过滤
      let lockMatch = true;
      if (lockFilter !== 'all') {
        lockMatch = row.locked === (lockFilter === 'locked');
      }

      return searchMatch && typeMatch && channelMatch && lockMatch;
    });
  }, [rows, searchTerm, filterType, channelFilter, lockFilter, ownedby]);

  const paginatedRows = useMemo(() => {
    const startIndex = page * rowsPerPage;
    return filteredRows.slice(startIndex, startIndex + rowsPerPage);
  }, [filteredRows, page, rowsPerPage]);

  const handleChangePage = (event, newPage) => {
    setPage(newPage);
  };

  const handleChangeRowsPerPage = (event) => {
    const newPageSize = parseInt(event.target.value, 10);
    setPage(0);
    setRowsPerPage(newPageSize);
    savePageSize('pricing_multiple', newPageSize);
  };

  // 当搜索词变化时重置到第一页
  useEffect(() => {
    setPage(0);
  }, [searchTerm, filterType, channelFilter, lockFilter]);

  return (
    <>
      {/* 工具栏 */}
      <Paper
        elevation={0}
        variant="outlined"
        sx={{
          p: 2,
          mb: 2,
          borderRadius: 1
        }}
      >
        <Box
          sx={{
            display: 'flex',
            flexWrap: 'wrap',
            alignItems: 'center',
            gap: 2,
            justifyContent: 'space-between'
          }}
        >
          {/* 搜索栏 */}
          <TextField
            placeholder={t('common.search')}
            value={searchTerm}
            onChange={(e) => setSearchTerm(e.target.value)}
            size="small"
            sx={{ width: { xs: '100%', sm: 280 } }}
            InputProps={{
              startAdornment: (
                <InputAdornment position="start">
                  <SearchIcon fontSize="small" />
                </InputAdornment>
              ),
              endAdornment: searchTerm && (
                <InputAdornment position="end">
                  <IconButton edge="end" onClick={() => setSearchTerm('')} size="small">
                    <Icon icon="mdi:close" width={16} />
                  </IconButton>
                </InputAdornment>
              )
            }}
          />

          {/* 过滤和分页控制 */}
          <Box
            sx={{
              display: 'flex',
              flexWrap: 'wrap',
              alignItems: 'center',
              gap: 1,
              width: { xs: '100%', sm: 'auto' }
            }}
          >
            <FormControl size="small" sx={{ minWidth: 120 }}>
              <InputLabel>{t('pricing_edit.type')}</InputLabel>
              <Select value={filterType} onChange={(e) => setFilterType(e.target.value)} label={t('pricing_edit.type')}>
                <MenuItem value="all">{t('modelpricePage.all')}</MenuItem>
                <MenuItem value="tokens">{t('modelpricePage.tokens')}</MenuItem>
                <MenuItem value="times">{t('modelpricePage.times')}</MenuItem>
              </Select>
            </FormControl>

            <FormControl size="small" sx={{ minWidth: 120 }}>
              <InputLabel>{t('modelpricePage.channelType')}</InputLabel>
              <Select value={channelFilter} onChange={(e) => setChannelFilter(e.target.value)} label={t('modelpricePage.channelType')}>
                <MenuItem value="all">{t('modelpricePage.all')}</MenuItem>
                {ownedby.map((channel) => (
                  <MenuItem key={channel.value} value={channel.value}>
                    {channel.label}
                  </MenuItem>
                ))}
              </Select>
            </FormControl>

            <FormControl size="small" sx={{ minWidth: 120 }}>
              <InputLabel>{t('pricing_edit.locked_title')}</InputLabel>
              <Select value={lockFilter} onChange={(e) => setLockFilter(e.target.value)} label={t('pricing_edit.locked_title')}>
                <MenuItem value="all">{t('modelpricePage.all')}</MenuItem>
                <MenuItem value="locked">{t('pricing_edit.locked')}</MenuItem>
                <MenuItem value="unlocked">{t('pricing_edit.unlocked')}</MenuItem>
              </Select>
            </FormControl>

            <ToggleButtonGroup value={unit} onChange={handleUnitChange} options={unitOptions} aria-label="unit toggle" />
          </Box>
        </Box>
      </Paper>

      {/* 数据表格 */}
      <Card>
        <PerfectScrollbar component="div" containerRef={stickyShadowRef}>
          <TableContainer sx={{ overflow: 'unset' }}>
            <Table
              sx={{
                minWidth: 800,
                tableLayout: 'fixed',
                '& th:first-of-type': {
                  width: 50,
                  maxWidth: 50,
                  minWidth: 50
                },
                '& td:first-of-type': {
                  width: 50,
                  maxWidth: 50,
                  minWidth: 50
                }
              }}
            >
              <KeywordTableHead
                headLabel={[
                  { id: 'type_channel', label: t('modelpricePage.type'), width: '20%', align: 'left' },
                  { id: 'price', label: t('modelpricePage.price'), width: '15%', align: 'center' },
                  { id: 'models', label: t('modelpricePage.model'), width: '30%', align: 'left' },
                  { id: 'extra_ratios', label: t('modelpricePage.extraRatios'), width: '25%', align: 'left' },
                  { id: 'action', label: t('common.actions'), width: '10%', align: 'right', sticky: true }
                ]}
              />
              <TableBody>
                {paginatedRows.map((row) => (
                  <PricesTableRow
                    item={row}
                    key={row.id}
                    onEdit={handleEditClick}
                    onDelete={handleDeleteClick}
                    ownedby={ownedby}
                    unit={unit}
                  />
                ))}
              </TableBody>
            </Table>
          </TableContainer>
        </PerfectScrollbar>

        {/* 分页 */}
        <TablePagination
          page={page}
          component="div"
          count={filteredRows.length}
          rowsPerPage={rowsPerPage}
          onPageChange={handleChangePage}
          rowsPerPageOptions={PAGE_SIZE_OPTIONS}
          onRowsPerPageChange={handleChangeRowsPerPage}
          showFirstButton
          showLastButton
        />
      </Card>

      {/* 删除确认对话框 */}
      <Dialog open={!!selectedRow} onClose={handleClose}>
        <DialogTitle>{t('pricing_edit.delTip')}</DialogTitle>
        <DialogContent dividers>{t('pricing_edit.delMultipleInfoTip', { count: selectedRow?.models?.length || 0 })}</DialogContent>
        <DialogActions>
          <Button onClick={handleClose}>{t('common.cancel')}</Button>
          <Button onClick={handleConfirmDelete} color="error">
            {t('common.delete')}
          </Button>
        </DialogActions>
      </Dialog>

      {/* 编辑对话框 */}
      <EditModal
        open={editRow !== null}
        onCancel={handleEditClose}
        onOk={handleModalOK}
        ownedby={ownedby}
        singleMode={false}
        pricesItem={editRow}
        rows={rows}
        unit={unit}
        noPriceModel={noPriceModels}
      />
    </>
  );
}

Multiple.propTypes = {
  prices: PropTypes.array,
  ownedby: PropTypes.array,
  reloadData: PropTypes.func,
  noPriceModels: PropTypes.array
};
