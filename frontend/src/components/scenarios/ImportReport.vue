<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { AlertTriangle, ChevronDown, ChevronRight, FileSpreadsheet, X } from 'lucide-vue-next'
import type { ScenarioStep } from '@/types/api'
import type { ImportReport, ImportWarning } from './importReport'

const props = defineProps<{ report: ImportReport; steps: ScenarioStep[] }>()
const emit = defineEmits<{ open: [stepId: string]; close: [] }>()
const { t, te } = useI18n()

const open = ref(true)
const code = ref<string | null>(null)

const stats = computed(() => [
  { key: 'blocks', n: props.report.blocks },
  { key: 'messages', n: props.report.messages },
  { key: 'questions', n: props.report.questions },
  { key: 'branches', n: props.report.branches },
  { key: 'rules', n: props.report.rules },
  { key: 'media', n: props.report.media_names },
])

// Codes in order of first appearance, with counts: the filter chips.
const codes = computed(() => {
  const m = new Map<string, number>()
  for (const w of props.report.warnings) m.set(w.code, (m.get(w.code) ?? 0) + 1)
  return [...m.entries()].map(([c, n]) => ({ code: c, n }))
})

const shown = computed(() =>
  [...props.report.warnings]
    .filter((w) => !code.value || w.code === code.value)
    .sort((a, b) => a.row - b.row),
)

function codeLabel(c: string): string {
  const k = `scenarios.importReport.codes.${c}`
  return te(k) ? t(k) : c
}

// A warning points at a step by id (this import) or by stable key (a later
// re-import or a reload keeps keys, not ids).
function stepOf(w: ImportWarning): ScenarioStep | null {
  return (
    props.steps.find((s) => !!w.step_id && s.id === w.step_id) ??
    props.steps.find((s) => !!w.step_key && s.key === w.step_key) ??
    null
  )
}

function onPick(w: ImportWarning): void {
  const s = stepOf(w)
  if (s) emit('open', s.id)
}
</script>

<template>
  <section class="report" :aria-label="t('scenarios.importReport.title')">
    <header class="report__head">
      <button type="button" class="report__toggle" :aria-expanded="open" @click="open = !open">
        <component :is="open ? ChevronDown : ChevronRight" :size="16" />
        <FileSpreadsheet :size="16" />
        <span class="report__title">{{ t('scenarios.importReport.title') }}</span>
      </button>
      <span class="report__summary">
        <span v-for="s in stats" :key="s.key" class="report__stat">
          {{ t(`scenarios.importReport.stats.${s.key}`) }}: <b>{{ s.n }}</b>
        </span>
      </span>
      <button
        type="button"
        class="report__close"
        :title="t('scenarios.importReport.close')"
        :aria-label="t('scenarios.importReport.close')"
        @click="emit('close')"
      >
        <X :size="16" />
      </button>
    </header>

    <div v-if="open" class="report__body">
      <p v-if="report.warnings.length === 0" class="report__ok">
        {{ t('scenarios.importReport.noWarnings') }}
      </p>
      <template v-else>
        <p class="report__lead">
          <AlertTriangle :size="14" />
          {{ t('scenarios.importReport.warningsLead', { n: report.warnings.length }) }}
        </p>
        <div class="report__chips" role="group" :aria-label="t('scenarios.importReport.filter')">
          <button
            type="button"
            :class="['chip', { 'chip--on': code === null }]"
            @click="code = null"
          >
            {{ t('scenarios.importReport.all') }} · {{ report.warnings.length }}
          </button>
          <button
            v-for="c in codes"
            :key="c.code"
            type="button"
            :class="['chip', { 'chip--on': code === c.code }]"
            @click="code = code === c.code ? null : c.code"
          >
            {{ codeLabel(c.code) }} · {{ c.n }}
          </button>
        </div>
        <ul class="report__list">
          <li v-for="(w, i) in shown" :key="`${w.row}-${w.code}-${i}`">
            <button
              type="button"
              class="warn"
              :disabled="!stepOf(w)"
              :title="stepOf(w) ? t('scenarios.importReport.openStep') : undefined"
              @click="onPick(w)"
            >
              <span class="warn__row">{{ t('scenarios.importReport.row', { n: w.row }) }}</span>
              <span class="warn__code">{{ codeLabel(w.code) }}</span>
              <span class="warn__msg">{{ w.message }}</span>
            </button>
          </li>
        </ul>
      </template>
    </div>
  </section>
</template>

<style scoped>
.report {
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--surface);
  margin-bottom: var(--space-3);
}
.report__head {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: 8px 12px;
  flex-wrap: wrap;
}
.report__toggle,
.report__close {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  background: none;
  border: 0;
  color: var(--fg);
  cursor: pointer;
  padding: 2px;
  border-radius: var(--radius-sm);
}
.report__close {
  margin-left: auto;
  color: var(--fg-muted);
}
.report__toggle:focus-visible,
.report__close:focus-visible,
.chip:focus-visible,
.warn:focus-visible {
  outline: 2px solid var(--ring);
  outline-offset: 1px;
}
.report__title {
  font-weight: 600;
  font-size: var(--text-sm);
}
.report__summary {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 12px;
  font-size: var(--text-sm);
  color: var(--fg-muted);
}
.report__stat b {
  color: var(--fg);
  font-weight: 600;
}
.report__body {
  border-top: 1px solid var(--border-subtle);
  padding: 8px 12px 12px;
}
.report__ok,
.report__lead {
  margin: 0 0 8px;
  font-size: var(--text-sm);
  color: var(--fg-muted);
  display: flex;
  align-items: center;
  gap: 6px;
}
.report__lead :deep(svg) {
  color: var(--warning);
}
.report__chips {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-bottom: 8px;
}
.chip {
  border: 1px solid var(--border);
  background: var(--bg-subtle);
  color: var(--fg-muted);
  border-radius: var(--radius-full);
  padding: 2px 10px;
  font-size: var(--text-xs);
  cursor: pointer;
}
.chip--on {
  border-color: var(--accent);
  color: var(--fg);
  background: var(--surface-hover);
}
.report__list {
  list-style: none;
  margin: 0;
  padding: 0;
  max-height: 280px;
  overflow-y: auto;
}
.warn {
  display: grid;
  grid-template-columns: 88px 180px 1fr;
  gap: 8px;
  width: 100%;
  text-align: left;
  background: none;
  border: 0;
  border-bottom: 1px solid var(--border-subtle);
  padding: 6px 4px;
  font-size: var(--text-sm);
  color: var(--fg);
  cursor: pointer;
}
.warn:hover:not(:disabled) {
  background: var(--surface-hover);
}
.warn:disabled {
  cursor: default;
}
.warn__row {
  color: var(--fg-subtle);
  font-variant-numeric: tabular-nums;
}
.warn__code {
  color: var(--fg-muted);
}
@media (max-width: 720px) {
  .warn {
    grid-template-columns: 72px 1fr;
  }
  .warn__msg {
    grid-column: 1 / -1;
  }
}
</style>
