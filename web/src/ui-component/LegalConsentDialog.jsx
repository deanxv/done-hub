import PropTypes from 'prop-types';
import { useState } from 'react';

import Button from '@mui/material/Button';
import Checkbox from '@mui/material/Checkbox';
import Dialog from '@mui/material/Dialog';
import DialogActions from '@mui/material/DialogActions';
import DialogContent from '@mui/material/DialogContent';
import DialogTitle from '@mui/material/DialogTitle';
import FormControlLabel from '@mui/material/FormControlLabel';
import Link from '@mui/material/Link';
import Typography from '@mui/material/Typography';

import { useTranslation } from 'react-i18next';

import useLogin from 'hooks/useLogin';
import { API } from 'utils/api';
import { showError } from 'utils/common';

// 版本化协议同意弹窗：管理员更新协议或存量老用户未同意时，进入后台强制补签。
// 需同意哪些协议由 AuthGuard 依据服务端下发的 need_agree_* 传入，弹窗只展示需要同意的那些。
export default function LegalConsentDialog({ needUserAgreement, needPrivacyPolicy }) {
  const { t } = useTranslation();
  const { loadUser } = useLogin();
  const [agreed, setAgreed] = useState(false);
  const [submitting, setSubmitting] = useState(false);

  const handleAgree = async () => {
    setSubmitting(true);
    try {
      const res = await API.post('/api/user/agree_terms');
      const { success, message } = res.data;
      if (success) {
        // 同意成功后刷新 Redux 用户数据，版本匹配后弹窗自动消失
        await loadUser();
      } else {
        showError(message);
        setSubmitting(false);
      }
    } catch (error) {
      showError(error.message);
      setSubmitting(false);
    }
  };

  return (
    <Dialog fullWidth maxWidth="xs" open disableEscapeKeyDown>
      <DialogTitle sx={{ pb: 2 }}>{t('legalConsentDialog.title')}</DialogTitle>
      <DialogContent sx={{ typography: 'body2' }}>
        <Typography variant="body2" sx={{ mb: 1 }}>
          {t('legalConsentDialog.description')}
        </Typography>
        <FormControlLabel
          control={<Checkbox checked={agreed} onChange={(e) => setAgreed(e.target.checked)} color="primary" />}
          label={
            <Typography variant="body2">
              {t('registerForm.agreementPrefix')}
              {needUserAgreement && (
                <Link href="/user-agreement" target="_blank" rel="noopener noreferrer" underline="hover">
                  {t('registerForm.userAgreement')}
                </Link>
              )}
              {needUserAgreement && needPrivacyPolicy && t('registerForm.agreementAnd')}
              {needPrivacyPolicy && (
                <Link href="/privacy-policy" target="_blank" rel="noopener noreferrer" underline="hover">
                  {t('registerForm.privacyPolicy')}
                </Link>
              )}
            </Typography>
          }
        />
      </DialogContent>
      <DialogActions>
        <Button variant="contained" color="primary" disabled={!agreed || submitting} onClick={handleAgree}>
          {t('legalConsentDialog.button')}
        </Button>
      </DialogActions>
    </Dialog>
  );
}

LegalConsentDialog.propTypes = {
  needUserAgreement: PropTypes.bool,
  needPrivacyPolicy: PropTypes.bool
};
