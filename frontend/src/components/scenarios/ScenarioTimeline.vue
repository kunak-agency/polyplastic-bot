<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  ArrowDown,
  ArrowUp,
  CircleAlert,
  Flag,
  GitBranch,
  GripVertical,
  ListChecks,
  Mail,
  Plus,
  TriangleAlert,
} from 'lucide-vue-next'
import { formatDayOffset } from '@/lib/calendar'
import { branchCount, groupBlocks, type Issue, type StepBlock } from '@/lib/scenarioBranches'
import type { ScenarioStep } from '@/types/api'

// The "Timeline" view (ux-spec v1, Enboarder): the same steps as the table,
// laid out by day — a column per day, the blocks of the day by time, a card per
// step with its kind, sites, branches and status. A click opens the step panel.
// Inside a block a card is dragged to another place (or moved with the arrows
// under the selected card, Alt+↑/↓ on the card); "+" inserts a message after.
const props = defineProps<{
  steps: ScenarioStep[]
  selectedId: string | null
  issues: Map<string, Issue[]>
  legacy: Map<string, string[]>
}>()
const emit = defineEmits<{
  select: [id: string]
  move: [id: string, by: -1 | 1]
  reorder: [id: string, targetId: string]
  'insert-after': [id: string]
}>()
const { t } = useI18n()

const days = computed(() => {
  const out: { day: number; blocks: StepBlock[] }[] = []
  for (const b of groupBlocks(props.steps)) {
    const last = out[out.length - 1]
    if (last && last.day === b.day_offset) last.blocks.push(b)
    else out.push({ day: b.day_offset, blocks: [b] })
  }
  return out
})

// Drag and drop stays inside the block the card came from.
// The dropped card takes the target's place: a line above the target when
// it comes from below, under it when it comes from above.
const drag = ref<{ id: string; block: string; index: number } | null>(null)
const over = ref<string | null>(null)
function blockOf(b: StepBlock): string {
  return `${b.day_offset}|${b.time}`
}
function onDragStart(e: DragEvent, b: StepBlock, id: string): void {
  drag.value = { id, block: blockOf(b), index: b.steps.findIndex((s) => s.id === id) }
  if (e.dataTransfer) {
    e.dataTransfer.effectAllowed = 'move'
    e.dataTransfer.setData('text/plain', id)
  }
}
function onDragOver(e: DragEvent, b: StepBlock, id: string): void {
  if (!drag.value || drag.value.block !== blockOf(b)) return
  e.preventDefault()
  if (e.dataTransfer) e.dataTransfer.dropEffect = 'move'
  over.value = id === drag.value.id ? null : id
}
function onDrop(e: DragEvent, b: StepBlock, id: string): void {
  const d = drag.value
  onDragEnd()
  if (!d || d.block !== blockOf(b) || d.id === id) return
  e.preventDefault()
  emit('reorder', d.id, id)
}
function onDragEnd(): void {
  drag.value = null
  over.value = null
}
function stepName(s: ScenarioStep): string {
  return s.title || t('scenarios.editor.untitled')
}

function level(id: string): 'error' | 'warning' | 'ok' {
  const list = props.issues.get(id) ?? []
  if (list.some((i) => i.level === 'error')) return 'error'
  if (list.length > 0 || (props.legacy.get(id) ?? []).length > 0) return 'warning'
  return 'ok'
}
function sitesShort(s: ScenarioStep): string {
  return s.sites.length === 0 ? t('scenarios.allSites') : s.sites.join(' · ')
}
</script>

