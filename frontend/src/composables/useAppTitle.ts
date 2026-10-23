import { watch } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'

// VITE_APP_NAME, when set, overrides the product name. The default is copy, so
// it lives in the catalogue (common.appName): the Docker build does not
// pass VITE_APP_NAME, and a Latin placeholder used to end up in the browser tab.
const APP_NAME_OVERRIDE = import.meta.env.VITE_APP_NAME

// Keeps document.title in sync with route.meta.title (an i18n key). Installed
// once from App.vue.
export function useAppTitle(): void {
  const route = useRoute()
  const { t } = useI18n()

  watch(
    () => route.meta.title,
    (key) => {
      const appName = APP_NAME_OVERRIDE || t('common.appName')
      document.title = key ? `${t(key)} — ${appName}` : appName
    },
    { immediate: true },
  )
}
