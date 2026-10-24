<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { UserPlus, UsersRound } from 'lucide-vue-next'
import SectionCard from '@/components/common/SectionCard.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import ErrorState from '@/components/common/ErrorState.vue'
import LoadingState from '@/components/common/LoadingState.vue'
import AddUserDialog from '@/components/team/AddUserDialog.vue'
import PeopleList from '@/components/team/PeopleList.vue'
import { Button } from '@/components/ui/button'
import { apiGetTeam, apiPatchUser } from '@/api/team'
import { useToast } from '@/composables/useToast'
import { useUsersStore } from '@/stores/users'
import { errorMessage } from '@/lib/apiError'
import { TEAM_COLUMNS, type TeamColumn } from '@/lib/dashboard'
import type { TeamMember, TeamResponse } from '@/types/api'

// Users of the cabinet in Settings (hr_lead; research role-dashboards §2):
// recruiters with their load, add / deactivate. Mass reassignment is in the
// registry (tick → "Reassign"). Columns come from TEAM_COLUMNS.
const { t } = useI18n()
const toast = useToast()
const users = useUsersStore()

const data = ref<TeamResponse | null>(null)
const error = ref<unknown>(null)
const loading = ref(false)
const addOpen = ref(false)

// Tabs by role: recruiters with their load, managers and mentors of the people
// directory (api_contract_auth.md, "People").
type Tab = 'recruiters' | 'managers' | 'mentors'
const TABS: Tab[] = ['recruiters', 'managers', 'mentors']
const tab = ref<Tab>('recruiters')
const busyId = ref<string | null>(null)

async function load(): Promise<void> {
  loading.value = true
  error.value = null
  try {
    data.value = await apiGetTeam()
  } catch (e) {
    error.value = e
  } finally {
    loading.value = false
  }
}
onMounted(load)

function cell(m: TeamMember, c: TeamColumn): string {
  return String(m[c.key] ?? '—')
}

function warn(m: TeamMember, c: TeamColumn): boolean {
  return !!c.warn && Number(m[c.key] ?? 0) > 0
}

async function toggleActive(m: TeamMember): Promise<void> {
  if (m.active && !window.confirm(t('team.deactivate.confirm', { name: m.full_name }))) return
  busyId.value = m.user_id
  try {
    await apiPatchUser(m.user_id, { active: !m.active })
    toast.success(m.active ? t('team.deactivate.done') : t('team.activate.done'))
    void users.loadRecruiters(true).catch(() => undefined)
    await load()
  } catch (e) {
    toast.error(errorMessage(e, 'team.deactivate.failed'))
  } finally {
    busyId.value = null
  }
}

function onCreated(): void {
  void users.loadRecruiters(true).catch(() => undefined)
  void load()
}
</script>

