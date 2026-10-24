<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, RouterLink } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { storeToRefs } from 'pinia'
import { CheckCircle2, EyeOff, Hand, Lock, Route as RouteIcon, UserPlus } from 'lucide-vue-next'
import PageContainer from '@/components/common/PageContainer.vue'
import SectionCard from '@/components/common/SectionCard.vue'
import ErrorState from '@/components/common/ErrorState.vue'
import LoadingState from '@/components/common/LoadingState.vue'
import RiskMark from '@/components/newcomers/RiskMark.vue'
import TicketSla from '@/components/tickets/TicketSla.vue'
import TicketStatus from '@/components/tickets/TicketStatus.vue'
import TicketPriority from '@/components/tickets/TicketPriority.vue'
import AssignDialog from '@/components/tickets/AssignDialog.vue'
import ResolveDialog from '@/components/tickets/ResolveDialog.vue'
import { Button } from '@/components/ui/button'
import { apiGetNewcomer } from '@/api/newcomers'
import {
  apiAssignTicket,
  apiGetTicket,
  apiListTickets,
  apiNoteTicket,
  apiPatchTicket,
  apiReplyTicket,
  apiResolveTicket,
} from '@/api/tickets'
import { useBreadcrumbTail } from '@/composables/useBreadcrumbs'
import { useToast } from '@/composables/useToast'
import { useNow } from '@/composables/useNow'
import { useTicketFormat } from '@/composables/useTicketFormat'
import { errorMessage } from '@/lib/apiError'
import { formatDayOffset } from '@/lib/calendar'
import {
  canNotifyManager,
  doneNoteBlock,
  foldQuote,
  isRestricted,
  signatureRole,
  ticketsOfNewcomer,
  visibleQuote,
} from '@/lib/tickets'
import { TONE_ICON, isSentiment } from '@/lib/tone'
import { useOperatorStore } from '@/stores/operator'
import { useTicketsStore } from '@/stores/tickets'
import type {
  Newcomer,
  Ticket,
  TicketAssignRequest,
  TicketDetail,
  TicketMessage,
} from '@/types/api'

const { t, d } = useI18n()
const route = useRoute()
const toast = useToast()
const now = useNow()
const { category } = useTicketFormat()
const { setBreadcrumbTail } = useBreadcrumbTail()
const queue = useTicketsStore()
const { name: me } = storeToRefs(useOperatorStore())

const id = computed(() => String(route.params.id))
const ticket = ref<TicketDetail | null>(null)
const error = ref<unknown>(null)
// Context comes from neighbouring endpoints; a failure there leaves the card
// usable, so it is not an error of the page.
const newcomer = ref<Newcomer | null>(null)
const siblings = ref<Ticket[]>([])

async function loadContext(tk: TicketDetail): Promise<void> {
  const [n, list] = await Promise.allSettled([
    apiGetNewcomer(tk.newcomer_id),
    apiListTickets({ newcomer_id: tk.newcomer_id }),
  ])
  if (ticket.value?.id !== tk.id) return
  newcomer.value = n.status === 'fulfilled' ? n.value : null
  siblings.value =
    list.status === 'fulfilled'
      ? ticketsOfNewcomer(list.value.items, tk.newcomer_id).filter((x) => x.id !== tk.id)
      : []
}

async function load(): Promise<void> {
  error.value = null
  ticket.value = null
  newcomer.value = null
  siblings.value = []
  try {
    const tk = await apiGetTicket(id.value)
    ticket.value = tk
    setBreadcrumbTail(t('tickets.detail.number', { number: tk.number }))
    void loadContext(tk)
  } catch (e) {
    error.value = e
  }
}
watch(id, load, { immediate: true })

// After any action: the contract does not say what reply/note/resolve answer,
// so the card is read again; the queue (and the sidebar badge) too.
async function refresh(): Promise<void> {
  try {
    ticket.value = await apiGetTicket(id.value)
  } catch (e) {
    toast.error(errorMessage(e, 'tickets.detail.loadFailed'))
  }
  void queue.load()
}

