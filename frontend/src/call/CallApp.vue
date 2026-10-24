<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Headphones, Mic, MicOff, Phone, PhoneOff, Send } from 'lucide-vue-next'
import AppLogo from '@/components/common/AppLogo.vue'
import { Button } from '@/components/ui/button'
import CallBubble from './components/CallBubble.vue'
import { useCallStore } from './store'
import { CALL_SITES, type CallSite } from './protocol'

// call.plexet.ru: one page, a voice call to Korsik with the regulation
// sources of every answer. The call logic is in ./session.ts.
const { t } = useI18n()
const call = useCallStore()
const s = call.state

const EXAMPLES = ['advance', 'vacation', 'sick'] as const
const SEGMENTS = 16

// ── Timer ──
const now = ref(Date.now())
let tick: ReturnType<typeof setInterval> | null = null
onMounted(() => {
  tick = setInterval(() => (now.value = Date.now()), 500)
})
const elapsed = computed(() => {
  if (s.startedAt === null) return '00:00'
  const end = s.endedAt ?? now.value
  const sec = Math.max(0, Math.floor((end - s.startedAt) / 1000))
  const mm = String(Math.floor(sec / 60)).padStart(2, '0')
  const ss = String(sec % 60).padStart(2, '0')
  return `${mm}:${ss}`
})

// ── Status ──
const live = computed(() => s.mic === 'live' && !s.muted && call.inCall)
const lit = computed(() => (live.value ? Math.round(s.micLevel * SEGMENTS) : 0))
const ring = computed(() => `${Math.round((live.value ? s.micLevel : 0) * 18)}px`)
const showVoice = computed(() => call.inCall && s.input === 'voice')

const phaseText = computed(() => {
  if (call.inCall && s.input === 'voice' && s.muted) return t('call.mic.muted')
  return t(`call.phase.${s.phase}`)
})

const micProblem = computed(() => {
  switch (s.mic) {
    case 'denied':
    case 'notFound':
    case 'unsupported':
      return s.mic
    case 'failed':
      return 'micFailed'
    default:
      return null
  }
})

const errorText = computed(() => {
  if (s.problem) return t(`call.errors.${s.problem}`)
  if (micProblem.value) return t(`call.errors.${micProblem.value}`)
  return ''
})

// ── Actions ──
function onCall() {
  call.start({ input: 'voice' })
}

function onTextOnly() {
  call.start({ input: 'text' })
}

function onSite(site: CallSite) {
  call.setSite(site)
}

const draft = ref('')
function onSubmit() {
  const text = draft.value.trim()
  if (!text) return
  call.ask(text)
  draft.value = ''
}

function onExample(key: (typeof EXAMPLES)[number]) {
  call.ask(t(`call.examples.${key}`))
}

// ── Transcript scroll ──
const feed = ref<HTMLElement | null>(null)
watch(
  () => [s.messages.length, s.messages[s.messages.length - 1]?.text],
  async () => {
    await nextTick()
    const el = feed.value
    if (el) el.scrollTop = el.scrollHeight
  },
)

function onPageHide() {
  call.disconnect()
}

onMounted(() => {
  window.addEventListener('pagehide', onPageHide)
  // Dev only: `?demo=transcript` paints a finished conversation (screenshots,
  // layout work) without a backend. The fixture is not in the production build.
  if (import.meta.env.DEV && new URLSearchParams(window.location.search).has('demo')) {
    void import('@/mocks/callDemo').then((m) => call.loadDemo(m.callDemoState()))
  }
})

onBeforeUnmount(() => {
  if (tick) clearInterval(tick)
  window.removeEventListener('pagehide', onPageHide)
  call.disconnect()
})
</script>

