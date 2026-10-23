<script setup lang="ts">
import { computed, type Component } from 'vue'
import { useI18n } from 'vue-i18n'
import { Check, CheckCheck, Circle, CircleSlash, TriangleAlert } from 'lucide-vue-next'
import { formatDayOffset } from '@/lib/calendar'
import type { EventStatus, TimelineEvent } from '@/types/api'

const props = defineProps<{ items: TimelineEvent[]; today: number }>()
const { t, d } = useI18n()

const ICONS: Record<EventStatus, Component> = {
  scheduled: Circle,
  sent: Check,
  answered: CheckCheck,
  skipped: CircleSlash,
  failed: TriangleAlert,
}

// One group per day of the scenario, in plan order; the server returns events
// flat, sorted or not.
const groups = computed(() => {
  const sorted = props.items
    .slice()
    .sort((a, b) => a.day_offset - b.day_offset || a.fire_at.localeCompare(b.fire_at))
  const out: { day: number; events: TimelineEvent[] }[] = []
  for (const e of sorted) {
    const last = out[out.length - 1]
    if (last && last.day === e.day_offset) last.events.push(e)
    else out.push({ day: e.day_offset, events: [e] })
  }
  return out
})
</script>

<template>
  <ol class="tl">
    <li
      v-for="g in groups"
      :key="g.day"
      :class="['tl__day', { 'tl__day--today': g.day === today, 'tl__day--future': g.day > today }]"
    >
      <div class="tl__day-label">
        <span class="tabular">{{ formatDayOffset(g.day) }}</span>
        <span v-if="g.day === today" class="tl__today">{{ t('newcomers.timeline.today') }}</span>
      </div>
      <ol class="tl__events">
        <li
          v-for="e in g.events"
          :key="e.event_id"
          :class="['tl__event', `tl__event--${e.status}`]"
        >
          <component :is="ICONS[e.status]" class="tl__icon" :size="16" aria-hidden="true" />
          <div class="tl__body">
            <div class="tl__title">
              <span class="fw-medium">{{ e.title }}</span>
              <span class="tl__kind">{{ t(`common.stepKind.${e.kind}`) }}</span>
            </div>
            <div class="tl__meta">
              <span class="tabular"
                >{{ d(new Date(e.fire_at), 'weekdayShort') }} ·
                {{ d(new Date(e.fire_at), 'time') }}</span
              >
              <span class="tl__status">{{ t(`newcomers.timeline.status.${e.status}`) }}</span>
            </div>
            <div v-if="e.answer" class="tl__answer">
              {{ t('newcomers.timeline.answer', { answer: e.answer }) }}
            </div>
            <details v-if="e.text" class="tl__text">
              <summary>{{ t('newcomers.timeline.sentText') }}</summary>
              <p>{{ e.text }}</p>
            </details>
          </div>
        </li>
      </ol>
    </li>
  </ol>
</template>

<style scoped>
.tl {
  list-style: none;
  margin: 0;
  padding: 0;
}
.tl__day {
  display: grid;
  grid-template-columns: 64px 1fr;
  gap: 12px;
  padding: 10px 0;
  border-bottom: 1px solid var(--border-subtle);
}
.tl__day:last-child {
  border-bottom: 0;
}
.tl__day-label {
  display: flex;
  flex-direction: column;
  gap: 2px;
  font-weight: 600;
  font-size: var(--text-sm);
  padding-top: 2px;
}
.tl__today {
  font-size: var(--text-xs);
  font-weight: 500;
  color: var(--accent-fg);
  background: var(--accent);
  border-radius: var(--radius-sm);
  padding: 1px 6px;
  width: fit-content;
}
.tl__day--today {
  background: var(--bg-subtle);
  margin: 0 -16px;
  padding-left: 16px;
  padding-right: 16px;
}
.tl__events {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 10px;
  position: relative;
}
.tl__event {
  display: grid;
  grid-template-columns: 16px 1fr;
  gap: 10px;
  position: relative;
}
/* The vertical rail between icons. */
.tl__event:not(:last-child)::before {
  content: '';
  position: absolute;
  left: 7.5px;
  top: 20px;
  bottom: -10px;
  width: 1px;
  background: var(--border);
}
.tl__icon {
  margin-top: 1px;
  color: var(--fg-faint);
  background: var(--surface);
  position: relative;
}
.tl__event--sent .tl__icon,
.tl__event--answered .tl__icon {
  color: var(--success);
}
.tl__event--failed .tl__icon {
  color: var(--danger);
}
.tl__event--skipped .tl__icon {
  color: var(--fg-subtle);
}
.tl__title {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: 8px;
}
.tl__kind {
  font-size: var(--text-xs);
  color: var(--fg-subtle);
}
.tl__meta {
  display: flex;
  gap: 8px;
  font-size: var(--text-sm);
  color: var(--fg-subtle);
}
.tl__event--failed .tl__status {
  color: var(--danger);
}
.tl__event--answered .tl__status,
.tl__event--sent .tl__status {
  color: var(--success);
}
.tl__answer {
  margin-top: 2px;
  font-size: var(--text-sm);
  color: var(--fg-muted);
}
.tl__text {
  margin-top: 2px;
  font-size: var(--text-sm);
  color: var(--fg-muted);
}
.tl__text summary {
  cursor: pointer;
  width: fit-content;
}
.tl__text p {
  margin: 4px 0 0;
  white-space: pre-wrap;
  max-width: 72ch;
}
.tl__day--future .tl__title {
  color: var(--fg-muted);
}
</style>
