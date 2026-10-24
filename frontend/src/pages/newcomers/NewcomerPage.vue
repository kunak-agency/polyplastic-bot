<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute, RouterLink } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { CalendarClock, History, Hourglass, Link2, PlugZap } from 'lucide-vue-next'
import PageContainer from '@/components/common/PageContainer.vue'
import SectionCard from '@/components/common/SectionCard.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import ErrorState from '@/components/common/ErrorState.vue'
import LoadingState from '@/components/common/LoadingState.vue'
import RiskMark from '@/components/newcomers/RiskMark.vue'
import ConnectionBadge from '@/components/newcomers/ConnectionBadge.vue'
import NewcomerTimeline from '@/components/newcomers/NewcomerTimeline.vue'
import NewcomerTickets from '@/components/tickets/NewcomerTickets.vue'
import ManagerInviteButton from '@/components/manager/ManagerInviteButton.vue'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import {
  apiGetNewcomer,
  apiGetTimeline,
  apiPatchNewcomer,
  apiSetNewcomerFlag,
  apiSimulateStart,
  apiTimeTravel,
} from '@/api/newcomers'
import { useBreadcrumbTail } from '@/composables/useBreadcrumbs'
import { useToast } from '@/composables/useToast'
import { errorMessage } from '@/lib/apiError'
import { formatDayOffset, isoToLocalDate } from '@/lib/calendar'
import type { Newcomer, TimelineEvent } from '@/types/api'

const { t, d } = useI18n()
const route = useRoute()
const toast = useToast()
const { setBreadcrumbTail } = useBreadcrumbTail()

const id = computed(() => String(route.params.id))
const newcomer = ref<Newcomer | null>(null)
const error = ref<unknown>(null)
const timeline = ref<TimelineEvent[] | null>(null)
const timelineError = ref<unknown>(null)

async function loadTimeline(): Promise<void> {
  timelineError.value = null
  try {
    timeline.value = (await apiGetTimeline(id.value)).items
  } catch (e) {
    timelineError.value = e
  }
}

async function load(): Promise<void> {
  error.value = null
  newcomer.value = null
  timeline.value = null
  try {
    newcomer.value = await apiGetNewcomer(id.value)
    setBreadcrumbTail(newcomer.value.full_name)
  } catch (e) {
    error.value = e
    return
  }
  await loadTimeline()
}
watch(id, load, { immediate: true })

// ── Time travel ─────────────────────────────────────────────────────────────
const ttOpen = ref(false)
// The server answers 409 for a newcomer who has not opened the invite: there is
// no dialog to send into. Said up front instead of after the click.
const connected = computed(() => newcomer.value?.connection === 'connected')
// Development builds only: plays "opened the invite" through the dev hook
// (POST /api/dev/simulate-start), so time travel can be shown without a phone.
const DEV = import.meta.env.DEV
const simulating = ref(false)
const ttDay = ref(0)
const ttBusy = ref(false)

// The days the scenario has steps on: travelling to a day without steps sends
// the same as the step day before it.
const stepDays = computed(() =>
  [...new Set((timeline.value ?? []).map((e) => e.day_offset))].sort((a, b) => a - b),
)

watch(ttOpen, (open) => {
  if (open) rsOpen.value = false
  if (!open || !newcomer.value) return
  // Suggest the first day that still has something to send.
  const pending = (timeline.value ?? []).find((e) => e.status === 'scheduled')
  ttDay.value = pending?.day_offset ?? stepDays.value[stepDays.value.length - 1] ?? 0
})

async function travel(): Promise<void> {
  const before = (timeline.value ?? []).filter((e) => e.status === 'scheduled').length
  ttBusy.value = true
  try {
    const r = await apiTimeTravel(id.value, ttDay.value)
    timeline.value = r.items
    const left = r.items.filter((e) => e.status === 'scheduled')
    const sent = before - left.length
    // Without consent surveys stay scheduled: say why they did not go.
    const heldSurveys =
      newcomer.value?.consent !== 'given' &&
      left.some((e) => e.kind === 'survey' && e.day_offset <= ttDay.value)
    if (sent > 0) toast.success(t('newcomers.timeTravel.done', sent))
    else if (!heldSurveys) toast.info(t('newcomers.timeTravel.nothing'))
    if (heldSurveys) toast.info(t('newcomers.timeTravel.surveysHeld'))
    ttOpen.value = false
  } catch (e) {
    toast.error(errorMessage(e, 'newcomers.timeTravel.failed'))
  } finally {
    ttBusy.value = false
  }
}

async function simulateStart(): Promise<void> {
  simulating.value = true
  try {
    newcomer.value = await apiSimulateStart({
      newcomer_id: id.value,
      address_form: 'vy',
      consent: 'given',
    })
    toast.success(t('newcomers.timeTravel.simulated'))
    // Consent sends the matured steps in the background: show them.
    await loadTimeline()
  } catch (e) {
    toast.error(errorMessage(e, 'newcomers.timeTravel.simulateFailed'))
  } finally {
    simulating.value = false
  }
}

