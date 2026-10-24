import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { apiExportScenario } from '@/api/scenarios'
import { useToast } from '@/composables/useToast'
import { errorMessage } from '@/lib/apiError'
import { safeFilename, saveBlob } from '@/lib/download'

// "Download Excel" (api_contract.md v4, export): the draft or a published
// version as .xlsx in the customer's TZ format. The server exports what is
// saved, so unsaved editor changes are flagged in the toast.
export function useScenarioExport() {
  const { t } = useI18n()
  const toast = useToast()
  const exporting = ref(false)

  async function download(
    scenario: { id: string; name: string },
    opts: { version?: number; dirty?: boolean } = {},
  ): Promise<void> {
    if (exporting.value) return
    exporting.value = true
    try {
      const { blob, filename } = await apiExportScenario(scenario.id, opts.version)
      const fallback = opts.version
        ? t('scenarios.export.fileVersion', { name: scenario.name, version: opts.version })
        : t('scenarios.export.fileDraft', { name: scenario.name })
      saveBlob(blob, safeFilename(filename ?? fallback, 'scenario.xlsx'))
      if (opts.dirty) toast.warning(t('scenarios.export.doneUnsaved'))
      else toast.success(t('scenarios.export.done'))
    } catch (e) {
      toast.error(errorMessage(e, 'scenarios.export.failed'))
    } finally {
      exporting.value = false
    }
  }

  return { exporting, download }
}
