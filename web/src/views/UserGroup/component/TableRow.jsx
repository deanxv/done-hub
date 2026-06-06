import PropTypes from 'prop-types'
import { useState } from 'react'

import { Button, IconButton, MenuItem, Popover, TableCell, TableRow, Tooltip } from '@mui/material'

import Label from 'ui-component/Label'
import TableSwitch from 'ui-component/Switch'
import ConfirmDialog from 'ui-component/confirm-dialog'
import { useTranslation } from 'react-i18next'
import { Icon } from '@iconify/react'

export default function UserGroupTableRow({ item, manageUserGroup, handleOpenModal, setModalUserGroupId }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(null)
  const [openDelete, setOpenDelete] = useState(false)
  const [statusSwitch, setStatusSwitch] = useState(item.enable)

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

  const handleStatus = async() => {
    const switchVlue = !statusSwitch
    const { success } = await manageUserGroup(item.id, 'status')
    if (success) {
      setStatusSwitch(switchVlue)
    }
  }

  const handleDelete = async() => {
    handleCloseMenu()
    await manageUserGroup(item.id, 'delete')
  }

  return (
    <>
      <TableRow tabIndex={item.id}>
        <TableCell>{item.id}</TableCell>

        <TableCell>{item.symbol}</TableCell>
        <TableCell>{item.name}</TableCell>
        <Tooltip title={item.description || ''} placement="top" disableHoverListener={!item.description}>
          <TableCell sx={{ maxWidth: 220, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
            {item.description || '-'}
          </TableCell>
        </Tooltip>
        <TableCell>{item.ratio}</TableCell>
        <TableCell>{item.api_rate}</TableCell>
        <TableCell>
          <Label variant="outlined" color={item.public ? 'primary' : 'error'}>
            {item.public ? '是' : '否'}
          </Label>
        </TableCell>
        <TableCell>
          <Label variant="outlined" color={item.promotion ? 'primary' : 'error'}>
            {item.promotion ? '是' : '否'}
          </Label>
        </TableCell>
        <TableCell>{item.min}</TableCell>
        <TableCell>{item.max}</TableCell>
        <TableCell>
          {' '}
          <TableSwitch id={`switch-${item.id}`} checked={statusSwitch} onChange={handleStatus}/>
        </TableCell>
        <TableCell>
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
        <MenuItem
          onClick={() => {
            handleCloseMenu()
            handleOpenModal()
            setModalUserGroupId(item.id)
          }}
        >
          <Icon icon="solar:pen-bold-duotone" style={{ marginRight: '16px' }}/>
          {t('common.edit')}
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
        content={t('common.deleteConfirm', { title: item.name })}
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
    </>
  )
}

UserGroupTableRow.propTypes = {
  item: PropTypes.object,
  manageUserGroup: PropTypes.func,
  handleOpenModal: PropTypes.func,
  setModalUserGroupId: PropTypes.func
}