// ── Recruiter's go-ahead (flag_required steps, spec r34/r36) ──────────────────
// The card has no flags of its own (GET does not return them): steps the bot
// holds for the go-ahead show in the timeline as "held", and the button
// releases them (POST /flags).
const RECRUITER_FLAG = 'recruiter_enabled'
const held = computed(() => (timeline.value ?? []).filter((e) => e.status === 'held'))
const giving = ref(false)
async function giveGoAhead(): Promise<void> {
  giving.value = true
  try {
    await apiSetNewcomerFlag(id.value, RECRUITER_FLAG, true)
    toast.success(t('newcomers.recruiterGo.done'))
    await loadTimeline()
  } catch (e) {
    toast.error(errorMessage(e, 'newcomers.recruiterGo.failed'))
  } finally {
    giving.value = false
  }
}

// ── Start date (PATCH /api/newcomers/{id}) ──────────────────────────────────
const rsOpen = ref(false)
const rsDate = ref('')
const rsBusy = ref(false)

watch(rsOpen, (open) => {
  if (open) ttOpen.value = false
  if (open && newcomer.value) rsDate.value = newcomer.value.start_date
})

async function reschedule(): Promise<void> {
  if (!newcomer.value || !rsDate.value) return
  if (rsDate.value === newcomer.value.start_date) {
    rsOpen.value = false
    return
  }
  rsBusy.value = true
  try {
    newcomer.value = await apiPatchNewcomer(id.value, { start_date: rsDate.value })
    toast.success(
      t('newcomers.reschedule.done', {
        date: d(isoToLocalDate(newcomer.value.start_date), 'short'),
      }),
    )
    rsOpen.value = false
    await loadTimeline()
  } catch (e) {
    toast.error(errorMessage(e, 'newcomers.reschedule.failed'))
  } finally {
    rsBusy.value = false
  }
}

function addressForm(n: Newcomer): string {
  return t(`newcomers.card.addressForm.${n.address_form ?? 'unknown'}`)
}
function consent(n: Newcomer): string {
  return t(`newcomers.card.consent.${n.consent ?? 'unknown'}`)
}
</script>

