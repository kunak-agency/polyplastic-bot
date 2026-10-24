<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { storeToRefs } from 'pinia'
import { RouterLink, useRoute, useRouter, type RouteLocationRaw } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { BellOff, BellRing, RefreshCw, UserCheck, UsersRound } from 'lucide-vue-next'
import PageContainer from '@/components/common/PageContainer.vue'
import PageHead from '@/components/common/PageHead.vue'
import SectionCard from '@/components/common/SectionCard.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import ErrorState from '@/components/common/ErrorState.vue'
import LoadingState from '@/components/common/LoadingState.vue'
import { Button } from '@/components/ui/button'
import { apiGetHrLeadDashboard } from '@/api/team'
import { apiGetManagers } from '@/api/manager'
import { useSiteStore } from '@/stores/site'
import { errorMessage } from '@/lib/apiError'
import { sortLoad } from '@/lib/dashboard'
import type { HrLeadDashboard, ManagerSummary } from '@/types/api'

// HR lead's "Team" (lib/roles.ts: hr_lead only). Two tabs, kept in ?tab=:
// recruiters — the load table of the dashboard in full (plus reaction), a row
// opens that recruiter's newcomers; managers — heads of units, a row opens the
// head's own view (/manager?name=). Users are added and switched off in Settings.
const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const site = useSiteStore()
const { current: currentSite } = storeToRefs(site)

type Tab = 'recruiters' | 'managers'
const tab = computed<Tab>(() => (route.query.tab === 'managers' ? 'managers' : 'recruiters'))
function setTab(v: Tab): void {
  void router.replace({ query: { ...route.query, tab: v === 'recruiters' ? undefined : v } })
}

const dash = ref<HrLeadDashboard | null>(null)
const managers = ref<ManagerSummary[] | null>(null)
const loading = ref(false)
const error = ref<unknown>(null)

async function load(): Promise<void> {
  loading.value = true
  error.value = null
  try {
    if (tab.value === 'managers') managers.value = await apiGetManagers()
    else dash.value = await apiGetHrLeadDashboard(site.apiSite())
  } catch (e) {
    error.value = e
  } finally {
    loading.value = false
  }
}
onMounted(load)
watch([tab, currentSite], load)

// Order by overdue SLA, as on "Today": never by reaction (no ranking).
const rows = computed(() => (dash.value ? sortLoad(dash.value.load) : []))
const unassigned = computed(() => dash.value?.unassigned ?? 0)

function minutes(m: number | null | undefined): string {
  if (m === null || m === undefined) return t('teamPage.recruiters.few')
  return m < 60
    ? t('team.minutes', { n: m })
    : t('team.hours', { h: Math.floor(m / 60), m: m % 60 })
}
const teamMedian = computed(() => minutes(dash.value?.median_first_response_min))

function recruiterLink(id: string): RouteLocationRaw {
  return { name: 'newcomers', query: { recruiter: id } }
}
function managerLink(name: string): RouteLocationRaw {
  return { name: 'manager', query: { name } }
}
function go(to: RouteLocationRaw): void {
  void router.push(to)
}
</script>

