<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { ArrowRight, Flag, GitBranch, Mail, Zap } from 'lucide-vue-next'
import { formatDayOffset } from '@/lib/calendar'
import {
  branchCount,
  branchFor,
  effectiveAdvance,
  groupBlocks,
  statsBadge,
  type Issue,
} from '@/lib/scenarioBranches'
import type { ScenarioStep, StepStats } from '@/types/api'

// Overview table of the scenario (verdict §Q3): day, time, sites, kind, text,
// buttons, branches "2/3", status. Steps of one block (same day and time) are
// grouped; branches are edited in the panel, never here.
const props = defineProps<{
  steps: ScenarioStep[]
  selectedId: string | null
  issues: Map<string, Issue[]>
  // Legacy step checks (scenarioSteps.ts): warnings.
  legacy: Map<string, string[]>
  stats: Map<string, StepStats>
  compact: boolean
}>()
const emit = defineEmits<{ select: [id: string] }>()
const { t } = useI18n()

const blocks = computed(() => groupBlocks(props.steps))

function sitesLabel(s: ScenarioStep): string {
  return s.sites.length === 0
    ? t('scenarios.allSites')
    : s.sites.map((c) => t(`common.site.${c}`)).join(', ')
}
function errorsOf(id: string): number {
  return (props.issues.get(id) ?? []).filter((i) => i.level === 'error').length
}
function warningsOf(id: string): number {
  return (
    (props.issues.get(id) ?? []).filter((i) => i.level === 'warning').length +
    (props.legacy.get(id) ?? []).length
  )
}
function statusTitle(s: ScenarioStep): string {
  const lines = (props.issues.get(s.id) ?? []).map((i) =>
    t(`scenarios.issue.${i.code}`, { detail: i.detail ?? '' }),
  )
  for (const l of props.legacy.get(s.id) ?? []) lines.push(t(`scenarios.issues.${l}`))
  return lines.join('\n')
}
function badge(s: ScenarioStep, label: string) {
  return statsBadge(props.stats.get(s.id)?.buttons.find((b) => b.label === label))
}
</script>

<template>
  <div class="tbl-scroll">
    <table :class="['tbl steps', { 'steps--compact': compact }]">
      <thead>
        <tr>
          <th>{{ t('scenarios.editor.columns.day') }}</th>
          <th>{{ t('scenarios.editor.columns.time') }}</th>
          <th v-if="!compact">{{ t('scenarios.editor.columns.sites') }}</th>
          <th v-if="!compact">{{ t('scenarios.editor.columns.kind') }}</th>
          <th>{{ t('scenarios.editor.columns.text') }}</th>
          <th>{{ t('scenarios.editor.columns.buttons') }}</th>
          <th class="td-num">{{ t('scenarios.editor.columns.branches') }}</th>
          <th>{{ t('scenarios.editor.columns.status') }}</th>
        </tr>
      </thead>
      <tbody>
        <template v-for="b in blocks" :key="`${b.day_offset}|${b.time}`">
          <tr
            v-for="(s, i) in b.steps"
            :key="s.id"
            :class="[
              'steps__row',
              {
                'steps__row--on': s.id === selectedId,
                'steps__row--err': errorsOf(s.id) > 0,
                'steps__row--warn': errorsOf(s.id) === 0 && warningsOf(s.id) > 0,
                'steps__row--first': i === 0,
                'steps__row--block': b.steps.length > 1,
              },
            ]"
            tabindex="0"
            :aria-selected="s.id === selectedId"
            @click="emit('select', s.id)"
            @keydown.enter.prevent="emit('select', s.id)"
          >
            <td
              v-if="i === 0"
              :rowspan="b.steps.length"
              class="tabular fw-medium block__when"
              @click.stop="emit('select', s.id)"
            >
              {{ formatDayOffset(s.day_offset) }}
              <div v-if="b.steps.length > 1" class="block__count">
                {{ t('scenarios.table.blockSize', b.steps.length) }}
              </div>
            </td>
            <td v-if="i === 0" :rowspan="b.steps.length" class="tabular block__when">
              {{ s.time }}
            </td>
            <td v-if="!compact" class="cell-muted">{{ sitesLabel(s) }}</td>
            <td v-if="!compact" class="cell-muted">{{ t(`common.stepKind.${s.kind}`) }}</td>
            <td class="cell-text">
              <div class="fw-medium title">
                <span>{{ s.title || t('scenarios.editor.untitled') }}</span>
                <Mail
                  v-if="s.hr_task"
                  :size="12"
                  class="ico"
                  :aria-label="t('scenarios.table.hrTask')"
                />
                <Flag
                  v-if="s.flag_required"
                  :size="12"
                  class="ico"
                  :aria-label="t('scenarios.table.recruiterFlag')"
                />
                <span
                  v-if="effectiveAdvance(s, i < b.steps.length - 1) === 'button'"
                  class="adv"
                  :title="t('scenarios.table.advanceButton')"
                  ><ArrowRight :size="12"
                /></span>
                <span
                  v-else-if="effectiveAdvance(s, i < b.steps.length - 1) === 'auto'"
                  class="adv adv--auto"
                  :title="t('scenarios.table.advanceAuto')"
                  ><Zap :size="12"
                /></span>
              </div>
              <div class="cell-preview">{{ s.text_vy || s.text_ty }}</div>
            </td>
            <td class="cell-chips">
              <span
                v-for="(btn, bi) in s.buttons"
                :key="bi"
                :class="['chip', { 'chip--branch': !!branchFor(s, bi) }]"
                :title="btn.label"
              >
                <GitBranch v-if="branchFor(s, bi)" :size="10" /><span class="chip__label">{{
                  btn.label || '…'
                }}</span>
                <span v-if="badge(s, btn.label)" class="chip__stat tabular">
                  {{ badge(s, btn.label)!.percent }}% ({{ badge(s, btn.label)!.picked }})
                </span>
              </span>
            </td>
            <td class="td-num tabular">
              <span v-if="s.buttons.length > 0">
                {{ branchCount(s).withBranch }}/{{ branchCount(s).total }}
              </span>
            </td>
            <td :title="statusTitle(s)">
              <span v-if="errorsOf(s.id) > 0" class="badge solid-danger">
                {{ t('scenarios.table.errors', errorsOf(s.id)) }}
              </span>
              <span v-else-if="warningsOf(s.id) > 0" class="badge solid-warning">
                {{ t('scenarios.table.warnings', warningsOf(s.id)) }}
              </span>
              <span v-else class="badge solid-success">{{ t('scenarios.issues.ok') }}</span>
            </td>
          </tr>
        </template>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
