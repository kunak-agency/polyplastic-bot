<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { BellRing, BellOff, ShieldCheck, Users } from 'lucide-vue-next'
import PageContainer from '@/components/common/PageContainer.vue'
import PageHead from '@/components/common/PageHead.vue'
import SectionCard from '@/components/common/SectionCard.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import ErrorState from '@/components/common/ErrorState.vue'
import LoadingState from '@/components/common/LoadingState.vue'
import { Button } from '@/components/ui/button'
import { apiGetManagers, apiGetManagerView, apiManagerTask } from '@/api/manager'
import { useToast } from '@/composables/useToast'
import { errorMessage } from '@/lib/apiError'
import { dayLabel } from '@/lib/overview'
import { pulseLevel } from '@/lib/digest'
import { isoToLocalDate } from '@/lib/calendar'
import { ipTone } from '@/lib/ipDecision'
import type { IpDecision, ManagerSummary, ManagerView } from '@/types/api'
import { useAuthStore } from '@/stores/auth'
import { canOpen } from '@/lib/roles'

// "My newcomers" of ux-spec 3.1 for the head of unit (case role 03): D+N,
// stage, last pulse, open "1:1 meeting" tasks — no tickets, no answer texts,
// no signal categories. The HR lead opens it as /manager?name=<manager_name>
// from Team → Heads of units or the newcomer card; not in his menu.
const { t, d } = useI18n()
const route = useRoute()
// Signed in as a head of unit: only his own people, no picker. The server
// takes the name from the session (api_contract_auth.md); it is sent anyway so
// a server without the auth wave answers the same.
const auth = useAuthStore()
const self = computed(() =>
  auth.viewer === 'manager' ? auth.user?.manager_name || auth.user?.full_name || '' : '',
)
const name = computed(
  () => self.value || (typeof route.query.name === 'string' ? route.query.name : ''),
)

// The HR lead picks a head of unit on Team → Heads of units: the bare /manager
// goes there, and "All heads of units" walks back to it. With auth off there
// is no Team page, so the picker below stays.
const router = useRouter()
const pickerOnTeam = computed(() => !self.value && canOpen(auth.viewer, 'team'))
const backTo = computed(() =>
  pickerOnTeam.value ? { name: 'team', query: { tab: 'managers' } } : { name: 'manager' },
)

const managers = ref<ManagerSummary[] | null>(null)
const view = ref<ManagerView | null>(null)
const error = ref<unknown>(null)
const loading = ref(false)

async function load(): Promise<void> {
  if (!name.value && pickerOnTeam.value) {
    void router.replace(backTo.value)
    return
  }
  loading.value = true
  error.value = null
  try {
    if (name.value) {
      view.value = await apiGetManagerView(name.value)
    } else {
      view.value = null
      managers.value = await apiGetManagers()
    }
  } catch (e) {
    error.value = e
  } finally {
    loading.value = false
  }
}
onMounted(load)
watch(name, load)

// Pulse as a level only (research role-dashboards §3): no number.
const PULSE_FACE = { good: '🙂', mid: '😐', low: '☹', none: '·' } as const

// "Met" / "Planned" on a 1:1 task — the same as the buttons in MAX.
const toast = useToast()
const taskBusy = ref<string | null>(null)
async function setTask(taskId: string, status: 'done' | 'planned'): Promise<void> {
  taskBusy.value = taskId
  try {
    await apiManagerTask(taskId, status)
    toast.success(t(status === 'done' ? 'manager.card.metDone' : 'manager.card.plannedDone'))
    await load()
  } catch (e) {
    toast.error(errorMessage(e, 'manager.card.taskFailed'))
  } finally {
    taskBusy.value = null
  }
}

function date(iso: string): string {
  return d(isoToLocalDate(iso), 'short')
}
function ipBadge(v: IpDecision | null | undefined): string {
  const tone = ipTone(v)
  return tone ? `solid-${tone}` : ''
}
</script>

