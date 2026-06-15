import PropTypes from 'prop-types';
import { useEffect, useState } from 'react';
import { Box, Button, Dialog, DialogActions, DialogContent, DialogTitle, Divider, Tab, Tabs } from '@mui/material';
import { useTranslation } from 'react-i18next';
import BatchAzureAPI from './BatchAzureAPI';
import BatchDelModel from './BatchDelModel';
import BatchAddUserGroup from './BatchAddUserGroup';
import BatchAddModel from './BatchAddModel';

const CustomTabPanel = ({ children, value, index }) => (
  <Box role="tabpanel" hidden={value !== index} id={`channel-tabpanel-${index}`} aria-labelledby={`channel-tab-${index}`} sx={{ p: 3 }}>
    {children}
  </Box>
);

CustomTabPanel.propTypes = {
  children: PropTypes.node,
  index: PropTypes.number.isRequired,
  value: PropTypes.number.isRequired
};

const a11yProps = (index) => ({
  id: `channel-tab-${index}`,
  'aria-controls': `channel-tabpanel-${index}`
});

const BatchModal = ({ open, setOpen, groupOptions, modelOptions }) => {
  const { t } = useTranslation();
  const [value, setValue] = useState(0);
  const [mountedTabs, setMountedTabs] = useState(() => new Set([0]));

  // 父组件常驻渲染 BatchModal，自身 useState 不随 Dialog 关闭而清。
  // 重开时显式回到默认 tab 并清空 lazy mount 集合，让用户每次重开看到的都是
  // 干净的初始状态，而不是上次留下的 tab 和已挂载集合。
  useEffect(() => {
    if (open) {
      setValue(0);
      setMountedTabs(new Set([0]));
    }
  }, [open]);

  const handleChange = (event, newValue) => {
    setValue(newValue);
    setMountedTabs((prev) => {
      if (prev.has(newValue)) return prev;
      const next = new Set(prev);
      next.add(newValue);
      return next;
    });
  };

  return (
    <Dialog open={open} onClose={() => setOpen(!open)} fullWidth maxWidth={'md'}>
      <DialogTitle>
        <Box>
          <Tabs
            value={value}
            onChange={handleChange}
            aria-label="batch channel tabs"
            variant="scrollable"
            scrollButtons="auto"
            allowScrollButtonsMobile
          >
            <Tab label={t('channel_index.batchAddUserGroup')} {...a11yProps(0)} />
            <Tab label={t('channel_index.batchAddModel')} {...a11yProps(1)} />
            <Tab label={t('channel_index.batchDelete')} {...a11yProps(2)} />
            <Tab label={t('channel_index.AzureApiVersion')} {...a11yProps(3)} />
          </Tabs>
        </Box>
      </DialogTitle>
      <Divider />
      <DialogContent>
        <CustomTabPanel value={value} index={0}>
          {mountedTabs.has(0) && <BatchAddUserGroup groupOptions={groupOptions} />}
        </CustomTabPanel>
        <CustomTabPanel value={value} index={1}>
          {mountedTabs.has(1) && <BatchAddModel modelOptions={modelOptions} />}
        </CustomTabPanel>
        <CustomTabPanel value={value} index={2}>
          {mountedTabs.has(2) && <BatchDelModel />}
        </CustomTabPanel>
        <CustomTabPanel value={value} index={3}>
          {mountedTabs.has(3) && <BatchAzureAPI />}
        </CustomTabPanel>
        <DialogActions>
          <Button onClick={() => setOpen(!open)}>{t('common.cancel')}</Button>
        </DialogActions>
      </DialogContent>
    </Dialog>
  );
};

BatchModal.propTypes = {
  open: PropTypes.bool,
  setOpen: PropTypes.func,
  groupOptions: PropTypes.array,
  modelOptions: PropTypes.array
};

export default BatchModal;
