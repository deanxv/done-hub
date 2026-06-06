import PropTypes from 'prop-types'
import {
  Box,
  Checkbox,
  IconButton,
  TableCell,
  TableHead,
  TableRow,
  TableSortLabel,
  Tooltip,
  Typography
} from '@mui/material'
import HelpOutlineIcon from '@mui/icons-material/HelpOutline'
import { stickyHeadCellSx } from './stickyCellSx'

const KeywordTableHead = ({ order, orderBy, headLabel, onRequestSort, numSelected, rowCount, onSelectAllClick }) => {
  const onSort = (property) => (event) => {
    onRequestSort(event, property)
  }

  const label = (cell) => {
    if (cell.tooltip) {
      return (
        <Box display="flex" alignItems="center" justifyContent="center">
          <Typography variant="body1" sx={{ fontWeight: 'inherit', color: 'inherit' }}>{cell.label}</Typography>
          <Tooltip title={cell.tooltip} placement="bottom-start" enterDelay={300}>
            <IconButton size="small">
              <HelpOutlineIcon fontSize="inherit"/>
            </IconButton>
          </Tooltip>
        </Box>
      )
    } else {
      return cell.label
    }
  }

  return (
    <TableHead>
      <TableRow>
        {headLabel.map((headCell) =>
          headCell.hide && headCell.hide === true ? null : (
            <TableCell
              key={headCell.id}
              align={headCell.align || 'right'}
              // sortDirection={orderBy === headCell.id ? order : false}
              sx={{
                width: headCell.width,
                minWidth: headCell.minWidth,
                p: '10px 8px',
                ...(headCell.sticky && stickyHeadCellSx)
              }}
            >
              {headCell.id === 'select' && onSelectAllClick ? (
                <Checkbox
                  indeterminate={numSelected > 0 && numSelected < rowCount}
                  checked={rowCount > 0 && numSelected === rowCount}
                  onChange={onSelectAllClick}
                  inputProps={{
                    'aria-label': 'select all channels'
                  }}
                />
              ) : headCell.disableSort ? (
                label(headCell)
              ) : (
                <TableSortLabel
                  hideSortIcon
                  active={orderBy === headCell.id}
                  direction={orderBy === headCell.id ? order : 'asc'}
                  onClick={onSort(headCell.id)}
                >
                  {label(headCell)}
                </TableSortLabel>
              )}
            </TableCell>
          )
        )}
      </TableRow>
    </TableHead>
  )
}

export default KeywordTableHead

KeywordTableHead.propTypes = {
  order: PropTypes.oneOf(['asc', 'desc']),
  orderBy: PropTypes.string,
  onRequestSort: PropTypes.func,
  headLabel: PropTypes.array,
  numSelected: PropTypes.number,
  rowCount: PropTypes.number,
  onSelectAllClick: PropTypes.func
}
