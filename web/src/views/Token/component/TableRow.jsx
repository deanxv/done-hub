import PropTypes from 'prop-types'
import { useEffect, useState } from 'react'
import { useSelector } from 'react-redux'

import { Box, Button, IconButton, Stack, TableCell, TableRow, Tooltip } from '@mui/material'

import RatioBadge from 'ui-component/RatioBadge'

import TableSwitch from 'ui-component/Switch'
import ConfirmDialog from 'ui-component/confirm-dialog'
import { copy, renderQuota, timestamp2string } from 'utils/common'
import Label from 'ui-component/Label'

import { Icon } from '@iconify/react'
import { useTranslation } from 'react-i18next'
import { stickyCellSx } from 'ui-component/stickyCellSx'

function statusInfo(t, status) {
  switch (status) {
    case 1:
      return t('common.enable')
    case 2:
      return t('common.disable')
    case 3:
      return t('common.expired')
    case 4:
      return t('common.exhaust')
    default:
      return t('common.unknown')
  }
}

function maskTokenKey(fullKey) {
  if (!fullKey) return ''
  if (fullKey.length <= 12) return fullKey
  return `${fullKey.slice(0, 6)}****${fullKey.slice(-6)}`
}

export default function TokensTableRow({ item, manageToken, handleOpenModal, setModalTokenId, userGroup, userIsReliable, isAdminSearch }) {
  const { t } = useTranslation()
  const [openDelete, setOpenDelete] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [statusSwitch, setStatusSwitch] = useState(item.status)
  const [keyVisible, setKeyVisible] = useState(false)

  // 非 admin 搜索时，列表里所有 token 都属于当前登录用户，「跟随用户」的实际倍率 = 用户当前分组的倍率
  const user = useSelector((state) => state.account.user)
  const followingRatio = !isAdminSearch && user?.group ? userGroup?.[user.group]?.ratio : undefined
  const fullKey = `sk-${item.key}`

  const renderGroupCell = (symbol, fallback, fallbackRatio) => {
    let label
    let ratio
    if (!symbol) {
      label = <Label color="default">{fallback}</Label>
      ratio = fallbackRatio
    } else {
      const g = userGroup[symbol]
      if (!g) {
        label = <Label color="error">{symbol} (不存在)</Label>
      } else {
        label = g.inaccessible
          ? <Label color="error">{g.name} (不可用)</Label>
          : <Label color={g.color}>{g.name}</Label>
        ratio = g.ratio
      }
    }
    return (
      <Stack direction="row" alignItems="center" justifyContent="center" spacing={0.5}>
        {label}
        {ratio !== undefined && ratio !== null && <RatioBadge ratio={ratio}/>}
      </Stack>
    )
  }

  const handleDeleteOpen = () => {
    setOpenDelete(true)
  }

  const handleDeleteClose = () => {
    setOpenDelete(false)
  }

  const handleStatus = async() => {
    const switchVlue = statusSwitch === 1 ? 2 : 1
    const { success } = await manageToken(item.id, 'status', switchVlue)
    if (success) {
      setStatusSwitch(switchVlue)
    }
  }

  const handleDelete = async() => {
    if (deleting) return

    setDeleting(true)
    try {
      await manageToken(item.id, 'delete', '')
    } finally {
      setDeleting(false)
      setOpenDelete(false)
    }
  }

  useEffect(() => {
    setStatusSwitch(item.status)
  }, [item.status])

  return (
    <>
      <TableRow tabIndex={item.id}>
        {isAdminSearch && (
          <TableCell>
            <Tooltip title={`ID: ${item.user_id}`} placement="top">
              <span>{item.user_id} - {item.owner_name || '-'}</span>
            </Tooltip>
          </TableCell>
        )}
        <TableCell>{item.name}</TableCell>
        <TableCell sx={{ whiteSpace: 'nowrap' }}>
          <Stack direction="row" alignItems="center" spacing={0.5}>
            <Box
              component="code"
              sx={{
                fontFamily: 'monospace',
                fontSize: '0.75rem',
                px: 0.75,
                py: 0.25,
                bgcolor: 'action.hover',
                borderRadius: 0.5,
                userSelect: keyVisible ? 'all' : 'none',
                wordBreak: keyVisible ? 'break-all' : 'normal'
              }}
            >
              {keyVisible ? fullKey : maskTokenKey(fullKey)}
            </Box>
            <Tooltip title={keyVisible ? t('token_index.hideKey') : t('token_index.showKey')} placement="top" arrow>
              <IconButton size="small" sx={{ p: 0.25 }} onClick={() => setKeyVisible((v) => !v)}>
                <Icon icon={keyVisible ? 'solar:eye-closed-bold-duotone' : 'solar:eye-bold-duotone'} width={16}/>
              </IconButton>
            </Tooltip>
            <Tooltip title={t('token_index.copy')} placement="top" arrow>
              <IconButton
                size="small"
                sx={{ p: 0.25, color: 'primary.main' }}
                onClick={() => copy(fullKey, t('token_index.token'))}
              >
                <Icon icon="solar:copy-bold-duotone" width={16}/>
              </IconButton>
            </Tooltip>
          </Stack>
        </TableCell>
        <TableCell>
          {isAdminSearch ? (
            <Stack direction="column" spacing={0.5} alignItems="flex-start">
              {renderGroupCell(item.group, '跟随用户')}
              {renderGroupCell(item.backup_group, '-')}
            </Stack>
          ) : (
            renderGroupCell(item.group, '跟随用户', followingRatio)
          )}
        </TableCell>
        {userIsReliable && (
          <TableCell>
            {renderGroupCell(item.setting?.billing_tag, '-')}
          </TableCell>
        )}

        <TableCell>
          <Tooltip
            title={(() => {
              return statusInfo(t, statusSwitch)
            })()}
            placement="top"
          >
            <TableSwitch
              id={`switch-${item.id}`}
              checked={statusSwitch === 1}
              onChange={handleStatus}
              // disabled={statusSwitch !== 1 && statusSwitch !== 2}
            />
          </Tooltip>
        </TableCell>

        {isAdminSearch ? (
          <TableCell>
            <Stack direction="column" spacing={0.5}>
              <span>{renderQuota(item.used_quota)}</span>
              <span style={{ color: 'text.secondary' }}>{item.unlimited_quota ? t('token_index.unlimited') : renderQuota(item.remain_quota, 2)}</span>
            </Stack>
          </TableCell>
        ) : (
          <>
            <TableCell>{renderQuota(item.used_quota)}</TableCell>
            <TableCell>{item.unlimited_quota ? t('token_index.unlimited') : renderQuota(item.remain_quota, 2)}</TableCell>
          </>
        )}

        {isAdminSearch ? (
          <TableCell sx={{ whiteSpace: 'nowrap' }}>
            <Stack direction="column" spacing={0.5}>
              <span>{timestamp2string(item.created_time)}</span>
              <span style={{ color: 'text.secondary' }}>{item.expired_time === -1 ? t('token_index.neverExpires') : timestamp2string(item.expired_time)}</span>
            </Stack>
          </TableCell>
        ) : (
          <>
            <TableCell sx={{ whiteSpace: 'nowrap' }}>{timestamp2string(item.created_time)}</TableCell>
            <TableCell sx={{ whiteSpace: 'nowrap' }}>{item.expired_time === -1 ? t('token_index.neverExpires') : timestamp2string(item.expired_time)}</TableCell>
          </>
        )}

        {isAdminSearch && (
          <TableCell sx={{ whiteSpace: 'nowrap' }}>
            {item.accessed_time ? timestamp2string(item.accessed_time) : '-'}
          </TableCell>
        )}

        <TableCell
          sx={stickyCellSx}
        >
          <Stack direction="row" justifyContent="center" alignItems="center" spacing={0.5}>
            <Tooltip title={t('common.edit')} placement="top" arrow>
              <IconButton
                size="small"
                onClick={() => {
                  handleOpenModal()
                  setModalTokenId(item.id)
                }}
              >
                <Icon icon="solar:pen-bold-duotone" width={20}/>
              </IconButton>
            </Tooltip>
            <Tooltip title={t('common.delete')} placement="top" arrow>
              <IconButton
                size="small"
                sx={{ color: 'error.main' }}
                onClick={handleDeleteOpen}
              >
                <Icon icon="solar:trash-bin-trash-bold-duotone" width={20}/>
              </IconButton>
            </Tooltip>
          </Stack>
        </TableCell>
      </TableRow>

      <ConfirmDialog
        open={openDelete}
        onClose={handleDeleteClose}
        title={t('common.delete')}
        content={t('common.deleteConfirm', { title: `Token "${item.name}"` })}
        action={
          <Button
            variant="contained"
            color="error"
            onClick={handleDelete}
            disabled={deleting}
          >
            {deleting ? '删除中...' : t('token_index.delete')}
          </Button>
        }
      />
    </>
  )
}

TokensTableRow.propTypes = {
  item: PropTypes.object,
  manageToken: PropTypes.func,
  handleOpenModal: PropTypes.func,
  setModalTokenId: PropTypes.func,
  userGroup: PropTypes.object,
  userIsReliable: PropTypes.bool,
  isAdminSearch: PropTypes.bool
}
