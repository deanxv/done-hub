import { useCallback, useEffect, useRef, useState } from 'react'
import { renderQuota, showError, showSuccess, trims, useIsAdmin } from 'utils/common'

import Table from '@mui/material/Table'
import TableBody from '@mui/material/TableBody'
import TableContainer from '@mui/material/TableContainer'
import PerfectScrollbar from 'react-perfect-scrollbar'
import TablePagination from '@mui/material/TablePagination'
import LinearProgress from '@mui/material/LinearProgress'
import ButtonGroup from '@mui/material/ButtonGroup'
import Toolbar from '@mui/material/Toolbar'
import IconButton from '@mui/material/IconButton'
import Divider from '@mui/material/Divider'
import {
  Box,
  Button,
  Card,
  Checkbox,
  Container,
  ListItemText,
  Menu,
  MenuItem,
  Skeleton,
  Stack,
  Tab,
  Tabs,
  Typography
} from '@mui/material'
import LogTableRow from './component/TableRow'
import KeywordTableHead from 'ui-component/TableHead'
import TableToolBar from './component/TableToolBar'
import { API } from 'utils/api'
import { getPageSize, PAGE_SIZE_OPTIONS, savePageSize } from 'constants'
import { Icon } from '@iconify/react'
import dayjs from 'dayjs'
import { useTranslation } from 'react-i18next'
import useMediaQuery from '@mui/material/useMediaQuery'
import { useTheme } from '@mui/material/styles'
import { useSelector } from 'react-redux'
import { useLogType } from './type/LogType'

// 「全部」和「消费」两个 Tab 才显示总消费汇总
const isCostLogType = (v) => v === '0' || v === '2'

