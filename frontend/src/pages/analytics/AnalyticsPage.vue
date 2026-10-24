<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { storeToRefs } from 'pinia'
import { useI18n } from 'vue-i18n'
import { CalendarClock, ChartColumn, ChevronLeft, ChevronRight, RefreshCw } from 'lucide-vue-next'
import PageContainer from '@/components/common/PageContainer.vue'
import PageHead from '@/components/common/PageHead.vue'
import SectionCard from '@/components/common/SectionCard.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import ErrorState from '@/components/common/ErrorState.vue'
import LoadingState from '@/components/common/LoadingState.vue'
import RiskZones from '@/components/overview/RiskZones.vue'
import PulseTrend from '@/components/overview/PulseTrend.vue'
import DigestSummary from '@/components/digest/DigestSummary.vue'
import DigestThemes from '@/components/digest/DigestThemes.vue'
import DigestSites from '@/components/digest/DigestSites.vue'
import DigestMaxPreview from '@/components/digest/DigestMaxPreview.vue'
import { Button } from '@/components/ui/button'
import { apiGetDigest } from '@/api/digest'
import { useSiteStore } from '@/stores/site'
import { errorMessage } from '@/lib/apiError'
import { isoWeek, shiftWeek } from '@/lib/digest'
import type { Digest } from '@/types/api'

// "Analytics" of ux-spec 3.7 as the weekly digest for the head of recruitment
// (case role 04): summary, themes of the week, risk zones, sites, dynamics to
// the previous week and the MAX message. The week lives in the route query, so
// the "Open analytics" button of the MAX message lands on the same week.
const { t, d } = useI18n()
const route = useRoute()
const router = useRouter()
const site = useSiteStore()
const { current: currentSite } = storeToRefs(site)

const thisWeek = isoWeek(new Date())
const week = computed(() =>
  typeof route.query.week === 'string' && route.query.week ? route.query.week : thisWeek,
)

const data = ref<Digest | null>(null)
const loading = ref(false)
const error = ref<unknown>(null)
let ticket = 0

async function load(): Promise<void> {
  const mine = ++ticket
  loading.value = true
  error.value = null
  try {
    const r = await apiGetDigest(week.value, site.apiSite())
    if (mine === ticket) data.value = r
  } catch (e) {
    if (mine === ticket) error.value = e
  } finally {
    if (mine === ticket) loading.value = false
  }
}

function go(n: number): void {
  const w = n === 0 ? thisWeek : shiftWeek(week.value, n)
  void router.replace({ query: { ...route.query, week: w } })
}

onMounted(() => {
  const q = route.query.site
  if (typeof q === 'string' && ['GrPP', 'Luzhniki', 'GTP'].includes(q)) {
    site.setSite(q as 'GrPP' | 'Luzhniki' | 'GTP')
  }
  void load()
})
watch([week, currentSite], () => void load())

function date(iso: string): string {
  return d(new Date(`${iso}T00:00:00`), 'dayMonth')
}

const subtitle = computed(() => {
  if (!data.value) return t('analytics.subtitle')
  const s = data.value.site
  const base = t('analytics.week', {
    site: s ? t(`common.site.${s}`) : t('common.site.allSites'),
    from: date(data.value.from),
    to: date(data.value.to),
  })
  return data.value.partial ? `${base} · ${t('analytics.partial')}` : base
})

const empty = computed(
  () =>
    !!data.value &&
    data.value.totals.on_pulse === 0 &&
    data.value.totals.problems === 0 &&
    data.value.themes.length === 0,
)
</script>

<template>
  <PageContainer width="wide">
    <PageHead :title="t('analytics.title')">
      <template #subtitle>{{ subtitle }}</template>
      <template #actions>
        <div class="weeks">
          <Button variant="outline" size="sm" :aria-label="t('analytics.prevWeek')" @click="go(-1)">
            <ChevronLeft :size="14" />
          </Button>
          <span class="weeks__label">{{ week }}</span>
          <Button
            variant="outline"
            size="sm"
            :aria-label="t('analytics.nextWeek')"
            :disabled="week >= thisWeek"
            @click="go(1)"
          >
            <ChevronRight :size="14" />
          </Button>
          <Button variant="ghost" size="sm" :disabled="week === thisWeek" @click="go(0)">
            <CalendarClock :size="14" />{{ t('analytics.currentWeek') }}
          </Button>
        </div>
        <Button variant="outline" size="sm" :disabled="loading" @click="load">
          <RefreshCw :size="14" aria-hidden="true" />{{ t('analytics.refresh') }}
        </Button>
      </template>
    </PageHead>

    <SectionCard v-if="!data && (loading || !error)">
      <LoadingState variant="card" :rows="4" />
    </SectionCard>
    <SectionCard v-else-if="!data">
      <ErrorState :message="errorMessage(error, 'analytics.loadError')">
        <template #retry>
          <Button size="sm" @click="load">{{ t('common.actions.retry') }}</Button>
        </template>
      </ErrorState>
    </SectionCard>
    <SectionCard v-else-if="empty">
      <EmptyState
        :icon="ChartColumn"
        :title="t('analytics.empty.title')"
        :subtitle="t('analytics.empty.description')"
      />
    </SectionCard>

    <div v-else-if="data" :class="['digest', { 'digest--stale': loading }]">
      <DigestSummary :digest="data" />
      <div class="digest__grid">
        <div class="digest__col">
          <DigestThemes :digest="data" />
          <DigestSites :digest="data" />
          <!-- Mood by weeks moved here from "Indicators" (research 2026-10-24). -->
          <PulseTrend v-if="data.pulse_trend" :trend="data.pulse_trend" />
        </div>
        <div class="digest__col">
          <div class="probation txt-sm">
            {{
              t(
                data.probation.this_week
                  ? 'analytics.probation.thisWeek'
                  : 'analytics.probation.nextWeek',
                {
                  n: data.probation.count,
                },
              )
            }}
          </div>
          <RiskZones :items="data.risk_zones.items" :total="data.risk_zones.items.length" />
          <DigestMaxPreview :digest="data" />
        </div>
      </div>
    </div>
  </PageContainer>
</template>

<style scoped>
.weeks {
  display: flex;
  align-items: center;
  gap: 6px;
}
.weeks__label {
  min-width: 80px;
  text-align: center;
  font-variant-numeric: tabular-nums;
  font-weight: 500;
}
.digest {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  transition: opacity 0.15s;
}
.digest--stale {
  opacity: 0.6;
}
.digest__grid {
  display: grid;
  grid-template-columns: minmax(0, 6fr) minmax(0, 5fr);
  gap: var(--space-4);
  align-items: start;
}
.digest__col {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}
.probation {
  padding: var(--space-3) var(--space-4);
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  background: var(--surface);
}
@media (max-width: 1100px) {
  .digest__grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