<template>
  <PageContainer width="wide">
    <PageHead :title="name ? name : t('manager.title')">
      <template #subtitle>{{ t('manager.subtitle') }}</template>
      <template v-if="name && !self" #actions>
        <Button as-child variant="outline" size="sm">
          <RouterLink :to="backTo">{{ t('manager.back') }}</RouterLink>
        </Button>
      </template>
    </PageHead>

    <SectionCard v-if="loading && !view && !managers">
      <LoadingState variant="card" :rows="3" />
    </SectionCard>
    <SectionCard v-else-if="error">
      <ErrorState :message="errorMessage(error, 'manager.loadError')">
        <template #retry>
          <Button size="sm" @click="load">{{ t('common.actions.retry') }}</Button>
        </template>
      </ErrorState>
    </SectionCard>

    <template v-else-if="name && view">
      <p :class="['conn', view.connected ? 'conn--on' : 'conn--off']">
        <component :is="view.connected ? BellRing : BellOff" :size="14" />
        {{ view.connected ? t('manager.connected') : t('manager.notConnected') }}
      </p>
      <SectionCard v-if="view.items.length === 0">
        <EmptyState :icon="Users" :title="t('manager.empty')" />
      </SectionCard>
      <div v-else class="cards">
        <article
          v-for="c in view.items"
          :key="c.newcomer_id"
          :class="['mcard', { 'mcard--flag': c.attention.length + c.meetings.length > 0 }]"
        >
          <header class="mcard__head">
            <div>
              <div class="mcard__name">{{ c.full_name }}</div>
              <div class="txt-sm muted">{{ c.position }} · {{ t(`common.site.${c.site}`) }}</div>
            </div>
            <div class="mcard__day">{{ dayLabel(c.day) }}</div>
          </header>
          <div class="mcard__row txt-sm">
            <span class="badge">{{ t(`common.stage.${c.stage}`) }}</span>
            <span class="muted">{{ t('manager.card.started', { date: date(c.start_date) }) }}</span>
          </div>
          <div class="mcard__row txt-sm">
            <span class="muted">{{ t('manager.card.pulse') }}:</span>
            <span :class="['pulse', `pulse--${pulseLevel(c.last_pulse)}`]">
              <span aria-hidden="true">{{ PULSE_FACE[pulseLevel(c.last_pulse)] }}</span>
              {{ t(`manager.card.pulseLevel.${pulseLevel(c.last_pulse)}`) }}
            </span>
          </div>
          <!-- IP decision: status only, the manager decides with the buttons in MAX (D+87). -->
          <div class="mcard__row txt-sm" role="status">
            <span class="muted">{{
              t('manager.card.probation', { date: date(c.probation_date) })
            }}</span>
            <span
              :class="['badge', ipBadge(c.ip_decision)]"
              :title="c.ip_decision ? undefined : t('manager.card.ipHint')"
            >
              {{ t(`manager.card.ip.${c.ip_decision ?? 'none'}`) }}
            </span>
          </div>
          <div v-if="c.attention.length" class="mcard__block">
            <div class="mcard__label">{{ t('manager.card.attention') }}</div>
            <ul>
              <li v-for="(a, i) in c.attention" :key="i">{{ a }}</li>
            </ul>
          </div>
          <div class="mcard__block">
            <div class="mcard__label">{{ t('manager.card.meetings') }}</div>
            <ul v-if="c.meetings.length">
              <li v-for="m in c.meetings" :key="m.id">
                {{ m.text }}
                <span :class="['badge', m.status === 'planned' ? 'solid-info' : 'solid-warning']">
                  {{ t(`manager.card.meetingStatus.${m.status}`) }}
                </span>
                <span class="mcard__btns">
                  <Button
                    size="sm"
                    variant="primary"
                    :disabled="taskBusy === m.id"
                    @click="setTask(m.id, 'done')"
                  >
                    {{ t('manager.card.met') }}
                  </Button>
                  <Button
                    v-if="m.status !== 'planned'"
                    size="sm"
                    :disabled="taskBusy === m.id"
                    @click="setTask(m.id, 'planned')"
                  >
                    {{ t('manager.card.planned') }}
                  </Button>
                </span>
              </li>
            </ul>
            <p v-else class="txt-sm muted">{{ t('manager.card.noMeetings') }}</p>
          </div>
          <!-- Placeholders: the backend for these comes later. -->
          <div class="mcard__later">
            <Button size="sm" variant="outline" disabled :title="t('manager.card.soon')">
              {{ t('manager.card.attachPlan') }}
            </Button>
          </div>
        </article>
      </div>
      <p class="privacy txt-sm muted"><ShieldCheck :size="14" />{{ t('manager.privacy') }}</p>
    </template>

    <SectionCard
      v-else-if="managers"
      :title="t('manager.pick.title')"
      :desc="t('manager.pick.desc')"
      body-padding="none"
    >
      <ul class="managers">
        <li v-for="m in managers" :key="m.manager_name">
          <RouterLink :to="{ name: 'manager', query: { name: m.manager_name } }">{{
            m.manager_name
          }}</RouterLink>
          <span class="txt-sm muted">
            {{ t('manager.pick.newcomers', { n: m.newcomers }, m.newcomers) }} ·
            {{ m.connected ? t('manager.pick.connected') : t('manager.pick.notConnected') }}
          </span>
        </li>
      </ul>
    </SectionCard>
  </PageContainer>
</template>

<style scoped>
.conn {
  display: flex;
  align-items: center;
  gap: 6px;
  margin: 0 0 var(--space-3);
  font-size: var(--text-sm);
}
.conn--on {
  color: var(--success);
}
.conn--off {
  color: var(--fg-muted);
}
.cards {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
  gap: var(--space-3);
}
.mcard {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: var(--space-3) var(--space-4);
  border: 1px solid var(--border);
  border-left: 3px solid transparent;
  border-radius: var(--radius-lg);
  background: var(--surface);
}
.mcard--flag {
  border-left-color: var(--warning);
}
.mcard__head {
  display: flex;
  justify-content: space-between;
  gap: var(--space-3);
}
.mcard__name {
  font-weight: 600;
}
.mcard__day {
  font-weight: 600;
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}
.mcard__row {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  align-items: center;
}
.mcard__label {
  font-size: var(--text-xs);
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: var(--fg-subtle);
}
.mcard__block ul {
  margin: 4px 0 0;
  padding-left: 18px;
  font-size: var(--text-sm);
}
.mcard__block li + li {
  margin-top: 4px;
}
.mcard__btns {
  display: inline-flex;
  gap: 6px;
  margin-left: 6px;
  vertical-align: middle;
}
.mcard__later {
  display: flex;
  gap: 6px;
  padding-top: 8px;
  border-top: 1px dashed var(--border-subtle);
}
.pulse--good {
  color: var(--success);
}
.pulse--mid {
  color: var(--warning);
}
.pulse--low {
  color: var(--danger);
  font-weight: 500;
}
.privacy {
  display: flex;
  gap: 8px;
  align-items: flex-start;
  margin-top: var(--space-4);
  max-width: 90ch;
}
.managers {
  list-style: none;
  margin: 0;
  padding: 0;
}
.managers li {
  display: flex;
  justify-content: space-between;
  gap: var(--space-3);
  padding: var(--space-3) var(--space-4);
  border-top: 1px solid var(--border-subtle);
}
.managers li:first-child {
  border-top: none;
}
.managers a {
  color: var(--fg);
  font-weight: 500;
}
</style>
