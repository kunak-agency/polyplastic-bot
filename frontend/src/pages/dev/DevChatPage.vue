<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { History, PlugZap, Send } from 'lucide-vue-next'
import PageContainer from '@/components/common/PageContainer.vue'
import SectionCard from '@/components/common/SectionCard.vue'
import { Button } from '@/components/ui/button'
import { apiDevChat, apiDevChatPress, apiDevChatSay } from '@/api/devChat'
import { apiGetTimeline, apiListNewcomers, apiSimulateStart, apiTimeTravel } from '@/api/newcomers'
import { useToast } from '@/composables/useToast'
import { errorMessage, isApiError } from '@/lib/apiError'
import { formatDayOffset } from '@/lib/calendar'
import { buttonRows, renderMarkup, type ChatButton } from '@/lib/chatMarkup'
import type { AddressForm, Consent, Newcomer } from '@/types/api'
import type { DevChatMessage } from '@/types/devChat'

// Dev-only emulator of the newcomer's MAX dialog (api_contract.md, "dev"): the
// feed is the fake MAX journal, buttons and text go through the dev hooks.
const { t, d } = useI18n()
const toast = useToast()

const newcomers = ref<Newcomer[]>([])
const selectedId = ref('')
const selected = computed(() => newcomers.value.find((n) => n.id === selectedId.value) ?? null)

const messages = ref<DevChatMessage[]>([])
const typing = ref(false)
const loadError = ref<unknown>(null)
const unavailable = ref(false)
const inFlight = ref(0)
const text = ref('')
const feed = ref<HTMLElement | null>(null)

async function loadNewcomers(): Promise<void> {
  try {
    newcomers.value = (await apiListNewcomers()).items
    if (!selectedId.value && newcomers.value.length > 0) selectedId.value = newcomers.value[0].id
  } catch (e) {
    toast.error(errorMessage(e))
  }
}

async function refreshNewcomer(): Promise<void> {
  const list = (await apiListNewcomers()).items
  newcomers.value = list
}

async function poll(): Promise<void> {
  if (!selectedId.value) return
  try {
    const r = await apiDevChat(selectedId.value)
    const grew = r.messages.length !== messages.value.length
    messages.value = r.messages
    typing.value = r.typing
    loadError.value = null
    unavailable.value = false
    if (grew) await scrollDown()
  } catch (e) {
    loadError.value = e
    unavailable.value = isApiError(e) && e.status === 404
  }
}

async function scrollDown(): Promise<void> {
  await nextTick()
  if (feed.value) feed.value.scrollTop = feed.value.scrollHeight
}

// Poll every 5 s; every 1 s while a request is in flight or the bot types
// (the RAG answer arrives while the press/text request is still pending).
let timer: ReturnType<typeof setTimeout> | undefined
function schedule(): void {
  clearTimeout(timer)
  const fast = inFlight.value > 0 || typing.value
  timer = setTimeout(
    async () => {
      await poll()
      schedule()
    },
    fast ? 1000 : 5000,
  )
}

async function busy<T>(fn: () => Promise<T>): Promise<T> {
  inFlight.value++
  schedule()
  try {
    return await fn()
  } finally {
    inFlight.value--
    await poll()
    schedule()
  }
}

watch(selectedId, async () => {
  messages.value = []
  await poll()
  await scrollDown()
  await loadDays()
})

onMounted(async () => {
  await loadNewcomers()
  schedule()
})
onBeforeUnmount(() => clearTimeout(timer))

// ── Connect (simulate-start) ────────────────────────────────────────────────
const addressForm = ref<AddressForm | ''>('')
const consent = ref<Consent | ''>('')
async function connect(): Promise<void> {
  if (!selectedId.value) return
  try {
    await busy(() =>
      apiSimulateStart({
        newcomer_id: selectedId.value,
        address_form: addressForm.value || null,
        consent: consent.value || null,
      }),
    )
    toast.success(t('devchat.connect.done'))
    await refreshNewcomer()
  } catch (e) {
    toast.error(errorMessage(e, 'devchat.connect.failed'))
  }
}

