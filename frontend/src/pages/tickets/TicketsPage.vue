<script setup lang="ts">
import { computed, onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { storeToRefs } from 'pinia'
import { useI18n } from 'vue-i18n'
import { CircleCheck, Clock, Columns3, Flame, List, SearchX } from 'lucide-vue-next'
import PageContainer from '@/components/common/PageContainer.vue'
import PageHead from '@/components/common/PageHead.vue'
import SectionCard from '@/components/common/SectionCard.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import ErrorState from '@/components/common/ErrorState.vue'
import LoadingState from '@/components/common/LoadingState.vue'
import TicketCard from '@/components/tickets/TicketCard.vue'
import { Button } from '@/components/ui/button'
import { useTicketsStore } from '@/stores/tickets'
import { useAuthStore } from '@/stores/auth'
import { SITE_FILTERS, useSiteStore, type SiteFilter } from '@/stores/site'
import { useNow } from '@/composables/useNow'
import { errorMessage } from '@/lib/apiError'
import {
  KANBAN_COLUMNS,
  SECURITY_SLA_WORKING_DAYS,
  activeCount,
  applyView,
  isSecurityOverdue,
  kanbanColumns,
  sortTickets,
  viewCounts,
  viewsFor,
  type TicketSort,
  type TicketView,
} from '@/lib/tickets'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const store = useTicketsStore()
const { items, loaded, loading, error } = storeToRefs(store)
const site = useSiteStore()
const { current: currentSite } = storeToRefs(site)
const now = useNow()
const auth = useAuthStore()
// "Security" is the HR lead's view only (#21); the others are everyone's.
const views = computed(() => viewsFor(auth.viewer))

// View and mode live in the route query (?view=overdue&mode=kanban): a pasted
// link opens the same queue. The site is the global filter of the topbar.
type Mode = 'list' | 'kanban'
const view = computed<TicketView>(() => {
  const v = route.query.view
  return typeof v === 'string' && (views.value as readonly string[]).includes(v)
    ? (v as TicketView)
    : 'all'
})
const mode = computed<Mode>(() => (route.query.mode === 'kanban' ? 'kanban' : 'list'))
// Newest first by default; ?sort=priority puts the most urgent on top.
const sort = computed<TicketSort>(() => (route.query.sort === 'priority' ? 'priority' : 'time'))

function setQuery(patch: Record<string, string | undefined>): void {
  void router.replace({ query: { ...route.query, ...patch } })
}
function setView(v: TicketView): void {
  setQuery({ view: v === 'all' ? undefined : v })
}
function setMode(m: Mode): void {
  setQuery({ mode: m === 'list' ? undefined : m })
}
function setSort(s: TicketSort): void {
  setQuery({ sort: s === 'time' ? undefined : s })
}
function setSite(s: SiteFilter): void {
  site.setSite(s)
}

function reload(): void {
  void store.load(site.apiSite())
}
watch(currentSite, reload)
onMounted(reload)

const counts = computed(() => viewCounts(items.value, now.value))
// Security tickets past their one-working-day SLA (counted on the client).
const securityOverdue = computed(
  () => items.value.filter((x) => isSecurityOverdue(x, now.value)).length,
)
const rows = computed(() => sortTickets(applyView(items.value, view.value, now.value), sort.value))
const columns = computed(() => kanbanColumns(rows.value, sort.value))
// Nothing waits for a person: the empty queue reads as success (ux-spec 3.4).
const allClosed = computed(
  () => loaded.value && activeCount(items.value) === 0 && view.value !== 'closed',
)
</script>

<template>
  <PageContainer width="wide">
    <PageHead :title="t('tickets.title')">
      <template #subtitle>{{ t('tickets.subtitle') }}</template>
      <template #actions>
        <div class="tabs" role="group" :aria-label="t('tickets.sort.label')">
          <button
            type="button"
            :class="['tab', { active: sort === 'time' }]"
            :aria-pressed="sort === 'time'"
            @click="setSort('time')"
          >
            <Clock :size="14" class="tab__ico" aria-hidden="true" />{{ t('tickets.sort.time') }}
          </button>
          <button
            type="button"
            :class="['tab', { active: sort === 'priority' }]"
            :aria-pressed="sort === 'priority'"
            @click="setSort('priority')"
          >
            <Flame :size="14" class="tab__ico" aria-hidden="true" />{{ t('tickets.sort.priority') }}
          </button>
        </div>
        <div class="tabs" role="group" :aria-label="t('tickets.mode.label')">
          <button
            type="button"
            :class="['tab', { active: mode === 'list' }]"
            :aria-pressed="mode === 'list'"
            @click="setMode('list')"
          >
            <List :size="14" class="tab__ico" aria-hidden="true" />{{ t('tickets.mode.list') }}
          </button>
          <button
            type="button"
            :class="['tab', { active: mode === 'kanban' }]"
            :aria-pressed="mode === 'kanban'"
            @click="setMode('kanban')"
          >
            <Columns3 :size="14" class="tab__ico" aria-hidden="true" />{{
              t('tickets.mode.kanban')
            }}
          </button>
        </div>
      </template>
    </PageHead>

    <div class="queue">
      <aside class="views">
        <nav :aria-label="t('tickets.views.label')">
          <div class="views__head">{{ t('tickets.views.label') }}</div>
          <button
            v-for="v in views"
            :key="v"
            type="button"
            :class="['views__item', { 'views__item--on': view === v }]"
            :aria-current="view === v ? 'true' : undefined"
            @click="setView(v)"
          >
            <span>{{ t(`tickets.views.${v}`) }}</span>
            <span
              :class="[
                'views__count tabular',
                {
                  'views__count--alert':
                    (v === 'overdue' && counts.overdue > 0) ||
                    (v === 'security' && securityOverdue > 0),
                },
              ]"
            >
              {{ counts[v] }}
            </span>
          </button>
        </nav>
        <div class="views__head views__head--gap">{{ t('tickets.sites.label') }}</div>
        <button
          v-for="s in SITE_FILTERS"
          :key="s"
          type="button"
          :class="['views__item', { 'views__item--on': currentSite === s }]"
          :aria-pressed="currentSite === s"
          @click="setSite(s)"
        >
          <span>{{ s === 'all' ? t('common.site.allSites') : t(`common.site.${s}`) }}</span>
        </button>
      </aside>

      <div class="queue__main">
        <p v-if="view === 'security'" class="security-note txt-sm" role="note">
          {{ t('tickets.security.note', { days: SECURITY_SLA_WORKING_DAYS }) }}
          <span v-if="securityOverdue > 0" class="security-note__warn">
            · {{ t('tickets.security.overdue', { n: securityOverdue }, securityOverdue) }}
          </span>
        </p>
        <SectionCard v-if="(loading && !loaded) || (error && !loaded)">
          <LoadingState v-if="loading" variant="table" :rows="5" />
          <ErrorState v-else :message="errorMessage(error, 'tickets.loadFailed')">
            <template #retry>
              <Button size="sm" @click="reload">{{ t('common.actions.retry') }}</Button>
            </template>
          </ErrorState>
        </SectionCard>

        <SectionCard v-else-if="allClosed">
          <EmptyState
            :icon="CircleCheck"
            :title="t('tickets.empty.title')"
            :subtitle="t('tickets.empty.description')"
          >
            <template v-if="counts.closed > 0" #action>
              <Button @click="setView('closed')">{{ t('tickets.views.closed') }}</Button>
            </template>
          </EmptyState>
        </SectionCard>

        <template v-else-if="mode === 'list'">
          <SectionCard v-if="rows.length === 0">
            <EmptyState
              :icon="SearchX"
              :title="t('tickets.noMatches.title')"
              :subtitle="t('tickets.noMatches.description')"
            >
              <template #action>
                <Button @click="setView('all')">{{ t('tickets.views.all') }}</Button>
              </template>
            </EmptyState>
          </SectionCard>
          <div v-else :class="['list', { 'is-stale': loading }]">
            <div class="list__meta muted txt-sm">{{ t('tickets.count', rows.length) }}</div>
            <TicketCard v-for="tk in rows" :key="tk.id" :ticket="tk" :now="now" />
          </div>
        </template>

        <div v-else :class="['kanban', { 'is-stale': loading }]">
          <section v-for="col in KANBAN_COLUMNS" :key="col" class="kanban__col">
            <header class="kanban__head">
              <span :class="['kanban__dot', `kanban__dot--${col}`]" aria-hidden="true" />
              <span class="fw-semi">{{ t(`tickets.status.${col}`) }}</span>
              <span class="kanban__count tabular">{{ columns[col].length }}</span>
            </header>
            <div class="kanban__body">
              <TicketCard v-for="tk in columns[col]" :key="tk.id" :ticket="tk" :now="now" tile />
              <div v-if="columns[col].length === 0" class="kanban__empty">
                {{ t('tickets.columns.empty') }}
              </div>
            </div>
          </section>
        </div>
      </div>
    </div>
  </PageContainer>