<template>
  <div class="users-panel">
    <div class="users-panel__head">
      <div>
        <h2 class="users-panel__title">{{ t('team.title') }}</h2>
        <p class="muted txt-sm">{{ t('team.subtitle') }}</p>
      </div>
      <Button v-if="tab === 'recruiters'" variant="primary" @click="addOpen = true">
        <UserPlus :size="14" aria-hidden="true" />{{ t('team.add.button') }}
      </Button>
    </div>

    <div class="users-panel__tabs" role="tablist">
      <button
        v-for="x in TABS"
        :key="x"
        type="button"
        role="tab"
        :aria-selected="tab === x"
        :class="['users-panel__tab', { 'is-active': tab === x }]"
        @click="tab = x"
      >
        {{ t(`people.users.tabs.${x}`) }}
      </button>
    </div>

    <PeopleList v-if="tab !== 'recruiters'" :role="tab === 'managers' ? 'manager' : 'mentor'" />

    <SectionCard v-else-if="!data && !error">
      <LoadingState variant="table" :rows="4" />
    </SectionCard>
    <SectionCard v-else-if="!data">
      <ErrorState :message="errorMessage(error, 'team.loadError')">
        <template #retry>
          <Button size="sm" @click="load">{{ t('common.actions.retry') }}</Button>
        </template>
      </ErrorState>
    </SectionCard>

    <template v-else>
      <div class="team-summary">
        <RouterLink
          :class="['tile', { 'tile--warn': data.unassigned > 0 }]"
          :to="{ name: 'newcomers', query: { recruiter: 'none' } }"
        >
          <span class="tile__label">{{ t('team.unassigned') }}</span>
          <span class="tile__value">{{ data.unassigned }}</span>
          <span class="tile__hint">{{ t('team.unassignedHint') }}</span>
        </RouterLink>
        <p class="muted txt-sm team-summary__hint">
          {{ t('team.reassignHint') }}
          <RouterLink :to="{ name: 'newcomers' }">{{ t('team.toRegistry') }}</RouterLink>
        </p>
      </div>

      <SectionCard
        :title="t('team.recruiters')"
        body-padding="none"
        :class="{ 'is-stale': loading }"
      >
        <EmptyState v-if="data.items.length === 0" :icon="UsersRound" :title="t('team.empty')" />
        <div v-else class="tbl-scroll">
          <table class="tbl">
            <thead>
              <tr>
                <th>{{ t('team.columns.name') }}</th>
                <th v-for="c in TEAM_COLUMNS" :key="c.key" class="td-num">{{ t(c.labelKey) }}</th>
                <th>{{ t('team.columns.status') }}</th>
                <th />
              </tr>
            </thead>
            <tbody>
              <tr v-for="m in data.items" :key="m.user_id" :class="{ 'row-off': !m.active }">
                <td>
                  <RouterLink
                    class="cell-link"
                    :to="{ name: 'newcomers', query: { recruiter: m.user_id } }"
                  >
                    {{ m.full_name }}
                  </RouterLink>
                  <div class="cell-sub">{{ m.login }}</div>
                </td>
                <td
                  v-for="c in TEAM_COLUMNS"
                  :key="c.key"
                  :class="['td-num tabular', { 'cell-warn': warn(m, c) }]"
                >
                  {{ cell(m, c) }}
                </td>
                <td>
                  <span :class="['badge', m.active ? 'solid-success' : '']">
                    {{ m.active ? t('team.status.active') : t('team.status.inactive') }}
                  </span>
                </td>
                <td class="td-actions">
                  <Button
                    size="sm"
                    :variant="m.active ? 'ghost' : 'outline'"
                    :disabled="busyId === m.user_id"
                    @click="toggleActive(m)"
                  >
                    {{ m.active ? t('team.deactivate.button') : t('team.activate.button') }}
                  </Button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </SectionCard>
    </template>

    <AddUserDialog v-model:open="addOpen" @created="onCreated" />
  </div>
</template>

<style scoped>
.users-panel__head {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: var(--space-3);
  margin-bottom: var(--space-3);
}
.users-panel__title {
  margin: 0;
  font-size: var(--text-lg, 16px);
  font-weight: 600;
}
.users-panel__head p {
  margin: 2px 0 0;
}
.users-panel__tabs {
  display: flex;
  gap: 4px;
  margin-bottom: var(--space-3);
  border-bottom: 1px solid var(--border-subtle);
}
.users-panel__tab {
  padding: 6px 12px;
  border: 0;
  border-bottom: 2px solid transparent;
  background: none;
  color: var(--fg-muted);
  font: inherit;
  cursor: pointer;
}
.users-panel__tab.is-active {
  color: var(--fg);
  border-bottom-color: var(--accent, var(--fg));
  font-weight: 500;
}
.team-summary {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  margin-bottom: var(--space-4);
}
.team-summary__hint {
  max-width: 60ch;
}
.tile {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 200px;
  padding: var(--space-3) var(--space-4);
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  background: var(--surface);
  color: var(--fg);
  text-decoration: none;
}
.tile:hover {
  border-color: var(--border-strong);
}
.tile__label {
  font-size: var(--text-sm);
  color: var(--fg-muted);
}
.tile__value {
  font-size: var(--text-3xl);
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}
.tile--warn .tile__value {
  color: var(--priority-p1);
}
.tile__hint {
  font-size: var(--text-xs);
  color: var(--fg-subtle);
}
.tbl-scroll {
  overflow-x: auto;
}
.cell-link {
  color: var(--fg);
  font-weight: 500;
}
.cell-sub {
  font-size: var(--text-sm);
  color: var(--fg-subtle);
}
.cell-warn {
  color: var(--danger);
  font-weight: 600;
}
.row-off {
  opacity: 0.55;
}
.td-actions {
  text-align: right;
}
.is-stale {
  opacity: 0.6;
}
</style>
