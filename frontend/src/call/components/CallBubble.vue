<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { BookOpen } from 'lucide-vue-next'
import type { CallMessage } from '../protocol'

// One line of the conversation. The look follows hack-ru-clinik
// components/call/TranscriptBubble.vue; the regulation chips are new.
const props = defineProps<{ message: CallMessage }>()
const { t } = useI18n()

const isUser = computed(() => props.message.role === 'user')
const sources = computed(() => props.message.sources ?? [])
</script>

<template>
  <div class="bubble-row" :class="isUser ? 'bubble-row--user' : 'bubble-row--bot'">
    <div class="bubble-meta">
      {{ isUser ? t('call.transcript.you') : t('call.transcript.bot') }}
      <span v-if="message.partial">· {{ t('call.transcript.recognizing') }}</span>
      <span v-if="message.pending">· {{ t('call.transcript.sending') }}</span>
      <span v-if="message.filler">· {{ t('call.transcript.filler') }}</span>
    </div>
    <div
      class="bubble"
      :class="[
        isUser ? 'bubble--user' : 'bubble--bot',
        {
          'bubble--partial': message.partial,
          'bubble--pending': message.pending,
          'bubble--filler': message.filler,
        },
      ]"
    >
      <span>{{ message.text }}</span>
      <span v-if="message.streaming" class="bubble__caret" aria-hidden="true" />
    </div>
    <ul v-if="sources.length" class="bubble-sources" :aria-label="t('call.transcript.sources')">
      <li
        v-for="(s, i) in sources"
        :key="`${s.doc}-${s.clause_id}-${i}`"
        class="bubble-source"
        :title="s.doc"
      >
        <BookOpen :size="12" aria-hidden="true" />
        {{ s.label }}
      </li>
    </ul>
  </div>
</template>

<style scoped>
.bubble-row {
  display: flex;
  flex-direction: column;
  gap: 4px;
  max-width: 82%;
}
.bubble-row--user {
  align-self: flex-end;
  align-items: flex-end;
}
.bubble-row--bot {
  align-self: flex-start;
  align-items: flex-start;
}
.bubble-meta {
  font-size: var(--text-xs);
  color: var(--fg-subtle);
  padding: 0 4px;
}
.bubble {
  padding: 10px 14px;
  border-radius: var(--radius-xl);
  font-size: var(--text-md);
  line-height: var(--leading-normal);
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.bubble--user {
  background: var(--accent);
  color: var(--accent-fg);
  border-bottom-right-radius: var(--radius-sm);
}
.bubble--bot {
  background: var(--bg-muted);
  color: var(--fg);
  border-bottom-left-radius: var(--radius-sm);
}
/* Interim recognition: grey until the final replaces it. */
.bubble--partial {
  background: var(--bg-muted);
  color: var(--fg-subtle);
  font-style: italic;
}
.bubble--pending {
  opacity: 0.6;
}
.bubble--filler {
  opacity: 0.65;
  font-style: italic;
}
.bubble__caret {
  display: inline-block;
  width: 7px;
  height: 1em;
  margin-left: 2px;
  vertical-align: -2px;
  background: currentColor;
  animation: caretBlink 0.9s steps(2) infinite;
}
@keyframes caretBlink {
  50% {
    opacity: 0;
  }
}
.bubble-sources {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin: 0;
  padding: 0;
  list-style: none;
}
.bubble-source {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 3px 8px;
  border: 1px solid var(--border);
  border-radius: 999px;
  background: var(--surface);
  color: var(--fg-muted);
  font-size: var(--text-xs);
  font-weight: 500;
}
</style>