<template>
  <main class="call-page">
    <header class="call-head">
      <AppLogo :size="22" />
      <h1 class="call-head__title">{{ t('call.title') }}</h1>
      <p class="call-head__sub">{{ t('call.subtitle') }}</p>
    </header>

    <section class="card call-panel">
      <div class="call-panel__sites">
        <span class="call-panel__label">{{ t('common.site.label') }}</span>
        <div class="tabs" role="radiogroup" :aria-label="t('common.site.label')">
          <button
            v-for="site in CALL_SITES"
            :key="site"
            type="button"
            role="radio"
            class="tab"
            :class="{ active: s.site === site }"
            :aria-checked="s.site === site"
            :disabled="call.inCall || call.busy"
            @click="onSite(site)"
          >
            {{ t(`common.site.${site}`) }}
          </button>
        </div>
      </div>

      <div class="call-panel__main">
        <div
          class="call-orb"
          :class="{
            'call-orb--live': call.inCall,
            'call-orb--speaking': live && s.userSpeaking,
            'call-orb--bot': call.inCall && s.botPlaying,
          }"
          :style="{ '--ring': ring }"
          aria-hidden="true"
        >
          <Mic v-if="!call.inCall || !s.muted" :size="34" />
          <MicOff v-else :size="34" />
        </div>

        <div class="call-panel__status" aria-live="polite">
          <div class="call-panel__phase">{{ phaseText }}</div>
          <div class="call-panel__timer tabular" :aria-label="t('call.timer')">{{ elapsed }}</div>
        </div>

        <div
          v-if="showVoice"
          class="call-meter"
          role="meter"
          :aria-label="t('call.mic.level')"
          aria-valuemin="0"
          aria-valuemax="100"
          :aria-valuenow="Math.round((live ? s.micLevel : 0) * 100)"
        >
          <i
            v-for="i in SEGMENTS"
            :key="i"
            class="call-meter__seg"
            :class="{
              'call-meter__seg--on': i <= lit,
              'call-meter__seg--hot': i <= lit && i > SEGMENTS - 3,
            }"
          />
        </div>

        <div class="call-panel__actions">
          <Button
            v-if="!call.inCall"
            variant="primary"
            size="lg"
            class="call-btn"
            :disabled="call.busy"
            @click="onCall"
          >
            <Phone :size="18" />
            {{ s.ended ? t('call.actions.callAgain') : t('call.actions.call') }}
          </Button>
          <template v-else>
            <Button variant="danger" size="lg" class="call-btn" @click="call.hangup()">
              <PhoneOff :size="18" />
              {{ t('call.actions.hangup') }}
            </Button>
            <Button
              v-if="s.input === 'voice'"
              :variant="s.muted ? 'danger' : 'secondary'"
              size="lg"
              :aria-pressed="s.muted"
              @click="call.setMuted(!s.muted)"
            >
              <MicOff v-if="!s.muted" :size="16" />
              <Mic v-else :size="16" />
              {{ s.muted ? t('call.actions.unmute') : t('call.actions.mute') }}
            </Button>
          </template>
        </div>

        <div v-if="errorText" class="call-error" role="alert">
          <span>{{ errorText }}</span>
          <Button v-if="micProblem && !call.inCall" variant="outline" size="sm" @click="onTextOnly">
            {{ t('call.actions.textOnly') }}
          </Button>
        </div>
      </div>
    </section>

    <section class="card call-feed-card">
      <div class="card-head">
        <div class="card-title">{{ t('call.transcript.title') }}</div>
      </div>
      <div ref="feed" class="call-feed">
        <p v-if="!s.messages.length" class="call-feed__empty">{{ t('call.transcript.empty') }}</p>
        <CallBubble v-for="m in s.messages" :key="m.id" :message="m" />
      </div>

      <div class="call-examples">
        <span class="call-panel__label">{{ t('call.examples.title') }}</span>
        <div class="call-examples__list">
          <button
            v-for="key in EXAMPLES"
            :key="key"
            type="button"
            class="call-example"
            :disabled="call.busy"
            @click="onExample(key)"
          >
            {{ t(`call.examples.${key}`) }}
          </button>
        </div>
      </div>

      <form class="call-composer" @submit.prevent="onSubmit">
        <label class="sr-only" for="call-text">{{ t('call.composer.label') }}</label>
        <input
          id="call-text"
          v-model="draft"
          class="input"
          type="text"
          autocomplete="off"
          :placeholder="t('call.composer.placeholder')"
          :aria-label="t('call.composer.label')"
        />
        <Button type="submit" variant="primary" :disabled="!draft.trim() || call.busy">
          <Send :size="14" />
          {{ t('call.actions.send') }}
        </Button>
      </form>
    </section>

    <p class="call-hint">
      <Headphones :size="12" aria-hidden="true" />
      {{ t('call.hint') }}
    </p>
  </main>
</template>

