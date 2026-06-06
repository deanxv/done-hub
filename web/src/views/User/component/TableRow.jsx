import PropTypes from 'prop-types'
import { useState } from 'react'

import {
  Button,
  IconButton,
  MenuItem,
  Popover,
  Stack,
  TableCell,
  TableRow,
  TextField,
  Tooltip
} from '@mui/material'

import Label from 'ui-component/Label'
import TableSwitch from 'ui-component/Switch'
import QuotaInput, { QUOTA_UNIT_CURRENCY } from 'ui-component/QuotaInput'
import { calculateQuota, renderNumber, renderQuota, showError, timestamp2string } from 'utils/common'
import { Icon } from '@iconify/react'
import { useTheme } from '@mui/material/styles'
import { useTranslation } from 'react-i18next'
import ConfirmDialog from 'ui-component/confirm-dialog'
import LinuxDoIcon from 'assets/images/icons/LinuxDoIcon'
import { stickyCellSx } from 'ui-component/stickyCellSx'

function renderRole(t, role) {
  switch (role) {
    case 1:
      return <Label color="default">{t('userPage.cUserRole')}</Label>
    case 3:
      return <Label color="primary">{t('userPage.reliableUserRole')}</Label>
    case 10:
      return <Label color="orange">{t('userPage.adminUserRole')}</Label>
    case 100:
      return <Label color="success">{t('userPage.superAdminRole')}</Label>
    default:
      return <Label color="error">{t('userPage.uUserRole')}</Label>
  }
}

