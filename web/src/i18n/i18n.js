import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';
import { resources } from './resources';
import LanguageDetector from 'i18next-browser-languagedetector';

i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    resources,
    // 首屏语言：用户手动选过的优先，其次上次解析出的显示语言（缓存于 default_language，
    // 由 StatusContext 在 /api/status 后写入），最后回退 zh_CN（与后端 config.go 的 viper 默认一致）。
    // 直接用缓存值可避免默认部署（zh_CN）刷新时先闪一下英文再切回中文。
    fallbackLng: 'zh_CN',
    debug: false,
    lng: localStorage.getItem('appLanguage') || localStorage.getItem('default_language') || 'zh_CN',
    interpolation: {
      escapeValue: false
    }
  });

export default i18n;