<style scoped>
.call-page {
  min-height: 100dvh;
  max-width: 720px;
  margin: 0 auto;
  padding: var(--space-6) var(--space-4) var(--space-8);
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  background: var(--bg);
  color: var(--fg);
}
.call-head {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}
.call-head__title {
  margin: var(--space-2) 0 0;
  font-size: var(--text-3xl);
  font-weight: 700;
  letter-spacing: var(--tracking-tight);
  line-height: var(--leading-tight);
}
.call-head__sub {
  margin: 0;
  color: var(--fg-muted);
  font-size: var(--text-md);
  line-height: var(--leading-normal);
}
.call-panel {
  padding: var(--space-5);
  display: flex;
  flex-direction: column;
  gap: var(--space-5);
}
.call-panel__sites {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex-wrap: wrap;
}
.call-panel__label {
  font-size: var(--text-sm);
  color: var(--fg-subtle);
}
.tab:disabled {
  cursor: default;
  opacity: 0.7;
}
.call-panel__main {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--space-4);
}
.call-orb {
  --ring: 0px;
  display: grid;
  place-items: center;
  width: 96px;
  height: 96px;
  border-radius: 50%;
  background: var(--bg-muted);
  color: var(--fg-subtle);
  box-shadow: 0 0 0 var(--ring) var(--accent-soft);
  transition:
    box-shadow 0.08s linear,
    background 0.2s ease,
    color 0.2s ease;
}
.call-orb--live {
  background: var(--accent);
  color: var(--accent-fg);
}
.call-orb--speaking {
  background: var(--success);
  box-shadow: 0 0 0 var(--ring) var(--success-soft);
}
.call-orb--bot {
  box-shadow: 0 0 0 10px var(--accent-soft);
}
.call-panel__status {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 2px;
}
.call-panel__phase {
  font-size: var(--text-xl);
  font-weight: 600;
}
.call-panel__timer {
  font-size: var(--text-md);
  color: var(--fg-subtle);
  font-variant-numeric: tabular-nums;
}
.call-meter {
  display: flex;
  gap: 3px;
  width: min(320px, 100%);
  height: 10px;
}
.call-meter__seg {
  flex: 1;
  border-radius: 2px;
  background: var(--bg-muted);
  transition: background 0.06s linear;
}
.call-meter__seg--on {
  background: var(--success);
}
.call-meter__seg--hot {
  background: var(--warning);
}
.call-panel__actions {
  display: flex;
  gap: var(--space-3);
  flex-wrap: wrap;
  justify-content: center;
}
.call-btn {
  min-width: 220px;
  height: 48px;
  font-size: var(--text-lg);
  border-radius: 999px;
}
.call-error {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex-wrap: wrap;
  justify-content: center;
  width: 100%;
  padding: var(--space-3) var(--space-4);
  border-radius: var(--radius-lg);
  background: var(--danger-soft);
  color: var(--fg);
  font-size: var(--text-md);
  text-align: center;
}
.call-feed-card {
  display: flex;
  flex-direction: column;
}
.call-feed {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  min-height: 220px;
  max-height: 46dvh;
  overflow-y: auto;
  padding: var(--space-4);
}
.call-feed__empty {
  margin: auto;
  max-width: 360px;
  text-align: center;
  color: var(--fg-subtle);
  font-size: var(--text-md);
}
.call-examples {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  padding: var(--space-3) var(--space-4);
  border-top: 1px solid var(--border);
}
.call-examples__list {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
}
.call-example {
  padding: 6px 12px;
  border: 1px solid var(--border);
  border-radius: 999px;
  background: var(--surface);
  color: var(--fg);
  font-size: var(--text-sm);
  cursor: pointer;
  transition: background 0.12s;
}
.call-example:hover:not(:disabled) {
  background: var(--surface-hover);
}
.call-example:disabled {
  opacity: 0.6;
  cursor: default;
}
.call-composer {
  display: flex;
  gap: var(--space-2);
  padding: var(--space-3) var(--space-4) var(--space-4);
}
.call-composer .input {
  flex: 1;
  min-width: 0;
}
.call-hint {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 4px;
  margin: 0;
  font-size: var(--text-xs);
  color: var(--fg-subtle);
}
.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip: rect(0 0 0 0);
  white-space: nowrap;
}
</style>
