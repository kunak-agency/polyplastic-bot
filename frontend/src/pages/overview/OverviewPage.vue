<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { storeToRefs } from 'pinia'
import { useI18n } from 'vue-i18n'
import { LayoutDashboard, RefreshCw } from 'lucide-vue-next'
import PageContainer from '@/components/common/PageContainer.vue'
import PageHead from '@/components/common/PageHead.vue'
import SectionCard from '@/components/common/SectionCard.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import ErrorState from '@/components/common/ErrorState.vue'
import LoadingState from '@/components/common/LoadingState.vue'
import OverviewTiles from '@/components/overview/OverviewTiles.vue'
import RiskZones from '@/components/overview/RiskZones.vue'
import ActionToday from '@/components/overview/ActionToday.vue'
import TopicHeatmap from '@/components/overview/TopicHeatmap.vue'
import PulseTrend from '@/components/overview/PulseTrend.vue'
import { Button } from '@/components/ui/button'
import { useOverviewStore } from '@/stores/overview'
import { useSiteStore } from '@/stores/site'
import { errorMessage } from '@/lib/apiError'

// HR dashboard of ux-spec 3.1: tiles with drill-down, risk zones with the
// reason in words, topic heat map and mood trend (3.7), "need action today".
// The topbar site switcher narrows every block.
const { t, d } = useI18n()
const store = useOverviewStore()
const { data, loading, error } = storeToRefs(store)
const site = useSiteStore()
const { current: currentSite } = storeToRefs(site)

function reload(): void {
  void store.load(site.apiSite())
}
watch(currentSite, reload)
onMounted(reload)

const overdueOnly = ref(false)
const risksEl = ref<HTMLElement | null>(null)
async function showOverdue(): Promise<void> {
  overdueOnly.value = true
  await nextTick()
  risksEl.value?.scrollIntoView({ behavior: 'smooth', block: 'start' })
}

function date(iso: string): string {
  return d(new Date(`${iso}T00:00:00`), 'dayMonth')
}

const subtitle = computed(() => {
  if (!data.value) return t('overview.subtitle')
  const s = data.value.site
  return t('overview.week', {
    site: s ? t(`common.site.${s}`) : t('common.site.allSites'),
    from: date(data.value.week.from),
    to: date(data.value.week.to),
  })
})

const empty = computed(
  () => !!data.value && data.value.tiles.in_adaptation === 0 && data.value.risk_zones.total === 0,
)
</script>

<template>
  <PageContainer width="wide">
    <PageHead :title="t('overview.title')">
      <template #subtitle>{{ subtitle }}</template>
      <template #actions>
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

    <div v-else-if="data" :class="['overview', { 'overview--stale': loading }]">
      <OverviewTiles :tiles="data.tiles" :stages="data.stages" @overdue="showOverdue" />
      <ActionToday :items="data.action_today" @overdue="showOverdue" />
      <div class="overview__grid">
        <div ref="risksEl" class="overview__risks">
          <RiskZones
            v-model:overdue-only="overdueOnly"
            :items="data.risk_zones.items"
            :total="data.risk_zones.total"
          />
        </div>
        <div class="overview__charts">
          <PulseTrend :trend="data.trend" />
          <TopicHeatmap :heatmap="data.heatmap" />
        </div>
      </div>
    </div>
  </PageContainer>
</template>

<style scoped>
.overview {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  transition: opacity 0.15s;
}
.overview--stale {
  opacity: 0.6;
}
.overview__grid {
  display: grid;
  grid-template-columns: minmax(0, 5fr) minmax(0, 6fr);
  gap: var(--space-4);
  align-items: start;
}
.overview__risks {
  scroll-margin-top: var(--space-4);
}
.overview__charts {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}
@media (max-width: 1100px) {
  .overview__grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