const restricted = computed(() => (ticket.value ? isRestricted(ticket.value) : false))
const resolved = computed(() => ticket.value?.status === 'resolved')
const mine = computed(
  () => !!ticket.value?.assignee_name && ticket.value.assignee_name === me.value,
)
const busy = ref(false)

// ── Take / assign / resolve ─────────────────────────────────────────────────
const assignOpen = ref(false)
const suggestions = computed(() => {
  const names = new Set<string>([me.value])
  for (const x of queue.items) if (x.assignee_name) names.add(x.assignee_name)
  return [...names].filter(Boolean)
})

async function assign(body: TicketAssignRequest, toastKey: 'taken' | 'assigned'): Promise<void> {
  if (!ticket.value) return
  busy.value = true
  try {
    await apiAssignTicket(ticket.value.id, body)
    toast.success(
      t(`tickets.detail.toast.${toastKey}`, {
        number: ticket.value.number,
        name: body.assignee_name,
      }),
    )
    assignOpen.value = false
    await refresh()
  } catch (e) {
    toast.error(
      errorMessage(
        e,
        toastKey === 'taken'
          ? 'tickets.detail.toast.takeFailed'
          : 'tickets.detail.toast.assignFailed',
      ),
    )
  } finally {
    busy.value = false
  }
}

// "Take it": straight away when nobody has it; taking it from someone else is
// a reassignment and asks for the comment.
function take(): void {
  if (!ticket.value || resolved.value || mine.value || busy.value) return
  if (ticket.value.assignee_name) {
    assignOpen.value = true
    return
  }
  void assign({ assignee_name: me.value.trim(), comment: '' }, 'taken')
}

function onAssign(body: TicketAssignRequest): void {
  void assign(body, body.assignee_name === me.value.trim() ? 'taken' : 'assigned')
}

// "Resolve" asks for an optional "what was done" first (wave 4, #31).
const resolveOpen = ref(false)
const noteBlock = computed(() => (ticket.value ? doneNoteBlock(ticket.value) : null))

function resolve(): void {
  if (!ticket.value || resolved.value || busy.value) return
  resolveOpen.value = true
}

async function onResolve(doneNote: string): Promise<void> {
  if (!ticket.value || resolved.value) return
  busy.value = true
  try {
    await apiResolveTicket(ticket.value.id, doneNote ? { done_note: doneNote } : {})
    resolveOpen.value = false
    toast.success(
      doneNote && !noteBlock.value
        ? t('tickets.detail.toast.resolvedNote', { number: ticket.value.number })
        : t('tickets.detail.toast.resolved', { number: ticket.value.number }),
    )
    await refresh()
  } catch (e) {
    toast.error(errorMessage(e, 'tickets.detail.toast.resolveFailed'))
  } finally {
    busy.value = false
  }
}

// Hotkey A — take it. KeyA, not the character: on the Russian layout the same
// key types another letter. Ignored while typing or in a dialog.
function onKey(e: KeyboardEvent): void {
  if (e.code !== 'KeyA' || e.ctrlKey || e.metaKey || e.altKey || e.repeat) return
  const el = e.target as HTMLElement | null
  if (el && (el.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(el.tagName))) return
  if (assignOpen.value) return
  e.preventDefault()
  take()
}
onMounted(() => document.addEventListener('keydown', onKey))
onBeforeUnmount(() => document.removeEventListener('keydown', onKey))

// ── Composer ────────────────────────────────────────────────────────────────
type ComposerMode = 'reply' | 'note'
const mode = ref<ComposerMode>('reply')
const text = ref('')
watch(resolved, (r) => {
  if (r) mode.value = 'note'
})
watch(id, () => {
  text.value = ''
  mode.value = 'reply'
})