export default function Log() {
  const { t } = useTranslation()
  const LogType = useLogType()
  const originalKeyword = {
    p: 0,
    username: '',
    token_name: '',
    model_name: '',
    start_timestamp: dayjs().startOf('day').unix(),
    end_timestamp: dayjs().unix() + 3600,
    log_type: '0',
    channel_id: '',
    source_ip: ''
  }

  const [page, setPage] = useState(0)
  const [order, setOrder] = useState('desc')
  const [orderBy, setOrderBy] = useState('created_at')
  const [rowsPerPage, setRowsPerPage] = useState(() => getPageSize('log'))
  const [listCount, setListCount] = useState(0)
  const [searching, setSearching] = useState(false)
  const [toolBarValue, setToolBarValue] = useState(originalKeyword)
  const [searchKeyword, setSearchKeyword] = useState(originalKeyword)
  const [refreshFlag, setRefreshFlag] = useState(false)
  const { userGroup } = useSelector((state) => state.account)
  const theme = useTheme()
  const matchUpMd = useMediaQuery(theme.breakpoints.up('sm'))

  const [logs, setLogs] = useState([])
  // null = 未加载 / 加载中（渲染为 '—'）；数字 = 已加载汇总值
  const [totalQuota, setTotalQuota] = useState(null)
  const statReqIdRef = useRef(0)
  const userIsAdmin = useIsAdmin()

  // 取自 searchKeyword 而非 toolBarValue：与实际触发查询的数据源对齐，
  // 避免未来如果工具栏改 log_type 不立即触发搜索时显示与列表脱节
  const showTotalCost = isCostLogType(searchKeyword.log_type)

  // 添加列显示设置相关状态
  const [columnVisibility, setColumnVisibility] = useState({
    created_at: true,
    channel_id: true,
    user_id: true,
    group: true,
    token_name: true,
    type: true,
    model_name: true,
    duration: true,
    message: true,
    completion: true,
    quota: true,
    source_ip: true,
    detail: true
  })
  const [columnMenuAnchor, setColumnMenuAnchor] = useState(null)

  // 处理列显示菜单打开和关闭
  const handleColumnMenuOpen = (event) => {
    setColumnMenuAnchor(event.currentTarget)
  }

  const handleColumnMenuClose = () => {
    setColumnMenuAnchor(null)
  }

  // 处理列显示状态变更
  const handleColumnVisibilityChange = (columnId) => {
    setColumnVisibility({
      ...columnVisibility,
      [columnId]: !columnVisibility[columnId]
    })
  }

  // 处理全选/取消全选列显示
  const handleSelectAllColumns = () => {
    const allColumns = Object.keys(columnVisibility)
    const areAllVisible = allColumns.every((column) => columnVisibility[column])

    const newColumnVisibility = {}
    allColumns.forEach((column) => {
      newColumnVisibility[column] = !areAllVisible
    })

    setColumnVisibility(newColumnVisibility)
  }

  const handleSort = (event, id) => {
    const isAsc = orderBy === id && order === 'asc'
    if (id !== '') {
      setOrder(isAsc ? 'desc' : 'asc')
      setOrderBy(id)
    }
  }

  const handleChangePage = (event, newPage) => {
    setPage(newPage)
  }

  const handleChangeRowsPerPage = (event) => {
    const newRowsPerPage = parseInt(event.target.value, 10)
    setPage(0)
    setRowsPerPage(newRowsPerPage)
    savePageSize('log', newRowsPerPage)
  }

  const searchLogs = async() => {
    // 如果正在搜索中，防止重复提交
    if (searching) {
      return
    }

    setPage(0)
    // 使用时间戳来确保即使搜索条件相同也能触发重新搜索
    const searchPayload = {
      ...toolBarValue,
      _timestamp: Date.now()
    }
    setSearchKeyword(searchPayload)
  }

  const handleToolBarValue = (event) => {
    setToolBarValue({ ...toolBarValue, [event.target.name]: event.target.value })
  }

  const handleTabsChange = async(event, newValue) => {
    const updatedToolBarValue = { ...toolBarValue, log_type: newValue }
    setToolBarValue(updatedToolBarValue)
    setPage(0)
    setSearchKeyword(updatedToolBarValue)
  }

  const fetchData = useCallback(
    async(page, rowsPerPage, keyword, order, orderBy) => {
      setSearching(true)
      keyword = trims(keyword)

      // 移除仅用于触发状态更新的时间戳字段
      if (keyword._timestamp) {
        delete keyword._timestamp
      }

      try {
        if (orderBy) {
          orderBy = order === 'desc' ? '-' + orderBy : orderBy
        }
        const url = userIsAdmin ? '/api/log/' : '/api/log/self/'
        if (!userIsAdmin) {
          delete keyword.username
          delete keyword.channel_id
        }

        const res = await API.get(url, {
          params: {
            page: page + 1,
            size: rowsPerPage,
            order: orderBy,
            ...keyword
          }
        })
        const { success, message, data } = res.data
        if (success) {
          setListCount(data.total_count)
          setLogs(data.data)
        } else {
          showError(message)
        }
      } catch (error) {
        console.error(error)
      }
      setSearching(false)
    },
    [userIsAdmin]
  )

  const fetchStat = useCallback(
    async(keyword) => {
      const cleaned = trims(keyword)
      if (cleaned._timestamp) delete cleaned._timestamp
      if (!isCostLogType(cleaned.log_type)) {
        setTotalQuota(null)
        return
      }
      if (!userIsAdmin) {
        delete cleaned.username
        delete cleaned.channel_id
      }

      // 请求前清零（避免显示旧值），同时用 reqId 防 race：
      // 快速切 Tab / 连点搜索时，仅最后一次发出的请求结果会被采纳
      const reqId = ++statReqIdRef.current
      setTotalQuota(null)
      try {
        const url = userIsAdmin ? '/api/log/stat' : '/api/log/self/stat'
        const res = await API.get(url, { params: cleaned })
        if (reqId !== statReqIdRef.current) return
        if (res.data?.success) {
          setTotalQuota(res.data.data?.quota || 0)
        } else {
          // 业务失败也要终结 Skeleton，避免用户面对永久骨架
          setTotalQuota(0)
        }
      } catch (error) {
        if (reqId !== statReqIdRef.current) return
        setTotalQuota(0)
        console.error(error)
      }
    },
    [userIsAdmin]
  )

  // 处理刷新
  const handleRefresh = async() => {
    setOrderBy('created_at')
    setOrder('desc')
    setToolBarValue(originalKeyword)
    setSearchKeyword(originalKeyword)
    setRefreshFlag(!refreshFlag)
  }

  // 导出状态
  const [exporting, setExporting] = useState(false)

  // 处理导出
  const handleExport = useCallback(async () => {
    if (exporting) return // 防止重复点击

    setExporting(true)
    try {
      const exportKeyword = trims(searchKeyword)

      // 移除仅用于触发状态更新的时间戳字段
      if (exportKeyword._timestamp) {
        delete exportKeyword._timestamp
      }

      let orderBy_export = orderBy
      if (orderBy_export) {
        orderBy_export = order === 'desc' ? '-' + orderBy_export : orderBy_export
      }

      const url = userIsAdmin ? '/api/log/export' : '/api/log/self/export'
      const params = {
        order: orderBy_export,
        ...exportKeyword
      }

      if (!userIsAdmin) {
        delete params.username
        delete params.channel_id
      }

      // 使用fetch进行同步请求，提供更好的错误处理
      const queryString = new URLSearchParams(params).toString()
      const downloadUrl = `${url}?${queryString}`

      const response = await fetch(downloadUrl, {
        method: 'GET',
        headers: {
          'Authorization': localStorage.getItem('token') ? `Bearer ${localStorage.getItem('token')}` : ''
        }
      })

      if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`)
      }

      // 获取文件名
      const contentDisposition = response.headers.get('Content-Disposition')
      let filename = `logs_export_${new Date().toISOString().slice(0, 19).replace(/:/g, '-')}.csv`
      if (contentDisposition) {
        const filenameMatch = contentDisposition.match(/filename=(.+)/)
        if (filenameMatch) {
          filename = filenameMatch[1]
        }
      }

      // 创建blob并下载
      const blob = await response.blob()
      const link = document.createElement('a')
      link.href = window.URL.createObjectURL(blob)
      link.download = filename
      document.body.appendChild(link)
      link.click()
      document.body.removeChild(link)
      window.URL.revokeObjectURL(link.href)

      showSuccess(t('logPage.exportSuccess'))
    } catch (error) {
      console.error('Export error:', error)
      showError(t('logPage.exportError') + ': ' + error.message)
    } finally {
      setExporting(false)
    }
  }, [searchKeyword, order, orderBy, userIsAdmin, t, exporting])

  useEffect(() => {
    fetchData(page, rowsPerPage, searchKeyword, order, orderBy)
  }, [page, rowsPerPage, searchKeyword, order, orderBy, fetchData, refreshFlag])

  useEffect(() => {
    fetchStat(searchKeyword)
  }, [searchKeyword, refreshFlag, fetchStat])

  return (
    <>
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
          <Typography variant="h2">{t('logPage.title')}</Typography>
          <Typography variant="subtitle1" color="text.secondary">
            Log
          </Typography>
        </Stack>
      </Stack>
      <Card>
        <Box sx={{ borderBottom: 1, borderColor: 'divider' }}>
          <Tabs
            value={toolBarValue.log_type}
            onChange={handleTabsChange}
            aria-label="basic tabs example"
            variant="scrollable"
            scrollButtons="auto"
            allowScrollButtonsMobile
            sx={{
              '& .MuiTabs-indicator': {
                display: 'none'
              }
            }}
          >
            {Object.values(LogType).map((option) => {
              return <Tab key={option.value} label={option.text} value={option.value}/>
            })}
          </Tabs>
        </Box>
        <Box component="form" noValidate>
          <TableToolBar filterName={toolBarValue} handleFilterName={handleToolBarValue} userIsAdmin={userIsAdmin}
                        onSearch={searchLogs}/>
        </Box>
        <Toolbar
          sx={{
            textAlign: 'right',
            height: 50,
            display: 'flex',
            justifyContent: 'space-between',
            p: (theme) => theme.spacing(0, 1, 0, 3)
          }}
        >
          <Container maxWidth="xl">
            {matchUpMd ? (
              <ButtonGroup variant="outlined" aria-label="outlined small primary button group">
                <Button onClick={handleRefresh} startIcon={<Icon icon="solar:refresh-circle-bold-duotone" width={18}/>}>
                  {t('logPage.refreshButton')}
                </Button>

                <Button
                  onClick={searchLogs}
                  size="small"
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
                      <Icon icon="solar:minimalistic-magnifer-line-duotone" width={18}/>
                    )
                  }
                  sx={{
                    ...(searching && {
                      bgcolor: 'action.hover',
                      color: 'primary.main',
                      '&:hover': {
                        bgcolor: 'action.selected'
                      }
                    })
                  }}
                >
                  {searching ? t('logPage.searching') : t('logPage.searchButton')}
                </Button>

                <Button
                  onClick={exporting ? undefined : handleExport}
                  size="small"
                  startIcon={
                    exporting ? (
                      <Icon
                        icon="solar:refresh-bold-duotone"
                        width={18}
                        style={{
                          animation: 'spin 1s linear infinite',
                          color: '#1976d2'
                        }}
                      />
                    ) : (
                      <Icon icon="solar:download-bold-duotone" width={18}/>
                    )
                  }
                  sx={{
                    ...(exporting && {
                      bgcolor: 'action.hover',
                      color: 'primary.main',
                      '&:hover': {
                        bgcolor: 'action.selected'
                      }
                    })
                  }}
                >
                  {exporting ? t('logPage.exporting') : t('logPage.exportButton')}
                </Button>

                <Button onClick={handleColumnMenuOpen} size="small"
                        startIcon={<Icon icon="solar:settings-bold-duotone" width={18}/>}>
                  {t('logPage.columnSettings')}
                </Button>
              </ButtonGroup>
            ) : (
              <Stack
                direction="row"
                spacing={1}
                divider={<Divider orientation="vertical" flexItem/>}
                justifyContent="space-around"
                alignItems="center"
              >
                <IconButton onClick={handleRefresh} size="small">
                  <Icon icon="solar:refresh-circle-bold-duotone" width={18}/>
                </IconButton>
                <IconButton
                  onClick={searchLogs}
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
                    <Icon icon="solar:minimalistic-magnifer-line-duotone" width={18}/>
                  )}
                </IconButton>
                <IconButton
                  onClick={exporting ? undefined : handleExport}
                  size="small"
                  sx={{
                    ...(exporting && {
                      bgcolor: 'action.hover',
                      color: 'primary.main'
                    })
                  }}
                >
                  {exporting ? (
                    <Icon
                      icon="solar:refresh-bold-duotone"
                      width={18}
                      style={{
                        animation: 'spin 1s linear infinite',
                        color: '#1976d2'
                      }}
                    />
                  ) : (
                    <Icon icon="solar:download-bold-duotone" width={18}/>
                  )}
                </IconButton>
                <IconButton onClick={handleColumnMenuOpen} size="small">
                  <Icon icon="solar:settings-bold-duotone" width={18}/>
                </IconButton>
              </Stack>
            )}

            <Menu
              anchorEl={columnMenuAnchor}
              open={Boolean(columnMenuAnchor)}
              onClose={handleColumnMenuClose}
              PaperProps={{
                style: {
                  maxHeight: 300,
                  width: 200
                }
              }}
            >
              <MenuItem disabled>
                <Typography variant="subtitle2">{t('logPage.selectColumns')}</Typography>
              </MenuItem>
              <MenuItem onClick={handleSelectAllColumns} dense>
                <Checkbox
                  checked={Object.values(columnVisibility).every((visible) => visible)}
                  indeterminate={
                    !Object.values(columnVisibility).every((visible) => visible) &&
                    Object.values(columnVisibility).some((visible) => visible)
                  }
                  size="small"
                />
                <ListItemText primary={t('logPage.columnSelectAll')}/>
              </MenuItem>
              {[
                { id: 'created_at', label: t('logPage.timeLabel') },
                { id: 'channel_id', label: t('logPage.channelLabel'), adminOnly: true },
                { id: 'user_id', label: t('logPage.userLabel'), adminOnly: true },
                { id: 'group', label: t('logPage.groupLabel') },
                { id: 'token_name', label: t('logPage.tokenLabel') },
                { id: 'type', label: t('logPage.typeLabel') },
                { id: 'model_name', label: t('logPage.modelLabel') },
                { id: 'duration', label: t('logPage.durationLabel') },
                { id: 'message', label: t('logPage.inputLabel') },
                { id: 'completion', label: t('logPage.outputLabel') },
                { id: 'quota', label: t('logPage.quotaLabel') },
                { id: 'source_ip', label: t('logPage.sourceIp') },
                { id: 'detail', label: t('logPage.detailLabel') }
              ].map(
                (column) =>
                  (!column.adminOnly || userIsAdmin) && (
                    <MenuItem key={column.id} onClick={() => handleColumnVisibilityChange(column.id)} dense>
                      <Checkbox checked={columnVisibility[column.id] || false} size="small"/>
                      <ListItemText primary={column.label}/>
                    </MenuItem>
                  )
              )}
            </Menu>
          </Container>
        </Toolbar>
        {searching && <LinearProgress/>}
        <PerfectScrollbar component="div">
          <TableContainer sx={{ overflow: 'unset' }}>
            <Table sx={{ minWidth: 800 }}>
              <KeywordTableHead
                order={order}
                orderBy={orderBy}
                onRequestSort={handleSort}
                headLabel={[
                  {
                    id: 'created_at',
                    label: t('logPage.timeLabel'),
                    disableSort: false,
                    hide: !columnVisibility.created_at
                  },
                  {
                    id: 'channel_id',
                    label: t('logPage.channelLabel'),
                    disableSort: false,
                    hide: !columnVisibility.channel_id || !userIsAdmin
                  },
                  {
                    id: 'user_id',
                    label: t('logPage.userLabel'),
                    disableSort: false,
                    hide: !columnVisibility.user_id || !userIsAdmin
                  },
                  {
                    id: 'group',
                    label: t('logPage.groupLabel'),
                    disableSort: false,
                    hide: !columnVisibility.group
                  },
                  {
                    id: 'token_name',
                    label: t('logPage.tokenLabel'),
                    disableSort: false,
                    hide: !columnVisibility.token_name
                  },
                  {
                    id: 'type',
                    label: t('logPage.typeLabel'),
                    disableSort: false,
                    hide: !columnVisibility.type
                  },
                  {
                    id: 'model_name',
                    label: t('logPage.modelLabel'),
                    disableSort: false,
                    hide: !columnVisibility.model_name
                  },
                  {
                    id: 'duration',
                    label: t('logPage.durationLabel'),
                    tooltip: t('logPage.durationTooltip'),
                    disableSort: true,
                    hide: !columnVisibility.duration
                  },
                  {
                    id: 'message',
                    label: t('logPage.inputLabel'),
                    disableSort: true,
                    hide: !columnVisibility.message
                  },
                  {
                    id: 'completion',
                    label: t('logPage.outputLabel'),
                    disableSort: true,
                    hide: !columnVisibility.completion
                  },
                  {
                    id: 'quota',
                    label: t('logPage.quotaLabel'),
                    disableSort: true,
                    hide: !columnVisibility.quota
                  },
                  {
                    id: 'source_ip',
                    label: t('logPage.sourceIp'),
                    disableSort: true,
                    hide: !columnVisibility.source_ip
                  },
                  {
                    id: 'detail',
                    label: t('logPage.detailLabel'),
                    disableSort: true,
                    hide: !columnVisibility.detail,
                    sticky: true
                  }
                ]}
              />
              <TableBody>
                {logs.map((row, index) => (
                  <LogTableRow
                    item={row}
                    key={`${row.id}_${index}`}
                    userIsAdmin={userIsAdmin}
                    userGroup={userGroup}
                    columnVisibility={columnVisibility}
                  />
                ))}
              </TableBody>
            </Table>
          </TableContainer>
        </PerfectScrollbar>
        <Box
          sx={(theme) => ({
            display: 'flex',
            flexDirection: { xs: 'column', sm: 'row' },
            alignItems: { xs: 'stretch', sm: 'center' },
            rowGap: 1,
            minHeight: { xs: 'auto', sm: 56 },
            px: { xs: 2, sm: 3 },
            py: { xs: 1, sm: 0 },
            bgcolor: theme.headBackgroundColor,
            borderTop: `1px dashed ${theme.tableBorderBottom}`
          })}
        >
          {showTotalCost && (
            <Stack
              direction="row"
              alignItems="center"
              spacing={0.75}
              sx={{ width: { xs: '100%', sm: 'auto' }, flexShrink: 0 }}
            >
              <Icon
                icon="solar:dollar-minimalistic-bold-duotone"
                width={16}
                color={theme.palette.primary.main}
              />
              <Typography variant="body2" color="text.secondary">
                {t('logPage.totalCost')}
              </Typography>
              <Box sx={{ flexGrow: { xs: 1, sm: 0 } }}/>
              <Box
                sx={{
                  minWidth: 80,
                  display: 'inline-flex',
                  alignItems: 'center',
                  justifyContent: { xs: 'flex-end', sm: 'flex-start' }
                }}
              >
                {totalQuota === null ? (
                  <Skeleton variant="text" width="100%" sx={{ fontSize: '0.875rem' }}/>
                ) : (
                  <Typography variant="body2" sx={{ color: 'primary.main', fontWeight: 700 }}>
                    {renderQuota(totalQuota, 2)}
                  </Typography>
                )}
              </Box>
            </Stack>
          )}
          <TablePagination
            sx={{
              '&.MuiTablePagination-root': {
                ml: { xs: 0, sm: 'auto' },
                width: { xs: '100%', sm: 'auto' },
                minHeight: 'auto',
                bgcolor: 'transparent',
                borderTop: 0,
                padding: 0
              }
            }}
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
        </Box>
      </Card>
    </>
  )
}
