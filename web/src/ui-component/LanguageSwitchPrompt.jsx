import { useEffect } from 'react';
import { useSelector } from 'react-redux';
import { useTranslation } from 'react-i18next';
import { Button, Stack } from '@mui/material';
import { enqueueSnackbar, closeSnackbar } from 'notistack';
import i18nList from 'i18n/i18nList';
import { detectBrowserLanguage } from 'utils/languageDetect';

// 记录“上一次已就该浏览器语言提示过”的标记，避免用户忽略后每次访问都被打扰。
// 仅当浏览器语言再次变化（例如从中文切到英文）时才会重新提示。
const LAST_PROMPTED_KEY = 'lastPromptedLanguage';

// LanguageSwitchPrompt 在检测到浏览器语言与当前显示语言不一致时，提示用户切换。
// 规则：
//   - 浏览器语言 == 当前显示语言 → 不提示；
//   - 已就该浏览器语言提示过（无论接受还是忽略）→ 不再提示，直到浏览器语言再次变化；
//   - 后台开关关闭 → 完全不提示。
const LanguageSwitchPrompt = () => {
  const { t, i18n } = useTranslation();
  const siteInfo = useSelector((state) => state.siteInfo);

  useEffect(() => {
    // siteInfo 尚未加载完成时不处理（isLoading 初始为 true）
    if (siteInfo?.isLoading) {
      return;
    }
    // 后台关闭了语言切换提示
    if (siteInfo?.language_switch_prompt_enabled === false) {
      return;
    }

    const detected = detectBrowserLanguage();
    if (!detected) {
      return;
    }
    // 浏览器语言与当前显示一致：无需提示
    if (detected === i18n.language) {
      return;
    }
    // 已就该浏览器语言提示过，且它此后未再变化：不打扰
    if (localStorage.getItem(LAST_PROMPTED_KEY) === detected) {
      return;
    }

    // 无条件记录本次检测到的浏览器语言，之后仅当它再次变化时才重新提示。
    // 这样用户随后手动切换显示语言不会被立刻反向追问。
    localStorage.setItem(LAST_PROMPTED_KEY, detected);

    const targetName = i18nList.find((item) => item.lng === detected)?.name || detected;
    enqueueSnackbar(t('common.languageSwitchPrompt', { language: targetName }), {
      variant: 'info',
      persist: true,
      preventDuplicate: true,
      action: (snackbarId) => (
        <Stack direction="row" spacing={1}>
          <Button
            color="inherit"
            size="small"
            variant="outlined"
            onClick={() => {
              i18n.changeLanguage(detected);
              // 显式持久化,不依赖 useI18n 的全局 languageChanged 监听(该监听仅随 Header 挂载)
              localStorage.setItem('appLanguage', detected);
              closeSnackbar(snackbarId);
            }}
          >
            {t('common.languageSwitchConfirm')}
          </Button>
          <Button color="inherit" size="small" onClick={() => closeSnackbar(snackbarId)}>
            {t('common.languageSwitchDismiss')}
          </Button>
        </Stack>
      )
    });
    // 只依赖 siteInfo 就绪状态：i18n.language 变化（含用户手动切换）不应触发重新判定
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [siteInfo?.isLoading, siteInfo?.language_switch_prompt_enabled]);

  return null;
};

export default LanguageSwitchPrompt;