<template>
  <PageContainer width="wide">
    <PageHead :title="t('teamPage.title')">
      <template #subtitle>{{ t('teamPage.subtitle') }}</template>
      <template #actions>
        <Button variant="outline" size="sm" :disabled="loading" @click="load">
          <RefreshCw :size="14" aria-hidden="true" />{{ t('dashboard.refresh') }}
        </Button>
      </template>
    </PageHead>

    <div class="tabs team-tabs" role="tablist" :aria-label="t('teamPage.tabsLabel')">
      <button
        type="button"
        role="tab"
        :class="['tab', { active: tab === 'recruiters' }]"
        :aria-selected="tab === 'recruiters'"
        @click="setTab('recruiters')"
      >
        <UsersRound :size="14" aria-hidden="true" />{{ t('teamPage.tabs.recruiters') }}
      </button>
      <button
        type="button"
        role="tab"
        :class="['tab', { active: tab === 'managers' }]"
        :aria-selected="tab === 'managers'"
        @click="setTab('managers')"
      >
        <UserCheck :size="14" aria-hidden="true" />{{ t('teamPage.tabs.managers') }}
      </button>
    </div>

    <SectionCard v-if="error">
      <ErrorState
        :message="
          errorMessage(
            error,
            tab === 'managers' ? 'teamPage.managersLoadError' : 'teamPage.loadError',
          )
        "
      >
        <template #retry>
          <Button size="sm" @click="load">{{ t('common.actions.retry') }}</Button>
        </template>
      </ErrorState>
    </SectionCard>

    <!-- Recruiters -->
    <template v-else-if="tab === 'recruiters'">
      <SectionCard v-if="!dash">
        <LoadingState variant="card" :rows="3" />
      </SectionCard>
      <SectionCard
        v-else
        :title="t('teamPage.recruiters.title')"
        :desc="t('teamPage.recruiters.desc')"
        body-padding="none"
      >
        <template #headActions>
          <RouterLink
            class="btn btn-outline btn-sm"
            :to="{ name: 'newcomers', query: { recruiter: 'none' } }"
            :title="t('teamPage.recruiters.reassignHint')"
          >
            {{ t('teamPage.recruiters.reassign') }}
          </RouterLink>
        </template>
        <EmptyState
          v-if="rows.length === 0 && unassigned === 0"
          :icon="UsersRound"
          :title="t('teamPage.recruiters.empty')"
        />
        <div v-else class="tbl-wrap">
          <table class="tbl">
            <thead>
              <tr>
                <th>{{ t('teamPage.recruiters.columns.name') }}</th>
                <th class="td-num">{{ t('teamPage.recruiters.columns.newcomers') }}</th>
                <th class="td-num">{{ t('teamPage.recruiters.columns.open') }}</th>
                <th class="td-num">{{ t('teamPage.recruiters.columns.overdue') }}</th>
                <th class="td-num">{{ t('teamPage.recruiters.columns.median') }}</th>
                <th class="td-num">{{ t('teamPage.recruiters.columns.unanswered') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="r in rows"
                :key="r.user_id"
                class="row-link"
                @click="go(recruiterLink(r.user_id))"
              >
                <td>
                  <RouterLink class="cell-link" :to="recruiterLink(r.user_id)" @click.stop>
                    {{ r.full_name }}
                  </RouterLink>
                </td>
                <td class="td-num tabular">{{ r.newcomers }}</td>
                <td class="td-num tabular">{{ r.tickets_open }}</td>
                <td :class="['td-num tabular', { 'cell-warn': r.tickets_sla_overdue > 0 }]">
                  {{ r.tickets_sla_overdue }}
                </td>
                <td :class="['td-num tabular', { muted: r.median_first_response_min == null }]">
                  {{ minutes(r.median_first_response_min) }}
                </td>
                <td :class="['td-num tabular', { 'cell-warn': (r.unanswered_24h ?? 0) > 0 }]">
                  {{ r.unanswered_24h ?? 0 }}
                </td>
              </tr>
              <tr
                v-if="unassigned > 0"
                class="row-link row-unassigned"
                @click="go(recruiterLink('none'))"
              >
                <td>
                  <RouterLink class="cell-link" :to="recruiterLink('none')" @click.stop>
                    {{ t('teamPage.recruiters.unassigned') }}
                  </RouterLink>
                </td>
                <td class="td-num tabular cell-warn">{{ unassigned }}</td>
                <td class="td-num muted">—</td>
                <td class="td-num muted">—</td>
                <td class="td-num muted">—</td>
                <td
                  :class="[
                    'td-num tabular',
                    { 'cell-warn': (dash.unassigned_unanswered_24h ?? 0) > 0 },
                  ]"
                >
                  {{ dash.unassigned_unanswered_24h ?? 0 }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <p class="foot muted txt-sm">
          <span>{{ t('teamPage.recruiters.teamMedian', { value: teamMedian }) }}</span>
          <span>{{ t('teamPage.recruiters.medianFoot') }}</span>
        </p>
      </SectionCard>
    </template>

    <!-- Heads of units -->
    <template v-else>
      <SectionCard v-if="!managers">
        <LoadingState variant="card" :rows="3" />
      </SectionCard>
      <SectionCard
        v-else
        :title="t('teamPage.managers.title')"
        :desc="t('teamPage.managers.desc')"
        body-padding="none"
      >
        <EmptyState
          v-if="managers.length === 0"
          :icon="UserCheck"
          :title="t('teamPage.managers.empty')"
        />
        <div v-else class="tbl-wrap">
          <table class="tbl">
            <thead>
              <tr>
                <th>{{ t('teamPage.managers.columns.name') }}</th>
                <th class="td-num">{{ t('teamPage.managers.columns.newcomers') }}</th>
                <th>{{ t('teamPage.managers.columns.bot') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="m in managers"
                :key="m.manager_name"
                class="row-link"
                @click="go(managerLink(m.manager_name))"
              >
                <td>
                  <RouterLink class="cell-link" :to="managerLink(m.manager_name)" @click.stop>
                    {{ m.manager_name }}
                  </RouterLink>
                </td>
                <td class="td-num tabular">{{ m.newcomers }}</td>
                <td>
                  <span :class="['conn', m.connected ? 'conn--on' : 'conn--off']">
                    <component
                      :is="m.connected ? BellRing : BellOff"
                      :size="14"
                      aria-hidden="true"
                    />
                    {{
                      m.connected
                        ? t('teamPage.managers.connected')
                        : t('teamPage.managers.notConnected')
                    }}
                  </span>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </SectionCard>
    </template>
  </PageContainer>
</template>

<style scoped>
.team-tabs {
  margin-bottom: var(--space-4);
}
.team-tabs .tab {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.tbl-wrap {
  overflow-x: auto;
}
.row-link {
  cursor: pointer;
}
.row-link:hover td {
  background: var(--bg-subtle);
}
.row-unassigned td {
  border-top: 1px solid var(--border);
}
.cell-link {
  color: var(--fg);
  font-weight: 500;
}
.cell-warn {
  color: var(--danger);
  font-weight: 600;
}
.foot {
  display: flex;
  flex-direction: column;
  gap: 2px;
  margin: 0;
  padding: 8px var(--space-4);
  border-top: 1px solid var(--border-subtle);
}
.conn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: var(--text-sm);
}
.conn--on {
  color: var(--success);
}
.conn--off {
  color: var(--fg-muted);
}
</style>
