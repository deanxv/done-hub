import React, { useEffect, useState, useCallback } from 'react';
import { API } from 'utils/api';
import { showError } from 'utils/common';
import { Box, Container, Typography } from '@mui/material';
import MainCard from 'ui-component/cards/MainCard';
import { useTranslation } from 'react-i18next';
import ContentViewer from 'ui-component/ContentViewer';

const PrivacyPolicy = () => {
  const { t } = useTranslation();
  const [content, setContent] = useState('');
  const [contentLoaded, setContentLoaded] = useState(false);

  const displayContent = useCallback(async () => {
    setContent(localStorage.getItem('privacy_policy') || '');
    try {
      const res = await API.get('/api/privacy_policy');
      const { success, message, data } = res.data;
      if (success) {
        setContent(data);
        localStorage.setItem('privacy_policy', data);
      } else {
        showError(message);
        setContent(t('privacyPolicy.loadingError'));
      }
    } catch (error) {
      setContent(t('privacyPolicy.loadingError'));
    }

    setContentLoaded(true);
  }, [t]);

  useEffect(() => {
    displayContent();
  }, [displayContent]);

  return (
    <>
      {contentLoaded && content === '' ? (
        <Box>
          <Container sx={{ paddingTop: '40px' }}>
            <MainCard title={t('privacyPolicy.title')}>
              <Typography variant="body2">{t('privacyPolicy.emptyDescription')}</Typography>
            </MainCard>
          </Container>
        </Box>
      ) : (
        <Box>
          <ContentViewer
            content={content}
            loading={!contentLoaded}
            errorMessage={content === t('privacyPolicy.loadingError') ? t('privacyPolicy.loadingError') : ''}
            containerStyle={{ minHeight: 'calc(100vh - 136px)' }}
            contentStyle={{ fontSize: 'larger' }}
          />
        </Box>
      )}
    </>
  );
};

export default PrivacyPolicy;
