import i18n from 'i18next';
import Backend from 'i18next-http-backend';
import LanguageDetector from 'i18next-browser-languagedetector';
import { initReactI18next } from 'react-i18next';

async function initI18next() {
    await i18n
        .use(Backend)
        .use(LanguageDetector)
        .use(initReactI18next)
        .init({
            backend: {
                loadPath: import.meta.env.DEV ? "/public/locales/{{lng}}.json" : "/static/locales/{{lng}}.json"
            },
            debug: import.meta.env.DEV,
            fallbackLng: 'en-US',
            interpolation: {
                escapeValue: false,
            },
            react: {
                useSuspense: false
            }
        });
    return i18n;
}

export default initI18next();