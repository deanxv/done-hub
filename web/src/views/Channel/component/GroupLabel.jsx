import PropTypes from 'prop-types';
import { Box } from '@mui/material';
import GroupRatioLabel from 'ui-component/GroupRatioLabel';

// 表格分组列：每个分组渲染为一枚「code + 倍率」双色胶囊，横向排列超出宽度自动换行。
const GroupLabel = ({ group, groupMap = {} }) => {
  let groups = [];
  if (group === '') {
    groups = ['default'];
  } else {
    groups = group.split(',');
    groups.sort();
  }
  return (
    <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 1 }}>
      {groups.map((item, index) => (
        <GroupRatioLabel key={index} label={item} ratio={groupMap[item]?.ratio} />
      ))}
    </Box>
  );
};

GroupLabel.propTypes = {
  group: PropTypes.string,
  groupMap: PropTypes.object
};

export default GroupLabel;
