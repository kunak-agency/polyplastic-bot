<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Sparkles } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { apiRewriteVy } from '@/api/scenarios'
import { useToast } from '@/composables/useToast'
import { errorMessage } from '@/lib/apiError'
import type { RewriteVyResponse, ScenarioDetail, ScenarioStep } from '@/types/api'
import { dropImportWarnings } from './importReport'

// "Make the 'you' variant (AI)" in the import report. The endpoint is called
// one step at a time (the model needs seconds per text, the server answers
// within 30 s), two calls in flight; commands and rules go last, alone. The
// draft that comes back replaces the editor's copy, so the button waits for
// unsaved edits to be saved first.
const props = defineProps<{ scenarioId: string; steps: ScenarioStep[]; dirty?: boolean }>()
const emit = defineEmits<{ done: [scenario: ScenarioDetail] }>()
const { t } = useI18n()
const toast = useToast()

const PARALLEL = 2
const running = ref(false)
const done = ref(0)
const total = ref(0)
const scenarioPhase = ref(false)

const label = computed(() => {
  if (!running.value) return t('rewrite.button')
  if (scenarioPhase.value) return t('rewrite.scenarioTexts')
  return t('rewrite.progress', { done: done.value, total: total.value })
})

async function run(): Promise<void> {
  if (running.value || props.dirty) return
  const ids = props.steps.map((s) => s.id)
  running.value = true
  scenarioPhase.value = false
  done.value = 0
  total.value = ids.length
  let updated = 0
  let failed = 0
  let failedVy = 0
  const acc: { last: ScenarioDetail | null } = { last: null }
  const cleared: RewriteVyResponse['cleared_warnings'] = []
  const take = (r: RewriteVyResponse): void => {
    updated += r.updated
    failed += r.failed
    failedVy += r.failed_vy ?? r.failed
    cleared.push(...r.cleared_warnings)
    acc.last = r.scenario
  }
  try {
    let next = 0
    const worker = async (): Promise<void> => {
      while (next < ids.length) {
        const id = ids[next++]
        take(await apiRewriteVy(props.scenarioId, { step_ids: [id], neutralize: true }))
        done.value++
      }
    }
    await Promise.all(Array.from({ length: Math.min(PARALLEL, ids.length) }, worker))
    scenarioPhase.value = true
    take(
      await apiRewriteVy(props.scenarioId, {
        step_ids: [],
        scenario_texts: true,
        neutralize: true,
      }),
    )
    dropImportWarnings(props.scenarioId, cleared, failedVy === 0)
    if (updated === 0 && failed === 0) toast.info(t('rewrite.nothing'))
    else if (failed === 0) toast.success(t('rewrite.done', { n: updated }))
    else toast.warning(t('rewrite.doneFailed', { n: updated, failed }))
  } catch (e) {
    toast.error(errorMessage(e, 'rewrite.failed'), {
      description: t('rewrite.stopped', { done: done.value, total: total.value }),
    })
    dropImportWarnings(props.scenarioId, cleared, false)
  } finally {
    running.value = false
    if (acc.last) emit('done', acc.last)
  }
}
</script>

<template>
  <Button
    size="sm"
    variant="outline"
    :disabled="running || dirty || steps.length === 0"
    :title="dirty ? t('rewrite.dirty') : t('rewrite.hint')"
    :aria-busy="running"
    @click="run"
  >
    <Sparkles :size="14" :class="{ spin: running }" />
    <span aria-live="polite">{{ label }}</span>
  </Button>
</template>

<style scoped>
.spin {
  animation: pulse 1.2s ease-in-out infinite;
}
@keyframes pulse {
  50% {
    opacity: 0.35;
  }
}
@media (prefers-reduced-motion: reduce) {
  .spin {
    animation: none;
  }
}
</style>