<template>
  <PageContainer>
    <LoadingState v-if="!newcomer && !error" variant="card" :rows="2" />
    <ErrorState v-else-if="error" :message="errorMessage(error, 'newcomers.card.loadFailed')">
      <template #retry>
        <Button size="sm" @click="load">{{ t('common.actions.retry') }}</Button>
      </template>
    </ErrorState>
    <template v-else-if="newcomer">
      <SectionCard class="head-card">
        <div class="head">
          <div class="head__main">
            <h1 class="page-title">{{ newcomer.full_name }}</h1>
            <div class="head__line">
              {{ newcomer.position }} · {{ t(`common.site.${newcomer.site}`) }} ·
              {{ newcomer.department }}
            </div>
            <div class="head__line muted">
              {{
                t('newcomers.card.people', {
                  manager: newcomer.manager_name,
                  mentor: newcomer.mentor_name || t('newcomers.card.noMentor'),
                  recruiter: newcomer.recruiter_name,
                })
              }}
            </div>
            <div class="head__line muted">
              {{
                t('newcomers.card.start', { date: d(isoToLocalDate(newcomer.start_date), 'short') })
              }}
              · {{ t('newcomers.card.today', { day: formatDayOffset(newcomer.day) }) }} ·
              {{ t(`common.stage.${newcomer.stage}`) }} · {{ addressForm(newcomer) }} ·
              {{ consent(newcomer) }}
            </div>
            <div class="head__badges">
              <Badge>{{
                t('newcomers.card.scenario', { version: newcomer.scenario_version })
              }}</Badge>
              <ConnectionBadge :connection="newcomer.connection" />
            </div>
            <div class="head__risk">
              <RiskMark :risk="newcomer.risk" :reason="newcomer.risk_reason" detailed />
            </div>
          </div>
          <div class="head__actions">
            <Button as-child>
              <RouterLink :to="{ name: 'newcomer-invite', params: { id: newcomer.id } }">
                <Link2 :size="14" />{{ t('newcomers.actions.invite') }}
              </RouterLink>
            </Button>
            <ManagerInviteButton :newcomer-id="newcomer.id" />
            <Button :aria-expanded="rsOpen" @click="rsOpen = !rsOpen">
              <CalendarClock :size="14" />{{ t('newcomers.reschedule.button') }}
            </Button>
            <Button variant="primary" :aria-expanded="ttOpen" @click="ttOpen = !ttOpen">
              <History :size="14" />{{ t('newcomers.timeTravel.button') }}
            </Button>
          </div>
        </div>

        <form v-if="rsOpen" class="tt-panel" @submit.prevent="reschedule">
          <div class="tt-panel__text">
            <div class="fw-semi">{{ t('newcomers.reschedule.title') }}</div>
            <p class="muted txt-sm">{{ t('newcomers.reschedule.hint') }}</p>
          </div>
          <label class="tt-panel__date">
            <span class="field-label">{{ t('newcomers.reschedule.date') }}</span>
            <input v-model="rsDate" class="input" type="date" required />
          </label>
          <div class="row gap-2">
            <Button type="button" @click="rsOpen = false">{{ t('common.actions.cancel') }}</Button>
            <Button type="submit" variant="primary" :disabled="rsBusy || !rsDate">
              {{ rsBusy ? t('newcomers.reschedule.submitting') : t('newcomers.reschedule.submit') }}
            </Button>
          </div>
        </form>

        <form v-if="ttOpen" class="tt-panel" @submit.prevent="travel">
          <div class="tt-panel__text">
            <div class="fw-semi">{{ t('newcomers.timeTravel.title') }}</div>
            <p class="muted txt-sm">{{ t('newcomers.timeTravel.hint') }}</p>
            <p v-if="!connected" class="tt-panel__warn txt-sm">
              {{ t('newcomers.timeTravel.notConnected') }}
            </p>
          </div>
          <label class="tt-panel__day">
            <span class="field-label">{{ t('newcomers.timeTravel.day') }}</span>
            <select v-model.number="ttDay" class="input">
              <option v-for="day in stepDays" :key="day" :value="day">
                {{ formatDayOffset(day) }}
              </option>
            </select>
          </label>
          <div class="row gap-2">
            <Button type="button" @click="ttOpen = false">{{ t('common.actions.cancel') }}</Button>
            <Button
              v-if="DEV && !connected"
              type="button"
              :disabled="simulating"
              :title="t('newcomers.timeTravel.simulateHint')"
              @click="simulateStart"
            >
              <PlugZap :size="14" />{{ t('newcomers.timeTravel.simulate') }}
            </Button>
            <Button
              type="submit"
              variant="primary"
              :disabled="ttBusy || stepDays.length === 0 || !connected"
            >
              {{ ttBusy ? t('newcomers.timeTravel.submitting') : t('newcomers.timeTravel.submit') }}
            </Button>
          </div>
        </form>
      </SectionCard>

      <NewcomerTickets :newcomer-id="newcomer.id" :site="newcomer.site" class="mt" />

      <SectionCard :title="t('newcomers.timeline.title')" class="mt">
        <LoadingState v-if="!timeline && !timelineError" :rows="6" />
        <ErrorState
          v-else-if="timelineError"
          :message="errorMessage(timelineError, 'newcomers.timeline.loadFailed')"
        >
          <template #retry>
            <Button size="sm" @click="loadTimeline">{{ t('common.actions.retry') }}</Button>
          </template>
        </ErrorState>
        <EmptyState
          v-else-if="timeline && timeline.length === 0"
          :title="t('newcomers.timeline.empty')"
        />
        <template v-else-if="timeline">
          <div v-if="held.length > 0" class="held" role="status">
            <Hourglass :size="16" class="held__icon" aria-hidden="true" />
            <div class="held__text">
              <div class="fw-medium">
                {{
                  t(
                    'newcomers.recruiterGo.held',
                    {
                      n: held.length,
                      titles: held.map((e) => e.title).join(', '),
                    },
                    held.length,
                  )
                }}
              </div>
              <div class="txt-sm muted">{{ t('newcomers.recruiterGo.hint') }}</div>
            </div>
            <Button size="sm" variant="primary" :disabled="giving" @click="giveGoAhead">
              {{ giving ? t('newcomers.recruiterGo.giving') : t('newcomers.recruiterGo.give') }}
            </Button>
          </div>
          <NewcomerTimeline :items="timeline" :today="newcomer.day" />
        </template>
      </SectionCard>
    </template>
  </PageContainer>
</template>

<style scoped>
.held {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 12px;
  margin-bottom: 12px;
  border: 1px solid var(--warning);
  border-radius: var(--radius-md);
}
.held__icon {
  color: var(--warning);
  flex-shrink: 0;
}
.held__text {
  flex: 1;
  min-width: 0;
}
.head {
  display: flex;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
}
.head__main {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
}
.head__main .page-title {
  margin: 0 0 4px;
}
.head__line {
  font-size: var(--text-base);
}
.head__badges {
  display: flex;
  gap: 6px;
  margin-top: 6px;
}
.head__risk {
  margin-top: 6px;
}
.head__actions {
  display: flex;
  gap: 8px;
  align-items: flex-start;
}
.tt-panel {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  gap: 12px 16px;
  margin-top: 16px;
  padding: 12px 14px;
  border: 1px dashed var(--border-strong);
  border-radius: var(--radius-md);
  background: var(--bg-subtle);
}
.tt-panel__text {
  flex: 1 1 320px;
}
.tt-panel__text p {
  margin: 4px 0 0;
}
.tt-panel__warn {
  color: var(--warning);
  font-weight: 500;
}
.tt-panel__date {
  display: flex;
  flex-direction: column;
  gap: 4px;
  width: 170px;
}
.tt-panel__day {
  display: flex;
  flex-direction: column;
  gap: 4px;
  width: 120px;
}
.mt {
  margin-top: 16px;
}
</style>