// ── Time travel ─────────────────────────────────────────────────────────────
const days = ref<number[]>([])
const ttDay = ref(0)
async function loadDays(): Promise<void> {
  days.value = []
  if (!selectedId.value) return
  try {
    const items = (await apiGetTimeline(selectedId.value)).items
    days.value = [...new Set(items.map((e) => e.day_offset))].sort((a, b) => a - b)
    const pending = items.find((e) => e.status === 'scheduled')
    ttDay.value = pending?.day_offset ?? days.value[0] ?? 0
  } catch {
    // the select stays empty; the chat itself still works
  }
}
async function travel(): Promise<void> {
  try {
    await busy(() => apiTimeTravel(selectedId.value, ttDay.value))
    toast.success(t('devchat.timeTravel.done'))
    await loadDays()
  } catch (e) {
    toast.error(errorMessage(e, 'devchat.timeTravel.failed'))
  }
}

// ── Newcomer actions ────────────────────────────────────────────────────────
async function say(value: string): Promise<void> {
  const v = value.trim()
  if (!v || !selectedId.value) return
  try {
    await busy(() => apiDevChatSay(selectedId.value, v))
  } catch (e) {
    toast.error(errorMessage(e, 'devchat.chat.sendFailed'))
  }
}
async function submitText(): Promise<void> {
  const v = text.value
  text.value = ''
  await say(v)
}
async function press(m: DevChatMessage, b: ChatButton): Promise<void> {
  if (b.action === 'open' && b.url) {
    window.open(b.url, '_blank', 'noopener,noreferrer')
    return
  }
  if (b.action === 'send') return say(b.text)
  if (b.action !== 'press') {
    toast.info(t('devchat.chat.noAction'))
    return
  }
  try {
    await busy(() => apiDevChatPress(selectedId.value, m.id, b.payload))
  } catch (e) {
    toast.error(errorMessage(e, 'devchat.chat.pressFailed'))
  }
}

function time(iso: string): string {
  return d(new Date(iso), 'time')
}
</script>

