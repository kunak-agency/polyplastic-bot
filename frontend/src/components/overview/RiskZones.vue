<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ShieldCheck } from 'lucide-vue-next'
import SectionCard from '@/components/common/SectionCard.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import RiskMark from '@/components/newcomers/RiskMark.vue'
import type { OverviewRiskZone } from '@/types/api'
import { dayLabel, filterRisks } from '@/lib/overview'

// "Risk zones" of ux-spec 3.1: every row carries its reason in words (Peakon
// pattern), colour is only a hint. Red first; the first seven, then "more".
const props = defineProps<{ items: OverviewRiskZone[]; total: number }>()
const overdueOnly = defineModel<boolean>('overdueOnly', { default: false })
const { t } = useI18n()

const PAGE = 7
const expanded = ref(false)
watch(overdueOnly, () => (expanded.value = false))

const filtered = computed(() => filterRisks(props.items, overdueOnly.value))
const shown = computed(() => (expanded.value ? filtered.value : filtered.value.slice(0, PAGE)))
const hasRed = computed(() => props.items.some((r) => r.zone === 'red'))
</script>

<template>
  <SectionCard body-padding="none">
    <template #title>
      {{ t('overview.risks.title') }}
      <span class="count">{{ overdueOnly ? filtered.length : total }}</span>
    </template>
    <template #desc>{{ t('overview.risks.desc') }}</template>
    <template #headActions>
      <label class="toggle txt-sm">
        <input v-model="overdueOnly" type="checkbox" />
        {{ t('overview.risks.overdueOnly') }}
      </label>
    </template>

    <EmptyState
      v-if="filtered.length === 0"
      :icon="ShieldCheck"
      :title="t('overview.risks.emptyTitle')"
      :subtitle="overdueOnly ? t('overview.risks.emptyOverdue') : t('overview.risks.emptyDesc')"
    />
    <ul v-else class="risks">
      <li v-for="r in shown" :key="r.newcomer_id" :class="['risk-row', `risk-row--${r.zone}`]">
        <div class="risk-row__head">
          <RiskMark :risk="r.zone" />
          <RouterLink
            class="risk-row__name"
            :to="{ name: 'newcomer', params: { id: r.newcomer_id } }"
          >
            {{ r.full_name }}
          </RouterLink>
          <span class="muted txt-sm">{{ r.site }} · {{ dayLabel(r.day) }}</span>
        </div>
        <ul class="risk-row__reasons">
          <li v-for="(reason, i) in r.reasons" :key="i">{{ reason }}</li>
        </ul>
      </li>
    </ul>

    <template v-if="filtered.length > 0" #foot>
      <div class="row gap-3 center foot">
        <button
          v-if="filtered.length > PAGE"
          type="button"
          class="btn btn-ghost btn-sm"
          @click="expanded = !expanded"
        >
          {{
            expanded
              ? t('overview.risks.less')
              : t('overview.risks.more', { n: filtered.length - PAGE })
          }}
        </button>
        <RouterLink
          v-if="hasRed"
          class="btn btn-outline btn-sm"
          :to="{ name: 'newcomers', query: { chips: 'red' } }"
        >
          {{ t('overview.risks.openAll') }}
        </RouterLink>
      </div>
    </template>
  </SectionCard>
</template>

<style scoped>
.count {
  margin-left: 6px;
  color: var(--fg-muted);
  font-weight: 500;
  font-variant-numeric: tabular-nums;
}
.toggle {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  color: var(--fg-muted);
  cursor: pointer;
  white-space: nowrap;
}
.risks {
  list-style: none;
  margin: 0;
  padding: 0;
}
.risk-row {
  padding: var(--space-3) var(--space-4);
  border-top: 1px solid var(--border-subtle);
  border-left: 3px solid transparent;
}
.risk-row:first-child {
  border-top: none;
}
.risk-row--red {
  border-left-color: var(--danger);
}
.risk-row--yellow {
  border-left-color: var(--warning);
}
.risk-row__head {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: 4px 10px;
}
.risk-row__name {
  font-weight: 500;
  color: var(--fg);
  text-decoration: none;
}
.risk-row__name:hover {
  text-decoration: underline;
}
.risk-row__reasons {
  margin: 4px 0 0;
  padding-left: 18px;
  font-size: var(--text-sm);
  color: var(--fg-muted);
}
.foot {
  justify-content: flex-end;
  width: 100%;
}
</style>