async function send(andResolve: boolean): Promise<void> {
  const tk = ticket.value
  const body = text.value.trim()
  if (!tk || !body || busy.value) return
  busy.value = true
  try {
    if (mode.value === 'reply') {
      // The bot signs the reply with the assignee's name: an unassigned
      // ticket is taken first, so the newcomer never gets an unsigned answer.
      if (!tk.assignee_name)
        await apiAssignTicket(tk.id, { assignee_name: me.value.trim(), comment: '' })
      await apiReplyTicket(tk.id, { text: body, resolve: andResolve })
      toast.success(
        andResolve
          ? t('tickets.detail.toast.sentResolved', { number: tk.number })
          : t('tickets.detail.toast.sent'),
      )
    } else {
      await apiNoteTicket(tk.id, { text: body })
      if (andResolve) await apiResolveTicket(tk.id)
      toast.success(
        andResolve
          ? t('tickets.detail.toast.resolved', { number: tk.number })
          : t('tickets.detail.toast.noteSaved'),
      )
    }
    text.value = ''
  } catch (e) {
    toast.error(
      errorMessage(
        e,
        mode.value === 'reply'
          ? 'tickets.detail.toast.replyFailed'
          : 'tickets.detail.toast.noteFailed',
      ),
    )
  } finally {
    busy.value = false
    await refresh()
  }
}

function onComposerKey(e: KeyboardEvent): void {
  if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
    e.preventDefault()
    void send(false)
  }
}

// ── Summary card (wave 4, #26) ──────────────────────────────────────────────
// Title: the model's one-line summary when there is one, the category otherwise.
const headline = computed(() => ticket.value?.summary_model?.trim() || null)
const tone = computed(() => {
  const v = ticket.value?.tone
  return isSentiment(v) ? v : null
})
// F10 and F5/F9 never show a quote; a long one is folded.
const quote = computed(() => (ticket.value ? visibleQuote(ticket.value) : null))
const quoteFold = computed(() => (quote.value ? foldQuote(quote.value) : null))
const quoteFull = ref(false)
watch(id, () => {
  quoteFull.value = false
})

// ── Thread ──────────────────────────────────────────────────────────────────
function author(m: TicketMessage): string {
  if (m.from === 'bot') return t('tickets.detail.thread.bot')
  if (m.from === 'newcomer') return ticket.value?.newcomer_name ?? ''
  return m.author || t('tickets.detail.thread.staff')
}

// Only restricted tickets (F5/F9) hide the newcomer's words: the server sends
// text: null, hidden: true. Defence in depth: even if such a card arrives with
// the words, the cabinet does not print them.
function hidden(m: TicketMessage): boolean {
  return m.hidden || m.text === null || (restricted.value && m.from === 'newcomer')
}

// ── Notify the manager (PATCH notify_manager) ───────────────────────────────
async function toggleNotify(e: Event): Promise<void> {
  const tk = ticket.value
  const el = e.target as HTMLInputElement
  if (!tk || busy.value) return
  busy.value = true
  try {
    const updated = await apiPatchTicket(tk.id, { notify_manager: el.checked })
    if (ticket.value?.id === updated.id) ticket.value = updated
    toast.success(
      t(
        updated.notify_manager ? 'tickets.detail.toast.notifyOn' : 'tickets.detail.toast.notifyOff',
      ),
    )
  } catch (err) {
    el.checked = tk.notify_manager
    toast.error(errorMessage(err, 'tickets.detail.toast.notifyFailed'))
  } finally {
    busy.value = false
  }
}

function when(iso: string): string {
  const date = new Date(iso)
  const today = new Date(now.value)
  return date.toDateString() === today.toDateString()
    ? d(date, 'time')
    : `${d(date, 'dayMonth')} ${d(date, 'time')}`
}
</script>