<template>
  <div class="tl" role="list" :aria-label="t('scenarios.editor.views.timeline')">
    <section v-for="d in days" :key="d.day" class="tl__day" role="listitem">
      <header :class="['tl__head', { 'tl__head--zero': d.day === 0 }]">
        <span class="tabular fw-semi">{{ formatDayOffset(d.day) }}</span>
        <span v-if="d.day === 0" class="tl__mark">{{ t('scenarios.timeline.firstDay') }}</span>
        <span class="tl__n">{{
          t('scenarios.editor.stepsCount', d.blocks.flatMap((b) => b.steps).length)
        }}</span>
      </header>
      <div v-for="b in d.blocks" :key="`${b.day_offset}|${b.time}`" class="tl__block">
        <div class="tl__time tabular">{{ b.time }}</div>
        <div
          v-for="(s, i) in b.steps"
          :key="s.id"
          :class="[
            'tl__slot',
            {
              'tl__slot--before': over === s.id && (drag?.index ?? 0) > i,
              'tl__slot--after': over === s.id && (drag?.index ?? 0) < i,
              'tl__slot--dragging': drag?.id === s.id,
            },
          ]"
          @dragover="onDragOver($event, b, s.id)"
          @dragleave="over = over === s.id ? null : over"
          @drop="onDrop($event, b, s.id)"
        >
          <button
            type="button"
            :class="[
              'tl__card',
              `tl__card--${level(s.id)}`,
              { 'tl__card--on': s.id === selectedId, 'tl__card--hr': !!s.hr_task },
            ]"
            :draggable="b.steps.length > 1"
            :aria-pressed="s.id === selectedId"
            :title="
              b.steps.length > 1 ? `${stepName(s)} · ${t('scenarios.order.dragHint')}` : stepName(s)
            "
            @click="emit('select', s.id)"
            @keydown.alt.up.prevent="emit('move', s.id, -1)"
            @keydown.alt.down.prevent="emit('move', s.id, 1)"
            @dragstart="onDragStart($event, b, s.id)"
            @dragend="onDragEnd"
          >
            <span class="tl__title">
              <GripVertical v-if="b.steps.length > 1" :size="12" class="tl__grip" />
              <Mail v-if="s.hr_task" :size="12" class="tl__ico" />
              <ListChecks v-else-if="s.kind === 'survey'" :size="12" class="tl__ico" />
              <span class="tl__text">{{ s.title || t('scenarios.editor.untitled') }}</span>
            </span>
            <span class="tl__meta">
              <span>{{ t(`common.stepKind.${s.kind}`) }}</span>
              <span>{{ sitesShort(s) }}</span>
              <span v-if="s.buttons.length > 0" class="tl__br">
                <GitBranch :size="10" />{{ branchCount(s).withBranch }}/{{ branchCount(s).total }}
              </span>
              <Flag v-if="s.flag_required" :size="10" />
              <CircleAlert v-if="level(s.id) === 'error'" :size="11" class="tl__err" />
              <TriangleAlert v-else-if="level(s.id) === 'warning'" :size="11" class="tl__warn" />
            </span>
          </button>
          <div v-if="s.id === selectedId" class="tl__tools">
            <template v-if="b.steps.length > 1">
              <button
                type="button"
                class="tl__tool"
                :disabled="i === 0"
                :title="t('scenarios.order.up')"
                :aria-label="t('scenarios.order.upNamed', { title: stepName(s) })"
                @click="emit('move', s.id, -1)"
              >
                <ArrowUp :size="12" />
              </button>
              <button
                type="button"
                class="tl__tool"
                :disabled="i === b.steps.length - 1"
                :title="t('scenarios.order.down')"
                :aria-label="t('scenarios.order.downNamed', { title: stepName(s) })"
                @click="emit('move', s.id, 1)"
              >
                <ArrowDown :size="12" />
              </button>
            </template>
            <button
              type="button"
              class="tl__tool tl__tool--wide"
              :aria-label="t('scenarios.order.insertAfterNamed', { title: stepName(s) })"
              @click="emit('insert-after', s.id)"
            >
              <Plus :size="12" />{{ t('scenarios.order.insertAfterShort') }}
            </button>
          </div>
        </div>
      </div>
    </section>
  </div>
</template>

<style scoped>
.tl {
  display: flex;
  gap: 10px;
  overflow-x: auto;
  padding: 12px;
  align-items: flex-start;
}
.tl__day {
  flex: 0 0 200px;
  display: flex;
  flex-direction: column;
  gap: 8px;
  min-width: 0;
}
.tl__head {
  display: flex;
  align-items: baseline;
  gap: 6px;
  padding-bottom: 6px;
  border-bottom: 2px solid var(--border);
}
.tl__head--zero {
  border-bottom-color: var(--accent);
}
.tl__mark {
  font-size: var(--text-xs);
  color: var(--accent);
}
.tl__n {
  margin-left: auto;
  font-size: var(--text-xs);
  color: var(--fg-subtle);
}
.tl__block {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.tl__time {
  font-size: var(--text-xs);
  color: var(--fg-muted);
}
.tl__slot {
  display: flex;
  flex-direction: column;
  gap: 2px;
  border-radius: var(--radius-md);
}
.tl__slot--before {
  box-shadow: 0 -3px 0 var(--accent);
}
.tl__slot--after {
  box-shadow: 0 3px 0 var(--accent);
}
.tl__slot--dragging {
  opacity: 0.5;
}
.tl__grip {
  flex: none;
  color: var(--fg-faint);
  cursor: grab;
}
.tl__tools {
  display: flex;
  gap: 2px;
}
.tl__tool {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 3px;
  height: 22px;
  min-width: 24px;
  padding: 0 4px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface);
  color: var(--fg-muted);
  font-size: var(--text-xs);
  cursor: pointer;
}
.tl__tool--wide {
  margin-left: auto;
  white-space: nowrap;
}
.tl__tool:hover:not(:disabled) {
  color: var(--fg);
  border-color: var(--border-strong);
}
.tl__tool:disabled {
  opacity: 0.35;
  cursor: default;
}
.tl__card {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
  padding: 6px 8px;
  border: 1px solid var(--border);
  border-left-width: 3px;
  border-radius: var(--radius-md);
  background: var(--surface);
  text-align: left;
  cursor: pointer;
}
.tl__card:hover {
  border-color: var(--border-strong);
}
.tl__card--warning {
  border-left-color: var(--warning);
}
.tl__card--error {
  border-left-color: var(--danger);
}
.tl__card--ok {
  border-left-color: var(--success);
}
.tl__card--hr {
  background: var(--bg-subtle);
  border-style: dashed;
}
.tl__card--on {
  background: var(--accent-soft);
  border-color: var(--accent);
}
.tl__title {
  display: flex;
  align-items: center;
  gap: 4px;
  min-width: 0;
  font-size: var(--text-sm);
  font-weight: 500;
}
.tl__text {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.tl__ico {
  flex: none;
  color: var(--fg-muted);
}
.tl__meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 2px 8px;
  font-size: var(--text-xs);
  color: var(--fg-subtle);
}
.tl__br {
  display: inline-flex;
  align-items: center;
  gap: 2px;
}
.tl__err {
  color: var(--danger);
}
.tl__warn {
  color: var(--warning);
}
</style>
