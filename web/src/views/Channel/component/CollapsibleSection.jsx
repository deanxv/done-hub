import PropTypes from 'prop-types'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Box, Button, Collapse, Typography } from '@mui/material'
import { Icon } from '@iconify/react'

const CollapsibleSection = ({ title, description, defaultExpanded = false, children }) => {
  const { t } = useTranslation()
  const [expanded, setExpanded] = useState(defaultExpanded)

  return (
    <Box
      sx={{
        border: '1px solid',
        borderColor: 'divider',
        borderRadius: 2,
        marginTop: 2,
        marginBottom: 2,
        overflow: 'hidden'
      }}
    >
      <Box
        sx={{
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
          padding: 2
        }}
      >
        <Box sx={{ flex: 1 }}>
          <Typography variant="h3">{title}</Typography>
          {description && <Typography variant="caption">{description}</Typography>}
        </Box>
        <Button
          onClick={() => setExpanded(!expanded)}
          endIcon={
            expanded ? (
              <Icon icon="solar:alt-arrow-up-line-duotone"/>
            ) : (
              <Icon icon="solar:alt-arrow-down-line-duotone"/>
            )
          }
          sx={{ textTransform: 'none', marginLeft: 2 }}
        >
          {expanded ? t('channel_edit.collapse') : t('channel_edit.expand')}
        </Button>
      </Box>

      <Collapse in={expanded}>
        <Box sx={{ padding: 2, paddingTop: 0 }}>{children}</Box>
      </Collapse>
    </Box>
  )
}

export default CollapsibleSection

CollapsibleSection.propTypes = {
  title: PropTypes.node,
  description: PropTypes.node,
  defaultExpanded: PropTypes.bool,
  children: PropTypes.node
}