.tbl-scroll {
  overflow-x: auto;
}
.steps__row--first td {
  border-top: 2px solid var(--border);
}
.block__when {
  vertical-align: top;
  background: var(--bg-subtle);
  border-right: 1px solid var(--border-subtle);
}
.block__count {
  font-size: var(--text-xs);
  color: var(--fg-subtle);
  font-weight: 400;
}
.steps__row {
  cursor: pointer;
}
.steps__row:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: -2px;
}
.steps__row--on td:not(.block__when) {
  background: var(--accent-soft);
}
.steps__row--err td:last-child {
  box-shadow: inset -3px 0 0 var(--danger);
}
.steps__row--warn td:last-child {
  box-shadow: inset -3px 0 0 var(--warning);
}
.steps td {
  padding-top: 7px;
  padding-bottom: 7px;
}
.cell-muted {
  color: var(--fg-muted);
  font-size: var(--text-sm);
}
.cell-text {
  max-width: 280px;
}
.title {
  display: flex;
  align-items: center;
  gap: 4px;
}
.title > span:first-child {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.ico {
  flex: none;
  color: var(--fg-muted);
}
.adv {
  display: inline-flex;
  color: var(--fg-subtle);
}
.adv--auto {
  color: var(--warning);
}
.cell-preview {
  font-size: var(--text-sm);
  color: var(--fg-subtle);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.cell-chips {
  max-width: 260px;
}
.steps--compact .cell-text {
  max-width: 190px;
}
.steps--compact .cell-chips {
  max-width: 180px;
}
.steps--compact .chip__stat {
  display: none;
}
.chip {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  margin: 1px 3px 1px 0;
  padding: 1px 6px;
  border-radius: var(--radius-full);
  border: 1px solid var(--border);
  font-size: var(--text-xs);
  white-space: nowrap;
}
.chip {
  max-width: 220px;
}
.steps--compact .chip {
  max-width: 160px;
}
/* A long label is cut on screen with an ellipsis; the full one is in the title. */
.chip > svg,
.chip__stat {
  flex: none;
}
.chip__label {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
}
.chip--branch {
  border-color: var(--accent-border);
}
.chip__stat {
  color: var(--info);
  font-weight: 600;
}
</style>
