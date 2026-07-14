import React, { useEffect, useState, useCallback } from 'react';
import { API } from 'utils/api';
import { showError } from 'utils/common';
import { Box, Container, Typography } from '@mui/material';
import MainCard from 'ui-component/cards/MainCard';
import { useTranslation } from 'react-i18next';
import ContentViewer from 'ui-component/ContentViewer';

const UserAgreement = () => {
  const { t } = useTranslation();
  const [content, setContent] = useState('');
  const [contentLoaded, setContentLoaded] = useState(false);

  const displayContent = useCallback(async () => {
    setContent(localStorage.getItem('user_agreement') || '');
    try {
      const res = await API.get('/api/user_agreement');
      const { success, message, data } = res.data;
      if (success) {
        setContent(data);
        localStorage.setItem('user_agreement', data);
      } else {
        showError(message);
        setContent(t('userAgreement.loadingError'));
      }
    } catch (error) {
      setContent(t('userAgreement.loadingError'));
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
            <MainCard title={t('userAgreement.title')}>
              <Typography variant="body2">{t('userAgreement.emptyDescription')}</Typography>
            </MainCard>
          </Container>
        </Box>
      ) : (
        <Box>
          <ContentViewer
            content={content}
            loading={!contentLoaded}
            errorMessage={content === t('userAgreement.loadingError') ? t('userAgreement.loadingError') : ''}
            containerStyle={{ minHeight: 'calc(100vh - 136px)' }}
            contentStyle={{ fontSize: 'larger' }}
          />
        </Box>
      )}
    </>
  );
};

export default UserAgreement;
