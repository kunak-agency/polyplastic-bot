<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter, RouterLink } from 'vue-router'
import { storeToRefs } from 'pinia'
import { useI18n } from 'vue-i18n'
import { useDebounceFn } from '@vueuse/core'
import { Search, Users, SearchX } from 'lucide-vue-next'
import PageContainer from '@/components/common/PageContainer.vue'
import PageHead from '@/components/common/PageHead.vue'
import SectionCard from '@/components/common/SectionCard.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import ErrorState from '@/components/common/ErrorState.vue'
import LoadingState from '@/components/common/LoadingState.vue'
import RiskMark from '@/components/newcomers/RiskMark.vue'
import ConnectionBadge from '@/components/newcomers/ConnectionBadge.vue'
import LaunchDialog from '@/components/newcomers/LaunchDialog.vue'
import { Button } from '@/components/ui/button'
import { useNewcomersStore } from '@/stores/newcomers'
import { useSiteStore } from '@/stores/site'
import { errorMessage } from '@/lib/apiError'
import { formatDayOffset, isoToLocalDate } from '@/lib/calendar'
import {
  NEWCOMER_CHIPS,
  adaptationProgress,
  applyChips,
  chipCounts,
  sortNewcomers,
  type NewcomerChip,
  type NewcomerSort,
} from '@/lib/newcomerFilters'
import type { Newcomer } from '@/types/api'

const { t, d, n: fmtN } = useI18n()
const route = useRoute()
const router = useRouter()
const store = useNewcomersStore()
const { items, loaded, loading, error } = storeToRefs(store)
const site = useSiteStore()
const { current: currentSite } = storeToRefs(site)

// Chips live in the route query (?chips=red,silent): a drill-down from the
// overview tiles and a pasted link land on the same filtered list.
function chipsFromQuery(): NewcomerChip[] {
  const raw = typeof route.query.chips === 'string' ? route.query.chips.split(',') : []
  return NEWCOMER_CHIPS.filter((c) => raw.includes(c))
}
const chips = ref<NewcomerChip[]>(chipsFromQuery())
const sortBy = ref<NewcomerSort>('risk')
const q = ref('')
const launchOpen = ref(false)

function toggleChip(c: NewcomerChip): void {
  chips.value = chips.value.includes(c) ? chips.value.filter((x) => x !== c) : [...chips.value, c]
  void router.replace({ query: { ...route.query, chips: chips.value.join(',') || undefined } })
}

function resetFilters(): void {
  chips.value = []
  q.value = ''
  void router.replace({ query: { ...route.query, chips: undefined } })
  reload()
}

function reload(): void {
  void store.load({ site: site.apiSite(), q: q.value })
}
const reloadDebounced = useDebounceFn(reload, 300)

watch(currentSite, reload)
watch(q, () => void reloadDebounced())
onMounted(reload)

// One "now" per render pass, so all chips agree on what "this week" is.
const now = ref(new Date())
watch(items, () => (now.value = new Date()))

const counts = computed(() => chipCounts(items.value, now.value))
const rows = computed(() =>
  sortNewcomers(applyChips(items.value, chips.value, now.value), sortBy.value),
)
const filtered = computed(() => chips.value.length > 0 || q.value.trim() !== '')

function activity(n: Newcomer): string {
  if (!n.last_activity_at) return t('newcomers.never')
  const days = Math.floor((now.value.getTime() - Date.parse(n.last_activity_at)) / 86_400_000)
  return days <= 0 ? t('newcomers.activity.today') : t('newcomers.activity.ago', days)
}

function onLaunched(created: Newcomer): void {
  void router.push({ name: 'newcomer-invite', params: { id: created.id } })
}

// The whole row opens the trajectory. The name stays a real link: it is the
// keyboard stop and the target of middle-click, so the row itself adds no
// second tab stop. Clicks on controls inside the row and text selection are
// left alone; Ctrl/Cmd-click opens a new tab like the link would.
function openRow(e: MouseEvent, id: string): void {
  const target = e.target as HTMLElement | null
  if (target?.closest('a, button, input, select, textarea, [role="button"]')) return
  if (window.getSelection()?.toString()) return
  const to = { name: 'newcomer', params: { id } }
  if (e.metaKey || e.ctrlKey) {
    window.open(router.resolve(to).href, '_blank', 'noopener')
    return
  }
  void router.push(to)
}
</script>