<template>
  <PageContainer width="wide">
    <h1 class="page-title">{{ t('devchat.pageTitle') }}</h1>
    <p class="muted txt-sm">{{ t('devchat.hint') }}</p>

    <div class="devchat">
      <aside class="devchat__side">
        <SectionCard :title="t('devchat.newcomer')">
          <select v-model="selectedId" class="input" data-testid="newcomer-select">
            <option value="" disabled>{{ t('devchat.choose') }}</option>
            <option v-for="n in newcomers" :key="n.id" :value="n.id">
              {{ n.full_name }} ·
              {{
                n.connection === 'connected' ? t('devchat.connected') : t('devchat.notConnected')
              }}
            </option>
          </select>
        </SectionCard>

        <SectionCard :title="t('devchat.connect.title')" class="mt">
          <label class="field">
            <span class="field-label">{{ t('devchat.connect.address') }}</span>
            <select v-model="addressForm" class="input" data-testid="address-form">
              <option value="">{{ t('devchat.connect.none') }}</option>
              <option value="ty">{{ t('devchat.connect.ty') }}</option>
              <option value="vy">{{ t('devchat.connect.vy') }}</option>
            </select>
          </label>
          <label class="field">
            <span class="field-label">{{ t('devchat.connect.consent') }}</span>
            <select v-model="consent" class="input" data-testid="consent">
              <option value="">{{ t('devchat.connect.none') }}</option>
              <option value="given">{{ t('devchat.connect.given') }}</option>
              <option value="declined">{{ t('devchat.connect.declined') }}</option>
            </select>
          </label>
          <Button
            :disabled="!selectedId || inFlight > 0 || selected?.connection === 'connected'"
            data-testid="connect"
            @click="connect"
          >
            <PlugZap :size="14" />{{ t('devchat.connect.submit') }}
          </Button>
        </SectionCard>

        <SectionCard :title="t('devchat.timeTravel.title')" class="mt">
          <label class="field">
            <span class="field-label">{{ t('devchat.timeTravel.day') }}</span>
            <select v-model.number="ttDay" class="input" data-testid="tt-day">
              <option v-for="day in days" :key="day" :value="day">
                {{ formatDayOffset(day) }}
              </option>
            </select>
          </label>
          <Button
            variant="primary"
            :disabled="!selectedId || days.length === 0 || inFlight > 0"
            data-testid="time-travel"
            @click="travel"
          >
            <History :size="14" />{{ t('devchat.timeTravel.submit') }}
          </Button>
        </SectionCard>
      </aside>

      <section class="phone" :aria-label="t('devchat.pageTitle')">
        <header class="phone__head">
          <div class="phone__avatar">K</div>
          <div>
            <div class="fw-semi">{{ t('devchat.chat.bot') }}</div>
            <div class="txt-sm muted" data-testid="typing">
              {{ typing ? t('devchat.chat.typing') : selected?.full_name }}
            </div>
          </div>
        </header>

        <div ref="feed" class="phone__feed" data-testid="feed">
          <p v-if="unavailable" class="phone__note">{{ t('devchat.chat.unavailable') }}</p>
          <p v-else-if="loadError" class="phone__note">
            {{ errorMessage(loadError, 'devchat.chat.loadFailed') }}
          </p>
          <p v-else-if="messages.length === 0" class="phone__note">{{ t('devchat.chat.empty') }}</p>

          <template v-for="m in messages" :key="m.id">
            <div v-if="m.from === 'toast'" class="toast-pill" data-testid="toast">
              {{ m.text }}
            </div>
            <div
              v-else
              :class="['msg', m.from === 'bot' ? 'msg--bot' : 'msg--me']"
              :data-testid="m.from === 'bot' ? 'bot-msg' : 'my-msg'"
              :data-mid="m.id"
            >
              <div class="msg__bubble">
                <div v-for="(a, i) in m.attachments ?? []" :key="i" class="msg__att">
                  {{ t('devchat.chat.attachment', { type: a.type }) }}
                </div>
                <div v-if="m.kind === 'start'" class="msg__text">
                  {{ t('devchat.chat.start') }}
                </div>
                <div v-else-if="m.kind === 'press'" class="msg__text msg__press">{{ m.text }}</div>
                <div v-else class="msg__text">
                  <template v-for="(s, i) in renderMarkup(m.text, m.format)" :key="i">
                    <a
                      v-if="s.href"
                      :href="s.href"
                      target="_blank"
                      rel="noopener noreferrer"
                      :class="{ b: s.bold, i: s.italic, u: s.underline, s: s.strike, c: s.code }"
                      >{{ s.text }}</a
                    ><span
                      v-else
                      :class="{ b: s.bold, i: s.italic, u: s.underline, s: s.strike, c: s.code }"
                      >{{ s.text }}</span
                    >
                  </template>
                </div>
                <div class="msg__meta">
                  <span v-if="m.edited" data-testid="edited"
                    >{{ t('devchat.chat.edited') }} ·
                  </span>
                  <span v-if="m.from === 'bot' && !m.notify"
                    >{{ t('devchat.chat.silent') }} ·
                  </span>
                  {{ time(m.edited_at ?? m.at) }}
                </div>
              </div>
              <div v-if="m.from === 'bot' && m.buttons?.length" class="kb">
                <div v-for="(row, ri) in buttonRows(m.buttons)" :key="ri" class="kb__row">
                  <button
                    v-for="(b, bi) in row"
                    :key="bi"
                    type="button"
                    :class="['kb__btn', `kb__btn--${b.action}`]"
                    :disabled="inFlight > 0 && b.action === 'press'"
                    :data-payload="b.payload"
                    @click="press(m, b)"
                  >
                    {{ b.text }}<span v-if="b.action === 'open'" aria-hidden="true"> ↗</span>
                  </button>
                </div>
              </div>
            </div>
          </template>
          <div v-if="typing" class="typing-dots" aria-hidden="true"><i /><i /><i /></div>
        </div>

        <form class="phone__input" @submit.prevent="submitText">
          <input
            v-model="text"
            class="input"
            :placeholder="t('devchat.chat.placeholder')"
            :disabled="!selected || selected.connection !== 'connected'"
            data-testid="chat-input"
          />
          <Button
            type="submit"
            variant="primary"
            size="icon"
            :aria-label="t('devchat.chat.send')"
            :disabled="!text.trim() || !selected"
            data-testid="chat-send"
          >
            <Send :size="14" />
          </Button>
        </form>
      </section>
    </div>
  </PageContainer>