<template>
  <PageContainer width="wide">
    <LoadingState v-if="!ticket && !error" variant="card" :rows="3" />
    <ErrorState v-else-if="error" :message="errorMessage(error, 'tickets.detail.loadFailed')">
      <template #retry>
        <Button size="sm" @click="load">{{ t('common.actions.retry') }}</Button>
      </template>
    </ErrorState>
    <template v-else-if="ticket">
      <SectionCard :class="['head-card', `head-card--${ticket.priority}`]">
        <div class="head">
          <div class="head__main">
            <h1 class="page-title head__title">
              <span class="tabular">{{
                t('tickets.detail.number', { number: ticket.number })
              }}</span>
              <span v-if="headline" class="head__cat">{{ headline }}</span>
              <span v-else class="head__cat" :title="t(`tickets.categoryFull.${ticket.category}`)">
                {{ category(ticket) }}
              </span>
            </h1>
            <div class="head__badges">
              <span
                v-if="headline"
                class="head__catline"
                :title="t(`tickets.categoryFull.${ticket.category}`)"
                >{{ category(ticket) }}</span
              >
              <TicketPriority :priority="ticket.priority" />
              <TicketStatus :status="ticket.status" />
              <TicketSla :ticket="ticket" :now="now" />
            </div>
            <div class="head__line">
              {{ ticket.newcomer_name }} · {{ t(`common.site.${ticket.site}`) }}
              <template v-if="newcomer"> · {{ newcomer.department }}</template>
              ·
              <span class="tabular" :title="t('tickets.detail.createdOnDayHint')">{{
                t('tickets.detail.createdOnDay', { day: formatDayOffset(ticket.day) })
              }}</span>
              <template v-if="newcomer">
                ·
                {{
                  t('tickets.detail.people', {
                    manager: newcomer.manager_name,
                    mentor: newcomer.mentor_name || '—',
                  })
                }}
              </template>
            </div>
          </div>
          <div class="head__actions">
            <Button
              v-if="!resolved && !mine"
              variant="primary"
              :disabled="busy"
              :title="t('tickets.detail.actions.takeHint')"
              @click="take"
            >
              <Hand :size="14" />{{ t('tickets.detail.actions.take') }}
              <kbd class="kbd">A</kbd>
            </Button>
            <Button :disabled="busy || resolved" @click="assignOpen = true">
              <UserPlus :size="14" />{{ t('tickets.detail.actions.assign') }}
            </Button>
            <Button
              :disabled="busy || resolved"
              :title="t('tickets.detail.actions.resolveHint')"
              @click="resolve"
            >
              <CheckCircle2 :size="14" />{{ t('tickets.detail.actions.resolve') }}
            </Button>
          </div>
        </div>
        <div v-if="restricted" class="restricted" role="note">
          <Lock :size="16" aria-hidden="true" />
          <div>
            <div class="fw-semi">{{ t('tickets.detail.restricted.banner') }}</div>
            <div class="txt-sm">{{ t('tickets.detail.restricted.hint') }}</div>
          </div>
        </div>
      </SectionCard>

      <div class="panes">
        <div class="panes__main">
          <SectionCard :title="t('tickets.detail.summary.title')">
            <p class="summary">{{ ticket.summary }}</p>
            <p v-if="tone" class="tone" :title="t('tickets.detail.summary.toneHint')">
              <span class="tone__icon" aria-hidden="true">{{ TONE_ICON[tone] }}</span>
              {{ t(`tickets.detail.summary.tone.${tone}`) }}
              <span class="muted">· {{ t('tickets.detail.summary.toneLabel') }}</span>
            </p>
            <blockquote v-if="quote && quoteFold" class="quote">
              <div class="quote__label">{{ t('tickets.detail.summary.quote') }}</div>
              «{{ quoteFull ? quote : quoteFold.short }}»
              <button
                v-if="quoteFold.folded"
                type="button"
                class="quote__more"
                :aria-expanded="quoteFull"
                @click="quoteFull = !quoteFull"
              >
                {{
                  quoteFull
                    ? t('tickets.detail.summary.showLess')
                    : t('tickets.detail.summary.showAll')
                }}
              </button>
            </blockquote>
          </SectionCard>

          <SectionCard :title="t('tickets.detail.thread.title')" class="mt">
            <div v-if="ticket.messages.length === 0" class="muted txt-sm">
              {{ t('tickets.detail.thread.empty') }}
            </div>
            <ol v-else class="thread">
              <li
                v-for="(m, i) in ticket.messages"
                :key="i"
                :class="['msg', `msg--${m.from}`, { 'msg--internal': m.internal }]"
              >
                <div class="msg__meta">
                  <span class="fw-medium">{{ author(m) }}</span>
                  <span v-if="m.internal" class="msg__tag">{{
                    t('tickets.detail.thread.internal')
                  }}</span>
                  <span class="msg__time tabular">{{ when(m.at) }}</span>
                </div>
                <div v-if="hidden(m)" class="msg__text msg__text--hidden">
                  <EyeOff :size="12" aria-hidden="true" />{{
                    t('tickets.detail.restricted.hidden')
                  }}
                </div>
                <div v-else class="msg__text">{{ m.text }}</div>
              </li>
            </ol>

            <form
              :class="['composer', { 'composer--note': mode === 'note' }]"
              :aria-label="t('tickets.detail.composer.label')"
              @submit.prevent="send(false)"
            >
              <div class="tabs composer__tabs" role="group">
                <button
                  type="button"
                  :class="['tab', { active: mode === 'reply' }]"
                  :aria-pressed="mode === 'reply'"
                  :disabled="resolved"
                  @click="mode = 'reply'"
                >
                  {{ t('tickets.detail.composer.reply') }}
                </button>
                <button
                  type="button"
                  :class="['tab', { active: mode === 'note' }]"
                  :aria-pressed="mode === 'note'"
                  @click="mode = 'note'"
                >
                  {{ t('tickets.detail.composer.note') }}
                </button>
              </div>
              <textarea
                v-model="text"
                class="textarea composer__input"
                :placeholder="
                  mode === 'reply'
                    ? t(`tickets.detail.composer.replyPlaceholderRole.${signatureRole(ticket)}`, {
                        operator: ticket.assignee_name || me,
                      })
                    : t('tickets.detail.composer.notePlaceholder')
                "
                :aria-label="
                  mode === 'reply'
                    ? t('tickets.detail.composer.reply')
                    : t('tickets.detail.composer.note')
                "
                @keydown="onComposerKey"
              />
              <p class="composer__hint txt-sm">
                <template v-if="resolved">{{ t('tickets.detail.composer.resolvedHint') }}</template>
                <template v-else-if="mode === 'reply'">
                  {{
                    t('tickets.detail.composer.replyHint', {
                      operator: ticket.assignee_name || me,
                      role: t(`tickets.detail.composer.role.${signatureRole(ticket)}`),
                      number: ticket.number,
                    })
                  }}
                </template>
                <template v-else>{{ t('tickets.detail.composer.noteHint') }}</template>
              </p>
              <div class="composer__foot">
                <span class="muted txt-xs">{{ t('tickets.detail.composer.shortcut') }}</span>
                <span class="grow" />
                <Button
                  v-if="!resolved"
                  type="button"
                  :disabled="busy || !text.trim()"
                  @click="send(true)"
                >
                  {{ t('tickets.detail.composer.sendResolve') }}
                </Button>
                <Button type="submit" variant="primary" :disabled="busy || !text.trim()">
                  {{
                    busy ? t('tickets.detail.composer.sending') : t('tickets.detail.composer.send')
                  }}
                </Button>
              </div>
            </form>
          </SectionCard>
        </div>

        <aside class="panes__side">
          <SectionCard :title="t('tickets.detail.context.title')">
            <RouterLink
              class="ctx-link"
              :to="{ name: 'newcomer', params: { id: ticket.newcomer_id } }"
            >
              <RouteIcon :size="14" aria-hidden="true" />{{
                t('tickets.detail.context.trajectory')
              }}
              →
            </RouterLink>
            <dl class="ctx">
              <dt>{{ t('tickets.detail.context.newcomer') }}</dt>
              <dd>
                {{ ticket.newcomer_name }}
                <div v-if="newcomer" class="muted txt-sm">{{ newcomer.position }}</div>
              </dd>
              <template v-if="newcomer">
                <dt>{{ t('tickets.detail.context.department') }}</dt>
                <dd>{{ newcomer.department }}</dd>
                <dt>{{ t('tickets.detail.context.manager') }}</dt>
                <dd>{{ newcomer.manager_name }}</dd>
                <dt>{{ t('tickets.detail.context.mentor') }}</dt>
                <dd>{{ newcomer.mentor_name || '—' }}</dd>
                <dt>{{ t('tickets.detail.context.today') }}</dt>
                <dd class="tabular">
                  {{ formatDayOffset(newcomer.day) }} · {{ t(`common.stage.${newcomer.stage}`) }}
                </dd>
                <dt>{{ t('tickets.detail.context.risk') }}</dt>
                <dd><RiskMark :risk="newcomer.risk" :reason="newcomer.risk_reason" /></dd>
              </template>
              <dt>{{ t('tickets.detail.context.route') }}</dt>
              <dd>{{ t(`tickets.route.${ticket.route}`) }}</dd>
              <dt>{{ t('tickets.detail.context.assignee') }}</dt>
              <dd>{{ ticket.assignee_name || t('tickets.card.unassigned') }}</dd>
              <dt>{{ t('tickets.detail.context.created') }}</dt>
              <dd class="tabular">{{ d(new Date(ticket.created_at), 'long') }}</dd>
            </dl>

            <p v-if="ticket.anonymous" class="ctx-note">
              <EyeOff :size="14" aria-hidden="true" />{{ t('tickets.detail.context.anonymous') }}
            </p>

            <div class="ctx-block">
              <template v-if="canNotifyManager(ticket.category) && !restricted">
                <label class="ctx-check">
                  <input
                    type="checkbox"
                    :checked="ticket.notify_manager"
                    :disabled="busy || resolved"
                    @change="toggleNotify"
                  />
                  {{ t('tickets.detail.context.notify') }}
                </label>
                <p class="muted txt-sm">{{ t('tickets.detail.context.notifyHint') }}</p>
              </template>
              <p v-else class="muted txt-sm">{{ t('tickets.detail.context.noManager') }}</p>
            </div>

            <div class="ctx-block">
              <div class="ctx-block__title">{{ t('tickets.detail.context.previous') }}</div>
              <ul v-if="siblings.length > 0" class="ctx-list">
                <li v-for="s in siblings" :key="s.id">
                  <RouterLink :to="{ name: 'ticket', params: { id: s.id } }" class="ctx-list__link">
                    #{{ s.number }} · {{ category(s) }}
                  </RouterLink>
                  <TicketStatus :status="s.status" />
                </li>
              </ul>
              <p v-else class="muted txt-sm">{{ t('tickets.detail.context.previousNone') }}</p>
            </div>
          </SectionCard>
        </aside>
      </div>

      <AssignDialog
        v-model:open="assignOpen"
        :number="ticket.number"
        :current="ticket.assignee_name"
        :me="me"
        :suggestions="suggestions"
        :busy="busy"
        @submit="onAssign"
      />
      <ResolveDialog
        v-model:open="resolveOpen"
        :number="ticket.number"
        :block="noteBlock"
        :busy="busy"
        @submit="onResolve"
      />
    </template>
  </PageContainer>
