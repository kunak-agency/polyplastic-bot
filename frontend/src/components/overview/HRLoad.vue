<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import SectionCard from '@/components/common/SectionCard.vue'
import type { Indicators } from '@/types/api'
import { splitMinutes } from '@/lib/indicators'

// What the bot took off people (research 2026-10-24, "HR load"): by the team
// as a whole, never by recruiter. Below min_group — "too little data".
const props = defineProps<{ load: Indicators['hr_load'] }>()
const { t, n } = useI18n()

const median = computed(() => {
  const v = props.load.first_response.value
  if (v === null) return null
  const { h, m } = splitMinutes(v)
  return h > 0 ? t('overview.tiles.hours', { h, m }) : t('overview.tiles.minutes', { m })
})
</script>

<template>
  <SectionCard :title="t('overview.hrLoad.title')" :desc="t('overview.hrLoad.desc')">
    <dl class="load">
      <div class="load__row">
        <dt>{{ t('overview.hrLoad.per100') }}</dt>
        <dd>
          <RouterLink :to="{ name: 'tickets' }" class="load__v">
            {{ load.per_100 !== null ? n(load.per_100, 'decimal') : t('overview.hrLoad.few') }}
          </RouterLink>
          <span class="load__sub">
            {{ t('overview.hrLoad.per100Hint', { tickets: load.tickets, n: load.employees }) }}
          </span>
        </dd>
      </div>
      <div class="load__row">
        <dt>{{ t('overview.hrLoad.botClosed') }}</dt>
        <dd>
          <span class="load__v">
            {{
              load.bot_closed.value !== null
                ? t('overview.tiles.percent', { v: load.bot_closed.value })
                : t('overview.hrLoad.few')
            }}
          </span>
          <span class="load__sub">
            {{
              t('overview.tiles.hints.bot_closed', {
                num: load.bot_closed.num,
                n: load.bot_closed.n,
              })
            }}
          </span>
        </dd>
      </div>
      <div class="load__row">
        <dt>{{ t('overview.hrLoad.median') }}</dt>
        <dd>
          <span class="load__v">{{ median ?? t('overview.hrLoad.few') }}</span>
          <span class="load__sub">
            {{ t('overview.tiles.hints.first_response', { n: load.first_response.n }) }}
          </span>
        </dd>
      </div>
      <div class="load__row">
        <dt>{{ t('overview.hrLoad.sla') }}</dt>
        <dd>
          <span class="load__v">
            {{
              load.first_response.sla_pct !== null && load.first_response.sla_pct !== undefined
                ? t('overview.tiles.percent', { v: load.first_response.sla_pct })
                : t('overview.hrLoad.few')
            }}
          </span>
          <span class="load__sub">{{
            t('overview.sites.n', { n: load.first_response.sla_n ?? 0 })
          }}</span>
        </dd>
      </div>
      <div v-if="load.kb_gaps !== null" class="load__row">
        <dt>{{ t('overview.hrLoad.kbGaps') }}</dt>
        <dd>
          <RouterLink :to="{ name: 'knowledge', query: { tab: 'unanswered' } }" class="load__v">
            {{ load.kb_gaps }}
          </RouterLink>
          <span class="load__sub">{{ t('overview.hrLoad.kbHint', { n: load.kb_questions }) }}</span>
        </dd>
      </div>
    </dl>
  </SectionCard>
</template>

<style scoped>
.load {
  margin: 0;
  display: flex;
  flex-direction: column;
}
.load__row {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  gap: var(--space-3);
  padding: 10px 0;
  border-bottom: 1px solid var(--border-subtle);
}
.load__row:last-child {
  border-bottom: none;
}
.load dt {
  font-size: var(--text-sm);
  color: var(--fg-muted);
}
.load dd {
  margin: 0;
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  text-align: right;
}
.load__v {
  font-weight: 600;
  font-size: var(--text-lg);
  font-variant-numeric: tabular-nums;
  color: var(--fg);
  text-decoration: none;
}
a.load__v:hover {
  text-decoration: underline;
  text-underline-offset: 3px;
}
.load__sub {
  font-size: var(--text-xs);
  color: var(--fg-subtle);
}
</style>