</template>

<style scoped>
.security-note {
  margin: 0 0 var(--space-3);
  padding: 8px 12px;
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-md);
  background: var(--bg-subtle);
  color: var(--fg-muted);
}
.security-note__warn {
  color: var(--danger);
  font-weight: 500;
}
.tab {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  border: 0;
}
/* Buttons reset only when inactive: the active tab keeps the shared white pill. */
.tab:not(.active) {
  background: transparent;
}
.queue {
  display: grid;
  grid-template-columns: 200px minmax(0, 1fr);
  gap: 16px;
  align-items: start;
}
.views {
  position: sticky;
  top: 16px;
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.views nav {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.views__head {
  padding: 0 10px 6px;
  font-size: var(--text-xs);
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: var(--tracking-wide);
  color: var(--fg-subtle);
}
.views__head--gap {
  margin-top: 16px;
}
.views__item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  height: 32px;
  padding: 0 10px;
  border: 0;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--fg-muted);
  font-size: var(--text-base);
  text-align: left;
  cursor: pointer;
}
.views__item:hover {
  background: var(--surface-hover);
  color: var(--fg);
}
.views__item--on {
  background: var(--bg-muted);
  color: var(--fg);
  font-weight: 500;
}
.views__count {
  font-size: var(--text-sm);
  color: var(--fg-subtle);
}
.views__count--alert {
  color: var(--danger);
  font-weight: 600;
}
.list {
  display: flex;
  flex-direction: column;
  gap: 8px;
  transition: opacity 0.15s;
}
.list__meta {
  padding: 0 2px;
}
.is-stale {
  opacity: 0.6;
}
.kanban {
  display: grid;
  grid-template-columns: repeat(4, minmax(220px, 1fr));
  gap: 12px;
  overflow-x: auto;
  transition: opacity 0.15s;
}
.kanban__col {
  display: flex;
  flex-direction: column;
  gap: 8px;
  min-width: 0;
  padding: 10px;
  border-radius: var(--radius-lg);
  background: var(--bg-subtle);
  border: 1px solid var(--border-subtle);
}
.kanban__head {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 0 2px 2px;
}
.kanban__dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
}
.kanban__dot--open {
  background: var(--warning);
}
.kanban__dot--assigned {
  background: var(--info);
}
.kanban__dot--human_active {
  background: var(--fg);
}
.kanban__dot--resolved {
  background: var(--success);
}
.kanban__count {
  margin-left: auto;
  font-size: var(--text-sm);
  color: var(--fg-subtle);
}
.kanban__body {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.kanban__empty {
  padding: 16px 8px;
  text-align: center;
  font-size: var(--text-sm);
  color: var(--fg-faint);
  border: 1px dashed var(--border);
  border-radius: var(--radius-md);
}
@media (max-width: 900px) {
  .queue {
    grid-template-columns: 1fr;
  }
  .views {
    position: static;
  }
}
</style>