</template>

<style scoped>
.devchat {
  display: grid;
  grid-template-columns: minmax(240px, 320px) minmax(320px, 440px);
  gap: 24px;
  margin-top: 16px;
  align-items: start;
}
.mt {
  margin-top: 16px;
}
.field {
  display: block;
  margin-bottom: 12px;
}
.phone {
  display: flex;
  flex-direction: column;
  height: 78vh;
  min-height: 520px;
  border: 1px solid var(--border);
  border-radius: 28px;
  background: var(--bg-subtle);
  overflow: hidden;
  box-shadow: var(--shadow-md);
}
.phone__head {
  display: flex;
  gap: 10px;
  align-items: center;
  padding: 12px 16px;
  background: var(--bg-elevated);
  border-bottom: 1px solid var(--border-subtle);
}
.phone__avatar {
  width: 36px;
  height: 36px;
  border-radius: var(--radius-full);
  display: grid;
  place-items: center;
  background: var(--accent);
  color: var(--accent-fg);
  font-weight: 600;
}
.phone__feed {
  flex: 1;
  overflow-y: auto;
  padding: 12px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.phone__note {
  color: var(--fg-muted);
  text-align: center;
  font-size: 13px;
  margin: auto 0;
}
.phone__input {
  display: flex;
  gap: 8px;
  padding: 10px;
  background: var(--bg-elevated);
  border-top: 1px solid var(--border-subtle);
}
.msg {
  display: flex;
  flex-direction: column;
  max-width: 88%;
}
.msg--bot {
  align-self: flex-start;
}
.msg--me {
  align-self: flex-end;
  align-items: flex-end;
}
.msg__bubble {
  padding: 8px 12px;
  border-radius: 16px;
  background: var(--bg-elevated);
  border: 1px solid var(--border-subtle);
}
.msg--me .msg__bubble {
  background: var(--accent-soft);
  border-color: var(--accent-border);
}
.msg__text {
  white-space: pre-wrap;
  word-break: break-word;
  font-size: 14px;
  line-height: 1.4;
}
.msg__press::before {
  content: '\25B8  ';
  color: var(--fg-muted);
}
.msg__att {
  font-size: 12px;
  color: var(--fg-muted);
  margin-bottom: 4px;
}
.msg__meta {
  font-size: 11px;
  color: var(--fg-faint);
  text-align: right;
  margin-top: 2px;
}
.b {
  font-weight: 700;
}
.i {
  font-style: italic;
}
.u {
  text-decoration: underline;
}
.s {
  text-decoration: line-through;
}
.c {
  font-family: var(--font-mono);
  background: var(--code-bg);
  border-radius: 4px;
  padding: 0 3px;
}
.kb {
  display: flex;
  flex-direction: column;
  gap: 4px;
  margin-top: 4px;
}
.kb__row {
  display: flex;
  gap: 4px;
}
.kb__btn {
  flex: 1;
  min-width: 0;
  padding: 7px 8px;
  border-radius: 10px;
  border: 1px solid var(--border);
  background: var(--bg-elevated);
  color: var(--accent);
  font-size: 13px;
  cursor: pointer;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.kb__btn:hover:not(:disabled) {
  background: var(--accent-soft);
}
.kb__btn:disabled {
  opacity: 0.5;
  cursor: progress;
}
.kb__btn--none {
  color: var(--fg-muted);
}
.toast-pill {
  align-self: center;
  font-size: 12px;
  padding: 4px 10px;
  border-radius: var(--radius-full);
  background: var(--fg);
  color: var(--bg);
  opacity: 0.8;
}
.typing-dots {
  align-self: flex-start;
  display: flex;
  gap: 4px;
  padding: 10px 12px;
  background: var(--bg-elevated);
  border-radius: 16px;
}
.typing-dots i {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--fg-muted);
  animation: blink 1s infinite;
}
.typing-dots i:nth-child(2) {
  animation-delay: 0.2s;
}
.typing-dots i:nth-child(3) {
  animation-delay: 0.4s;
}
@keyframes blink {
  50% {
    opacity: 0.2;
  }
}
@media (max-width: 860px) {
  .devchat {
    grid-template-columns: 1fr;
  }
}
</style>