<template>
  <PageContainer width="wide">
    <PageHead :title="t('newcomers.title')">
      <template #subtitle>{{ t('newcomers.subtitle') }}</template>
      <template #actions>
        <Button variant="primary" @click="launchOpen = true">{{
          t('newcomers.actions.launch')
        }}</Button>
      </template>
    </PageHead>

    <div class="toolbar">
      <label class="input-with-icon toolbar__search">
        <Search class="ico-leading" :size="14" aria-hidden="true" />
        <input
          v-model="q"
          class="input"
          type="search"
          :placeholder="t('newcomers.search.placeholder')"
          :aria-label="t('newcomers.search.label')"
        />
      </label>
      <div class="chips" role="group" :aria-label="t('newcomers.chips.label')">
        <button
          v-for="c in NEWCOMER_CHIPS"
          :key="c"
          type="button"
          :class="['chip', { 'chip--on': chips.includes(c) }]"
          :aria-pressed="chips.includes(c)"
          @click="toggleChip(c)"
        >
          {{ t(`newcomers.chips.${c}`) }}
          <span class="chip__count tabular">{{ counts[c] }}</span>
        </button>
      </div>
      <div class="toolbar__spacer" />
      <label class="toolbar__sort">
        <span class="muted txt-sm">{{ t('newcomers.sort.label') }}</span>
        <select v-model="sortBy" class="input input--select">
          <option value="risk">{{ t('newcomers.sort.risk') }}</option>
          <option value="day">{{ t('newcomers.sort.day') }}</option>
        </select>
      </label>
    </div>

    <SectionCard body-padding="none">
      <LoadingState v-if="loading && !loaded" variant="table" :rows="5" />
      <ErrorState
        v-else-if="error && !loaded"
        :message="errorMessage(error, 'newcomers.loadFailed')"
      >
        <template #retry>
          <Button size="sm" @click="reload">{{ t('common.actions.retry') }}</Button>
        </template>
      </ErrorState>
      <EmptyState
        v-else-if="rows.length === 0 && !filtered && currentSite === 'all'"
        :icon="Users"
        :title="t('newcomers.empty.title')"
        :subtitle="t('newcomers.empty.description')"
      >
        <template #action>
          <Button variant="primary" @click="launchOpen = true">{{
            t('newcomers.actions.launch')
          }}</Button>
        </template>
      </EmptyState>
      <EmptyState
        v-else-if="rows.length === 0"
        :icon="SearchX"
        :title="t('newcomers.noMatches.title')"
        :subtitle="t('newcomers.noMatches.description')"
      >
        <template #action>
          <Button @click="resetFilters">{{ t('newcomers.actions.resetFilters') }}</Button>
        </template>
      </EmptyState>
      <div v-else class="tbl-scroll" :class="{ 'is-stale': loading }">
        <table class="tbl">
          <thead>
            <tr>
              <th>{{ t('newcomers.columns.name') }}</th>
              <th>{{ t('newcomers.columns.site') }}</th>
              <th>{{ t('newcomers.columns.department') }}</th>
              <th>{{ t('newcomers.columns.startDate') }}</th>
              <th>{{ t('newcomers.columns.stage') }}</th>
              <th>{{ t('newcomers.columns.activity') }}</th>
              <th class="td-num">{{ t('newcomers.columns.pulse') }}</th>
              <th>{{ t('newcomers.columns.risk') }}</th>
              <th class="td-num">{{ t('newcomers.columns.tickets') }}</th>
              <th>{{ t('newcomers.columns.connection') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="n in rows" :key="n.id" class="row-link" @click="openRow($event, n.id)">
              <td>
                <RouterLink class="cell-link" :to="{ name: 'newcomer', params: { id: n.id } }">
                  {{ n.full_name }}
                </RouterLink>
                <div class="cell-sub">{{ n.position }}</div>
              </td>
              <td>{{ t(`common.site.${n.site}`) }}</td>
              <td>
                {{ n.department }}
                <div class="cell-sub">{{ n.manager_name }} · {{ n.mentor_name }}</div>
              </td>
              <td class="tabular">{{ d(isoToLocalDate(n.start_date), 'short') }}</td>
              <td class="cell-stage">
                <div class="row gap-2 between">
                  <span class="tabular fw-medium">{{ formatDayOffset(n.day) }}</span>
                  <span class="cell-sub">{{ t(`common.stage.${n.stage}`) }}</span>
                </div>
                <div
                  class="progress sz-2"
                  role="progressbar"
                  :aria-valuenow="Math.round(adaptationProgress(n.day) * 100)"
                  aria-valuemin="0"
                  aria-valuemax="100"
                  :aria-label="
                    t('newcomers.progressLabel', {
                      percent: fmtN(adaptationProgress(n.day), 'percent'),
                    })
                  "
                >
                  <i :style="{ width: `${adaptationProgress(n.day) * 100}%` }" />
                </div>
              </td>
              <td class="cell-sub-strong">{{ activity(n) }}</td>
              <td class="td-num tabular">
                {{ n.last_pulse === null ? '—' : t('newcomers.pulse', { value: n.last_pulse }) }}
              </td>
              <td><RiskMark :risk="n.risk" :reason="n.risk_reason" /></td>
              <td class="td-num tabular">{{ n.open_tickets }}</td>
              <td><ConnectionBadge :connection="n.connection" /></td>
            </tr>
          </tbody>
        </table>
      </div>
    </SectionCard>

    <LaunchDialog v-model:open="launchOpen" @launched="onLaunched" />
  </PageContainer>
</template>

<style scoped>
.toolbar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px 12px;
  margin-bottom: 12px;
}
.toolbar__search {
  width: 280px;
}
.toolbar__spacer {
  flex: 1;
}
.toolbar__sort {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}
.input--select {
  width: auto;
}
.chips {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
.chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 28px;
  padding: 0 10px;
  border: 1px solid var(--border);
  border-radius: 999px;
  background: var(--surface);
  color: var(--fg-muted);
  font-size: var(--text-sm);
  cursor: pointer;
}
.chip:hover {
  color: var(--fg);
  border-color: var(--border-strong);
}
.chip--on {
  background: var(--fg);
  border-color: var(--fg);
  color: var(--bg);
}
.chip--on:hover {
  color: var(--bg);
}
.chip__count {
  font-size: var(--text-xs);
  opacity: 0.7;
}
.tbl-scroll {
  overflow-x: auto;
  transition: opacity 0.15s;
}
.tbl-scroll.is-stale {
  opacity: 0.6;
}
.row-link {
  cursor: pointer;
}
.cell-link {
  color: var(--fg);
  font-weight: 500;
}
.cell-link:hover {
  text-decoration: underline;
}
.cell-sub {
  font-size: var(--text-sm);
  color: var(--fg-subtle);
}
.cell-sub-strong {
  font-size: var(--text-sm);
  color: var(--fg-muted);
  white-space: nowrap;
}
.cell-stage {
  min-width: 170px;
}
.cell-stage .progress {
  margin-top: 6px;
}
</style>
