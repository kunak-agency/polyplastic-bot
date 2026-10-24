<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import SectionCard from '@/components/common/SectionCard.vue'
import type { FunnelStep, Indicators } from '@/types/api'
import { FUNNEL_PERCENT_MIN, lossTarget } from '@/lib/indicators'

// Adaptation funnel of the period's cohort (research 2026-10-24): invited →
// connected before D0 → passed D0 → active at D+30 → active at D+90 → probation
// decision. Under each step: lost, with the reason in words; those whose day
// has not come yet are "not there yet", not lost. Below 20 people — numbers
// only; below min_group — only "invited".
const props = defineProps<{ funnel: Indicators['funnel']; minGroup: number }>()
const { t } = useI18n()

const max = computed(() => Math.max(1, props.funnel.cohort))

// Nobody reached the step and nobody was lost on it (its day has not come
// for anyone yet): a dash, not "0 · 0 %".
function notYet(s: FunnelStep): boolean {
  return s.count === 0 && s.lost.length === 0
}

function width(count: number | null): string {
  if (count === null) return '0%'
  return `${Math.max(2, (count / max.value) * 100)}%`
}
</script>

<template>
  <SectionCard :title="t('overview.funnel.title')" :desc="t('overview.funnel.desc')">
    <ol class="funnel">
      <li v-for="s in funnel.steps" :key="s.key" :class="['step', { 'step--hidden': s.hidden }]">
        <div class="step__head">
          <span class="step__label">{{ t(`overview.funnel.steps.${s.key}`) }}</span>
          <span class="step__value">
            <template v-if="s.hidden">{{
              t('overview.funnel.hidden', { min: minGroup })
            }}</template>
            <template v-else-if="notYet(s)">—</template>
            <template v-else>
              {{ s.count
              }}<span v-if="s.percent !== null" class="step__pct"> · {{ s.percent }} %</span>
            </template>
          </span>
        </div>
        <div class="bar" aria-hidden="true">
          <span class="bar__fill" :style="{ width: width(s.count) }" />
        </div>
        <ul v-if="!s.hidden && (s.lost.length > 0 || s.pending > 0)" class="step__notes">
          <li v-for="l in s.lost" :key="l.reason">
            <RouterLink
              v-if="lossTarget(l.reason)"
              :to="{ name: lossTarget(l.reason)!.name, query: lossTarget(l.reason)!.query }"
              class="step__loss step__loss--link"
            >
              {{
                t('overview.funnel.lost', {
                  n: l.count,
                  reason: t(`overview.funnel.reasons.${l.reason}`),
                })
              }}
            </RouterLink>
            <span v-else class="step__loss">
              {{
                t('overview.funnel.lost', {
                  n: l.count,
                  reason: t(`overview.funnel.reasons.${l.reason}`),
                })
              }}
            </span>
          </li>
          <li v-if="s.pending > 0" class="step__pending">
            {{ t('overview.funnel.pending', { n: s.pending }) }}
          </li>
        </ul>
      </li>
    </ol>
    <p class="note txt-xs muted">
      <template v-if="funnel.hidden">{{ t('overview.funnel.small', { min: minGroup }) }}</template>
      <template v-else-if="!funnel.percents">{{
        t('overview.funnel.absolute', { n: FUNNEL_PERCENT_MIN })
      }}</template>
      {{ t('overview.funnel.activeDef') }}
    </p>
  </SectionCard>
</template>

<style scoped>
.funnel {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  grid-template-columns: repeat(6, minmax(0, 1fr));
  gap: var(--space-3);
}
@media (max-width: 1100px) {
  .funnel {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }
}
@media (max-width: 640px) {
  .funnel {
    grid-template-columns: minmax(0, 1fr);
  }
}
.step {
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 0;
}
.step__head {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.step__label {
  font-size: var(--text-sm);
  color: var(--fg-muted);
}
.step__value {
  font-size: var(--text-xl);
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}
.step__pct {
  font-size: var(--text-sm);
  font-weight: 500;
  color: var(--fg-subtle);
}
.step--hidden .step__value {
  color: var(--fg-faint);
}
.bar {
  height: 8px;
  border-radius: 4px;
  background: var(--bg-subtle);
  overflow: hidden;
}
.bar__fill {
  display: block;
  height: 100%;
  border-radius: 4px;
  background: #3987e5;
}
.step__notes {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
  font-size: var(--text-xs);
}
.step__loss {
  color: var(--fg-muted);
}
.step__loss--link {
  text-decoration: underline;
  text-decoration-color: var(--border-strong);
  text-underline-offset: 3px;
}
.step__loss--link:hover {
  color: var(--fg);
}
.step__pending {
  color: var(--fg-subtle);
}
.note {
  margin: var(--space-3) 0 0;
}
</style>
