import { useI18n } from 'vue-i18n'
import { apiRemind } from '@/api/team'
import { useToast } from '@/composables/useToast'
import { errorMessage, isApiError } from '@/lib/apiError'
import { remindConflict, remindOutcome, type RemindOutcome } from '@/lib/nudges'

// "Remind" of the card and of "Today": one place that reads the answer.
// 200 → "Sent"; 202 → the server's "will go at 08:00 newcomer time";
// 200 failed → error; 409 → a soft "nothing to remind about".
// Returns true when something went or was planned (the caller reloads).
export function useRemind() {
  const { t } = useI18n()
  const toast = useToast()

  function show(o: RemindOutcome): void {
    const text = o.text ?? t(`dashboard.remind.${o.key}`)
    if (o.tone === 'success') toast.success(text)
    else if (o.tone === 'info') toast.info(text)
    else toast.error(text)
  }

  async function remind(newcomerId: string): Promise<boolean> {
    try {
      const { httpStatus, result } = await apiRemind(newcomerId)
      const o = remindOutcome(result, httpStatus)
      show(o)
      return o.tone !== 'error'
    } catch (e) {
      const soft = isApiError(e) ? remindConflict(e.status, e.serverMessage) : null
      if (soft) show(soft)
      else toast.error(errorMessage(e, 'dashboard.remind.failed'))
      return false
    }
  }

  return { remind }
}