export default function UsersTableRow({ item, manageUser, handleOpenModal, setModalUserId }) {
  const { t } = useTranslation()
  const theme = useTheme()
  const [open, setOpen] = useState(null)
  const [openDelete, setOpenDelete] = useState(false)
  const [openChangeQuota, setOpenChangeQuota] = useState(false)
  const [statusSwitch, setStatusSwitch] = useState(item.status)
  const [quotaDelta, setQuotaDelta] = useState(0)
  const [remark, setRemark] = useState('')

  const handleDeleteOpen = () => {
    handleCloseMenu()
    setOpenDelete(true)
  }

  const handleDeleteClose = () => {
    setOpenDelete(false)
  }

  const handleOpenMenu = (event) => {
    setOpen(event.currentTarget)
  }

  const handleCloseMenu = () => {
    setOpen(null)
  }

  const closeAndResetChangeQuota = () => {
    setOpenChangeQuota(false)
    setQuotaDelta(0)
    setRemark('')
  }

  const handleChangeQuota = async() => {
    const quota = Number(quotaDelta)
    if (!quota) {
      showError(t('userPage.changeQuotaNotEmpty'))
      return
    }
    // 欠费用户（item.quota < 0）的可扣上限按 0 算，避免允许在负余额上继续扣（业务上"扣减不超过正余额"）
    if (quota < 0 && Math.abs(quota) > Math.max(0, item.quota)) {
      showError(t('userPage.changeQuotaNotEnough'))
      return
    }
    const ok = await manageUser(item.id, 'quota', { quota, remark })
    if (ok) closeAndResetChangeQuota()
  }

  const handleStatus = async() => {
    const switchVlue = statusSwitch === 1 ? 2 : 1
    const { success } = await manageUser(item.id, 'status', switchVlue)
    if (success) {
      setStatusSwitch(switchVlue)
    }
  }

  const handleDelete = async() => {
    handleCloseMenu()
    await manageUser(item.id, 'delete', '')
  }

  return (
    <>
      <TableRow tabIndex={item.id}>
        <TableCell>{item.id}</TableCell>

        <TableCell>{item.username}</TableCell>

        <TableCell>
          <Label>{item.group}</Label>
        </TableCell>

        <TableCell>
          <Stack direction="row" spacing={0.5} alignItems="center" justifyContent="center">
            <Tooltip title={t('token_index.remainingQuota')} placement="top">
              <Label color={'primary'} variant="outlined">
                {' '}
                {renderQuota(item.quota)}{' '}
              </Label>
            </Tooltip>
            <Tooltip title={t('token_index.usedQuota')} placement="top">
              <Label color={'primary'} variant="outlined">
                {' '}
                {renderQuota(item.used_quota)}{' '}
              </Label>
            </Tooltip>
            <Tooltip title={t('userPage.useQuota')} placement="top">
              <Label color={'primary'} variant="outlined">
                {' '}
                {renderNumber(item.request_count)}{' '}
              </Label>
            </Tooltip>
          </Stack>
        </TableCell>
        <TableCell>{renderRole(t, item.role)}</TableCell>
        <TableCell>
          <Stack direction="row" spacing={0.5} alignItems="center" justifyContent="center">
            <Tooltip title={item.wechat_id ? item.wechat_id : t('profilePage.notBound')} placement="top">
              <Icon icon="ri:wechat-fill"
                    color={item.wechat_id ? theme.palette.success.dark : theme.palette.grey[400]}/>
            </Tooltip>
            <Tooltip title={item.github_id ? item.github_id : t('profilePage.notBound')} placement="top">
              <Icon icon="ri:github-fill" color={item.github_id ? theme.palette.text.primary : theme.palette.grey[400]}/>
            </Tooltip>
            <Tooltip title={item.linuxdo_username ? item.linuxdo_username : '未绑定'} placement="top">
              <span style={{ display: 'inline-flex', alignItems: 'center' }}>
                <LinuxDoIcon
                  size={16}
                  color={item.linuxdo_username ? '#ffb003' : theme.palette.grey[400]}
                  style={{ opacity: item.linuxdo_username ? 1 : 0.6 }}
                />
              </span>
            </Tooltip>
            <Tooltip title={item.email ? item.email : t('profilePage.notBound')} placement="top">
              <Icon icon="ri:mail-fill" color={item.email ? theme.palette.text.primary : theme.palette.grey[400]}/>
            </Tooltip>
          </Stack>
        </TableCell>
        <TableCell sx={{ whiteSpace: 'nowrap' }}>{item.created_time === 0 ? t('common.unknown') : timestamp2string(item.created_time)}</TableCell>
        <TableCell sx={{ whiteSpace: 'nowrap' }}>{item.last_login_time === 0 ? t('common.unknown') : timestamp2string(item.last_login_time)}</TableCell>
        <TableCell sx={{ whiteSpace: 'nowrap' }}>{item.last_login_ip === '' || item.last_login_time === undefined ? t('common.unknown') : item.last_login_ip}</TableCell>
        <TableCell>
          {' '}
          <TableSwitch id={`switch-${item.id}`} checked={statusSwitch === 1} onChange={handleStatus}/>
        </TableCell>
        <TableCell
          sx={stickyCellSx}
        >
          <IconButton onClick={handleOpenMenu} sx={{ color: 'rgb(99, 115, 129)' }}>
            <Icon icon="solar:menu-dots-circle-bold-duotone"/>
          </IconButton>
        </TableCell>
      </TableRow>

      <Popover
        open={!!open}
        anchorEl={open}
        onClose={handleCloseMenu}
        anchorOrigin={{ vertical: 'top', horizontal: 'left' }}
        transformOrigin={{ vertical: 'top', horizontal: 'right' }}
        PaperProps={{
          sx: { minWidth: 140 }
        }}
      >
        {/* 设置为普通用户 - 只对非普通用户和非超级管理员显示 */}
        {item.role !== 100 && item.role !== 1 && (
          <MenuItem
            onClick={() => {
              handleCloseMenu()
              manageUser(item.id, 'set_role', 1)
            }}
          >
            <Icon icon="solar:user-bold-duotone" style={{ marginRight: '16px' }}/>
            {t('userPage.setCommonUser')}
          </MenuItem>
        )}

        {/* 设置为可信内部员工 - 只对非可信内部员工和非超级管理员显示 */}
        {item.role !== 100 && item.role !== 3 && (
          <MenuItem
            onClick={() => {
              handleCloseMenu()
              manageUser(item.id, 'set_role', 3)
            }}
          >
            <Icon icon="solar:verified-check-bold-duotone" style={{ marginRight: '16px' }}/>
            {t('userPage.setReliable')}
          </MenuItem>
        )}

        {/* 设置为管理员 - 只对非管理员和非超级管理员显示 */}
        {item.role !== 100 && item.role !== 10 && (
          <MenuItem
            onClick={() => {
              handleCloseMenu()
              manageUser(item.id, 'set_role', 10)
            }}
          >
            <Icon icon="solar:shield-user-bold-duotone" style={{ marginRight: '16px' }}/>
            {t('userPage.setAdmin')}
          </MenuItem>
        )}

        <MenuItem
          onClick={() => {
            handleCloseMenu()
            handleOpenModal()
            setModalUserId(item.id)
          }}
        >
          <Icon icon="solar:pen-bold-duotone" style={{ marginRight: '16px' }}/>
          {t('common.edit')}
        </MenuItem>
        <MenuItem
          onClick={() => {
            handleCloseMenu()
            setOpenChangeQuota(true)
          }}
        >
          <Icon icon="solar:wallet-money-bold-duotone" style={{ marginRight: '16px' }}/>
          {t('userPage.changeQuota')}
        </MenuItem>
        <MenuItem onClick={handleDeleteOpen} sx={{ color: 'error.main' }}>
          <Icon icon="solar:trash-bin-trash-bold-duotone" style={{ marginRight: '16px' }}/>
          {t('common.delete')}
        </MenuItem>
      </Popover>

      <ConfirmDialog
        open={openDelete}
        onClose={handleDeleteClose}
        title={t('common.delete')}
        content={t('common.deleteConfirm', { title: `用户 "${item.username}"` })}
        action={
          <Button
            variant="contained"
            color="error"
            onClick={handleDelete}
          >
            {t('common.delete')}
          </Button>
        }
      />

      <ConfirmDialog
        open={openChangeQuota}
        onClose={closeAndResetChangeQuota}
        title={t('userPage.changeQuota')}
        content={
          <Stack spacing={2} sx={{ mt: 2 }}>
            <QuotaInput
              id="quota-label"
              name="quotaDelta"
              label={t('userPage.changeQuota')}
              value={quotaDelta}
              onChange={(e) => setQuotaDelta(e.target.value)}
              defaultUnit={QUOTA_UNIT_CURRENCY}
              maxDeduct={Math.max(0, item.quota)}
              helperText={t('userPage.changeQuotaHelperText', {
                tokens: renderNumber(Math.max(0, item.quota)),
                money: '$' + calculateQuota(Math.max(0, item.quota), 6)
              })}
            />
            <TextField
              fullWidth
              id="quota-remark-label"
              label={t('userPage.quotaRemark')}
              type="text"
              value={remark}
              onChange={(e) => setRemark(e.target.value)}
            />
          </Stack>
        }
        action={
          <Button variant="contained" color="primary" onClick={handleChangeQuota}>
            {t('common.submit')}
          </Button>
        }
      />
    </>
  )
}

UsersTableRow.propTypes = {
  item: PropTypes.object,
  manageUser: PropTypes.func,
  handleOpenModal: PropTypes.func,
  setModalUserId: PropTypes.func
}
