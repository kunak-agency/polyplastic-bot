<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { ArrowRight, TriangleAlert } from 'lucide-vue-next'
import { formatDayOffset, isoToLocalDate, type PlanPreview } from '@/lib/calendar'

defineProps<{ preview: PlanPreview }>()
const { t, d } = useI18n()

// A day with many steps shows the first ones by name and the rest as a count.
const SHOWN = 3
</script>

<template>
  <div class="plan">
    <ol class="plan__days">
      <li v-for="day in preview.days" :key="day.day_offset" class="plan__day">
        <span class="plan__offset tabular">{{ formatDayOffset(day.day_offset) }}</span>
        <div class="plan__body">
          <div class="plan__date">
            <span :class="{ 'plan__date--moved': day.movedTo }">
              {{ d(isoToLocalDate(day.planned), 'weekdayShort') }}
            </span>
            <template v-if="day.movedTo">
              <ArrowRight :size="12" aria-hidden="true" />
              <span class="plan__moved">
                {{
                  t('newcomers.preview.moved', {
                    date: d(isoToLocalDate(day.movedTo), 'weekdayShort'),
                  })
                }}
              </span>
            </template>
            <span class="plan__count">· {{ t('newcomers.preview.steps', day.steps.length) }}</span>
          </div>
          <div class="plan__titles">
            {{
              day.steps
                .slice(0, SHOWN)
                .map((s) => s.title)
                .join(', ')
            }}<template v-if="day.steps.length > SHOWN">, …</template>
          </div>
        </div>
      </li>
    </ol>
    <div v-if="preview.shiftedSteps > 0" class="plan__warning" role="note">
      <TriangleAlert :size="14" aria-hidden="true" />
      <span>{{ t('newcomers.preview.shiftedWarning', preview.shiftedSteps) }}</span>
    </div>
    <p class="plan__disclaimer">{{ t('newcomers.preview.disclaimer') }}</p>
  </div>
</template>

<style scoped>
.plan__days {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.plan__day {
  display: grid;
  grid-template-columns: 44px 1fr;
  gap: 8px;
  font-size: var(--text-sm);
}
.plan__offset {
  font-weight: 600;
  color: var(--fg);
}
.plan__date {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 4px;
  color: var(--fg);
}
.plan__date--moved {
  text-decoration: line-through;
  color: var(--fg-subtle);
}
.plan__moved {
  color: var(--warning);
  font-weight: 500;
}
.plan__count {
  color: var(--fg-subtle);
}
.plan__titles {
  color: var(--fg-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.plan__warning {
  display: flex;
  gap: 6px;
  align-items: flex-start;
  margin-top: 12px;
  padding: 8px 10px;
  border-radius: var(--radius-md);
  background: var(--warning-soft);
  color: var(--fg);
  font-size: var(--text-sm);
}
.plan__warning svg {
  color: var(--warning);
  flex: none;
  margin-top: 2px;
}
.plan__disclaimer {
  margin: 8px 0 0;
  font-size: var(--text-xs);
  color: var(--fg-subtle);
}
</style>
