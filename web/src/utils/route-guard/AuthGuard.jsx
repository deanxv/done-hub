import { useSelector } from 'react-redux';
import { useEffect, useContext } from 'react';
import { UserContext } from 'contexts/UserContext';
import { useNavigate } from 'react-router-dom';
import LegalConsentDialog from 'ui-component/LegalConsentDialog';

const AuthGuard = ({ children }) => {
  const account = useSelector((state) => state.account);
  const { isUserLoaded } = useContext(UserContext);
  const navigate = useNavigate();

  useEffect(() => {
    if (isUserLoaded && !account.user) {
      navigate('/login');
      return;
    }
  }, [account, navigate, isUserLoaded]);

  // 在用户信息加载完成前不渲染子组件
  if (!isUserLoaded) {
    return null;
  }

  // 协议同意前置闸门：由服务端实时下发 need_agree_*，未同意则只渲染同意弹窗，不放行进后台
  const user = account.user;
  if (user && (user.need_agree_user_agreement || user.need_agree_privacy_policy)) {
    return (
      <LegalConsentDialog
        needUserAgreement={Boolean(user.need_agree_user_agreement)}
        needPrivacyPolicy={Boolean(user.need_agree_privacy_policy)}
      />
    );
  }

  return children;
};

export default AuthGuard;
