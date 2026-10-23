import { useI18n } from 'vue-i18n'
import { useToast } from '@/composables/useToast'

// Copy with a toast either way. The Clipboard API needs a secure context
// (https or localhost); elsewhere it is missing and the user is told to copy
// by hand rather than getting silence.
export function useCopy() {
  const { t } = useI18n()
  const toast = useToast()
  return async function copy(text: string, doneKey = 'common.actions.copied'): Promise<void> {
    try {
      if (!navigator.clipboard) throw new Error('clipboard unavailable')
      await navigator.clipboard.writeText(text)
      toast.success(t(doneKey))
    } catch {
      toast.error(t('common.errors.copyFailed'))
    }
  }
}