</template>

<style scoped>
.head-card {
  border-left: 4px solid var(--priority-p2);
}
.head-card--P0 {
  border-left-color: var(--priority-p0);
}
.head-card--P1 {
  border-left-color: var(--priority-p1);
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
  gap: 6px;
  min-width: 0;
}
.head__title {
  display: flex;
  align-items: baseline;
  flex-wrap: wrap;
  gap: 10px;
  margin: 0;
}
.head__cat {
  font-weight: 500;
}
.head__badges {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.head__line {
  font-size: var(--text-base);
  color: var(--fg-muted);
}
.head__actions {
  display: flex;
  gap: 8px;
  align-items: flex-start;
  flex-wrap: wrap;
}
.kbd {
  margin-left: 4px;
  padding: 0 4px;
  border-radius: var(--radius-xs);
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  background: color-mix(in oklab, currentColor 18%, transparent);
}
.restricted {
  display: flex;
  gap: 10px;
  align-items: flex-start;
  margin-top: 14px;
  padding: 10px 12px;
  border-radius: var(--radius-md);
  background: var(--danger-soft);
  color: var(--danger);
}
.restricted .txt-sm {
  color: var(--fg-muted);
  margin-top: 2px;
}
.panes {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 320px;
  gap: 16px;
  margin-top: 16px;
  align-items: start;
}
.mt {
  margin-top: 16px;
}
.summary {
  margin: 0;
  line-height: var(--leading-normal);
}
.quote {
  margin: 12px 0 0;
  padding: 8px 12px;
  border-left: 3px solid var(--border-strong);
  background: var(--bg-subtle);
  border-radius: 0 var(--radius-sm) var(--radius-sm) 0;
}
.tone {
  display: flex;
  align-items: center;
  gap: 6px;
  margin: 8px 0 0;
  font-size: var(--text-sm);
}
.tone__icon {
  font-size: 16px;
  line-height: 1;
}
.head__catline {
  font-size: var(--text-sm);
  color: var(--fg-muted);
  align-self: center;
}
.quote__more {
  display: block;
  margin-top: 6px;
  padding: 0;
  border: 0;
  background: none;
  color: var(--accent);
  font: inherit;
  font-size: var(--text-sm);
  font-style: normal;
  cursor: pointer;
}
.quote__label {
  font-size: var(--text-xs);
  color: var(--fg-subtle);
  margin-bottom: 2px;
}
.thread {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.msg {
  max-width: 85%;
  padding: 8px 12px;
  border-radius: var(--radius-lg);
  border: 1px solid var(--border);
  background: var(--surface);
}
.msg--bot {
  background: var(--bg-muted);
  border-color: transparent;
}
.msg--newcomer {
  align-self: flex-start;
}
.msg--staff {
  align-self: flex-end;
  background: var(--accent-soft);
  border-color: transparent;
}
.msg--internal {
  align-self: stretch;
  max-width: none;
  background: var(--note-bg);
  border: 1px solid var(--note-border);
}
.msg__meta {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: var(--text-sm);
  margin-bottom: 2px;
}
.msg__tag {
  font-size: var(--text-xs);
  color: var(--param-assumption-fg);
  font-weight: 500;
}
.msg__time {
  margin-left: auto;
  color: var(--fg-subtle);
  font-size: var(--text-xs);
}
.msg__text {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.msg__text--hidden {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  color: var(--fg-subtle);
  font-style: italic;
}
.composer {
  margin-top: 16px;
  padding-top: 14px;
  border-top: 1px solid var(--border);
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.composer__tabs {
  align-self: flex-start;
}
.composer__tabs .tab {
  border: 0;
  background: transparent;
}
.composer__tabs .tab.active {
  background: var(--bg);
}
.composer__tabs .tab:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.composer__input {
  min-height: 96px;
  padding: 8px 10px;
  resize: vertical;
}
.composer--note .composer__input {
  background: var(--note-bg);
  border-color: var(--note-border);
}
.composer__hint {
  margin: 0;
  color: var(--fg-subtle);
}
.composer__foot {
  display: flex;
  align-items: center;
  gap: 8px;
}
.ctx-link {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-weight: 500;
  color: var(--fg);
  margin-bottom: 12px;
}
.ctx-link:hover {
  text-decoration: underline;
}
.ctx {
  display: grid;
  grid-template-columns: 110px minmax(0, 1fr);
  gap: 6px 10px;
  margin: 0;
  font-size: var(--text-base);
}
.ctx dt {
  color: var(--fg-subtle);
  font-size: var(--text-sm);
}
.ctx dd {
  margin: 0;
}
.ctx-note {
  display: flex;
  gap: 6px;
  align-items: flex-start;
  margin: 12px 0 0;
  font-size: var(--text-sm);
  color: var(--fg-muted);
}
.ctx-block {
  margin-top: 14px;
  padding-top: 12px;
  border-top: 1px solid var(--border);
}
.ctx-block p {
  margin: 4px 0 0;
}
.ctx-block__title {
  font-size: var(--text-sm);
  color: var(--fg-subtle);
  margin-bottom: 6px;
}
.ctx-check {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  font-weight: 500;
}
.ctx-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.ctx-list li {
  display: flex;
  justify-content: space-between;
  gap: 8px;
  align-items: center;
  font-size: var(--text-sm);
}
.ctx-list__link {
  color: var(--fg);
}
.ctx-list__link:hover {
  text-decoration: underline;
}
@media (max-width: 1000px) {
  .panes {
    grid-template-columns: 1fr;
  }
}
</style>
