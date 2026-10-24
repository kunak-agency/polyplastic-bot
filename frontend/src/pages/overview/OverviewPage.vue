<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { storeToRefs } from 'pinia'
import { useI18n } from 'vue-i18n'
import { ArrowRight, LayoutDashboard, RefreshCw } from 'lucide-vue-next'
import PageContainer from '@/components/common/PageContainer.vue'
import PageHead from '@/components/common/PageHead.vue'
import SectionCard from '@/components/common/SectionCard.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import ErrorState from '@/components/common/ErrorState.vue'
import LoadingState from '@/components/common/LoadingState.vue'
import IndicatorTiles from '@/components/overview/IndicatorTiles.vue'
import AdaptationFunnel from '@/components/overview/AdaptationFunnel.vue'
import SitesCompare from '@/components/overview/SitesCompare.vue'
import TopicHeatmap from '@/components/overview/TopicHeatmap.vue'
import TopicTrends from '@/components/overview/TopicTrends.vue'
import HRLoad from '@/components/overview/HRLoad.vue'
import { Button } from '@/components/ui/button'
import { useIndicatorsStore } from '@/stores/indicators'
import { useSiteStore } from '@/stores/site'
import { useAuthStore } from '@/stores/auth'
import { errorMessage } from '@/lib/apiError'
import { canOpen } from '@/lib/roles'
import { PERIODS, parsePeriod, todayTarget } from '@/lib/indicators'
import type { IndicatorPeriodKey, SiteCode } from '@/types/api'

// "Indicators" (research 2026-10-24-overview-metrics.md): adaptation outcomes
// for the HR lead over a period — outcome tiles, the cohort funnel, sites
// against the company average, blockers of the period and the HR load. No
// lists of people and no "today" actions here: one line links to "Today".
// The period lives in ?period=, the site in the topbar switcher.
const { t, d } = useI18n()
const route = useRoute()
const router = useRouter()
const store = useIndicatorsStore()
const { data, loading, error } = storeToRefs(store)
const site = useSiteStore()
const { current: currentSite } = storeToRefs(site)
const auth = useAuthStore()

const period = computed<IndicatorPeriodKey>(() => parsePeriod(route.query.period))

function setPeriod(p: IndicatorPeriodKey): void {
  void router.replace({ query: { ...route.query, period: p === '30d' ? undefined : p } })
}

function reload(): void {
  void store.load(site.apiSite(), period.value)
}
watch([currentSite, period], reload)
onMounted(reload)

function date(iso: string): string {
  return d(new Date(`${iso}T00:00:00`), 'dayMonth')
}

const headline = computed(() => {
  if (!data.value) return t('overview.subtitle')
  const s = data.value.site
  return t('overview.headline', {
    site: s ? t(`common.site.${s}`) : t('common.site.allSites'),
    from: date(data.value.period.from),
    to: date(data.value.period.to),
  })
})

const todayName = computed(() =>
  todayTarget(auth.status === 'authed' && canOpen(auth.viewer, 'dashboard')),
)

// Nobody in the flow at all (not just in this period): nothing to measure.
const empty = computed(
  () => !!data.value && data.value.cohort === 0 && data.value.hr_load.employees === 0,
)

// "What blocks <site>": narrow the screen to the site, then show its blockers.
const topicsEl = ref<HTMLElement | null>(null)
async function showBlockers(s: SiteCode): Promise<void> {
  site.setSite(s)
  await nextTick()
  topicsEl.value?.scrollIntoView({ behavior: 'smooth', block: 'start' })
}
</script>

<template>
  <PageContainer width="wide">
    <PageHead :title="t('overview.title')">
      <template #subtitle>{{ t('overview.subtitle') }} · {{ headline }}</template>
      <template #actions>
        <div class="tabs" role="group" :aria-label="t('overview.period.label')">
          <button
            v-for="p in PERIODS"
            :key="p"
            type="button"
            :class="['tab', { active: period === p }]"
            :aria-pressed="period === p"
            @click="setPeriod(p)"
          >
            {{ t(`overview.period.${p}`) }}
          </button>
        </div>
        <Button variant="outline" size="sm" :disabled="loading" @click="reload">
          <RefreshCw :size="14" aria-hidden="true" />{{ t('overview.refresh') }}
        </Button>
      </template>
    </PageHead>

    <SectionCard v-if="!data && (loading || !error)">
      <LoadingState variant="card" :rows="4" />
    </SectionCard>
    <SectionCard v-else-if="!data">
      <ErrorState :message="errorMessage(error, 'overview.loadError')">
        <template #retry>
          <Button size="sm" @click="reload">{{ t('common.actions.retry') }}</Button>
        </template>
      </ErrorState>
    </SectionCard>

    <SectionCard v-else-if="empty">
      <EmptyState
        :icon="LayoutDashboard"
        :title="t('overview.empty.title')"
        :subtitle="t('overview.empty.description')"
      >
        <template #action>
          <RouterLink class="btn btn-primary btn-sm" :to="{ name: 'newcomers' }">
            {{ t('overview.empty.action') }}
          </RouterLink>
        </template>
      </EmptyState>
    </SectionCard>

    <div v-else-if="data" :class="['ind', { 'ind--stale': loading }]">
      <div class="ind__bar">
        <span class="ind__cohort">
          <b>{{ t('overview.cohort', { n: data.cohort }) }}</b>
          <span v-if="data.synthetic > 0" class="muted">
            · {{ t('overview.synthetic', { n: data.synthetic }) }}
          </span>
        </span>
        <RouterLink class="ind__today" :to="{ name: todayName }">
          <span>
            {{
              data.today.total === 0
                ? t('overview.today.none')
                : t('overview.today.line', {
                    items: t('overview.today.items', data.today.total),
                    urgent:
                      data.today.urgent > 0
                        ? t('overview.today.urgent', data.today.urgent)
                        : t('overview.today.noUrgent'),
                  })
            }}
          </span>
          <span class="ind__open">
            {{ t('overview.today.open') }}<ArrowRight :size="14" aria-hidden="true" />
          </span>
        </RouterLink>
      </div>

      <IndicatorTiles :tiles="data.tiles" :min-group="data.min_group" />
      <AdaptationFunnel :funnel="data.funnel" :min-group="data.min_group" />
      <SitesCompare :data="data" :current="data.site" @blocker="showBlockers" />

      <div class="ind__grid">
        <div ref="topicsEl" class="ind__topics">
          <TopicHeatmap :heatmap="data.topics.heatmap" :title="t('overview.topics.title')">
            <TopicTrends :trends="data.topics.trends" :min-group="data.min_group" />
          </TopicHeatmap>
        </div>
        <HRLoad :load="data.hr_load" />
      </div>
    </div>
  </PageContainer>
</template>

<style scoped>
.ind {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  transition: opacity 0.15s;
}
.ind--stale {
  opacity: 0.6;
}
.ind__bar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-2) var(--space-4);
}
.ind__cohort {
  font-size: var(--text-sm);
}
.ind__today {
  display: inline-flex;
  align-items: center;
  gap: var(--space-3);
  padding: 8px var(--space-4);
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  background: var(--surface);
  color: var(--fg);
  font-size: var(--text-sm);
  text-decoration: none;
}
.ind__today:hover {
  border-color: var(--border-strong);
}
.ind__open {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  color: var(--accent);
  font-weight: 500;
}
.ind__grid {
  display: grid;
  grid-template-columns: minmax(0, 7fr) minmax(0, 4fr);
  gap: var(--space-4);
  align-items: start;
}
.ind__topics {
  scroll-margin-top: var(--space-4);
}
@media (max-width: 1100px) {
  .ind__grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
