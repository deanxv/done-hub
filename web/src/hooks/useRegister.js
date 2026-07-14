import { API } from 'utils/api';
import { useNavigate } from 'react-router';
import { showSuccess } from 'utils/common';
import { useTranslation } from 'react-i18next';

const useRegister = () => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const register = async (input, turnstile) => {
    try {
      // agreement 是前端勾选字段，转换为后端校验用的 agreed 字段
      const payload = { ...input, agreed: input.agreement === true };
      delete payload.agreement;
      let affCode = localStorage.getItem('aff');
      if (affCode) {
        payload.aff_code = affCode;
      }

      const res = await API.post(`/api/user/register?turnstile=${turnstile}`, payload);
      const { success, message } = res.data;
      if (success) {
        showSuccess(t('common.registerOk'));
        navigate('/login');
      }
      return { success, message };
    } catch (err) {
      // 请求失败，设置错误信息
      return { success: false, message: '' };
    }
  };

  const sendVerificationCode = async (email, turnstile) => {
    try {
      const res = await API.get(`/api/verification?email=${email}&turnstile=${turnstile}`);
      const { success, message } = res.data;
      if (success) {
        showSuccess(t('common.registerTip'));
      }
      return { success, message };
    } catch (err) {
      // 请求失败，设置错误信息
      return { success: false, message: '' };
    }
  };

  return { register, sendVerificationCode };
};

export default useRegister;
