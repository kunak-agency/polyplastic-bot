<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ExternalLink, Send } from 'lucide-vue-next'
import SectionCard from '@/components/common/SectionCard.vue'
import { Button } from '@/components/ui/button'
import { apiSendDigest } from '@/api/digest'
import { useToast } from '@/composables/useToast'
import type { Digest } from '@/types/api'

// The single MAX message of ux-spec 3.7 exactly as it leaves on Monday 9:00,
// with its link button. "Send now" exists only in `pnpm dev` (the endpoint is
// development-only on the server too).
const props = defineProps<{ digest: Digest }>()
const { t } = useI18n()
const toast = useToast()
const dev = import.meta.env.DEV
const sending = ref(false)

async function send(): Promise<void> {
  sending.value = true
  try {
    const r = await apiSendDigest(props.digest.week)
    if (r.logged) toast.info(t('analytics.max.logged'))
    else toast.success(t('analytics.max.sent', { n: r.sent_to }))
  } catch {
    toast.error(t('analytics.max.sendFailed'))
  } finally {
    sending.value = false
  }
}
</script>

<template>
  <SectionCard>
    <template #title>{{ t('analytics.max.title') }}</template>
    <template #desc>{{ t('analytics.max.desc') }}</template>
    <template v-if="dev" #headActions>
      <Button size="sm" variant="outline" :disabled="sending" @click="send">
        <Send :size="14" />{{ t('analytics.max.send') }}
      </Button>
    </template>
    <div class="bubble">
      <pre class="bubble__text">{{ digest.max_message.text }}</pre>
      <a class="bubble__btn" :href="digest.max_message.link" target="_blank" rel="noopener">
        {{ t('analytics.max.button') }} <ExternalLink :size="12" />
      </a>
    </div>
    <p class="txt-sm muted len">
      {{ t('analytics.max.length', { n: digest.max_message.length }) }}
    </p>
  </SectionCard>
</template>

<style scoped>
.bubble {
  max-width: 520px;
  border-radius: 14px;
  border: 1px solid var(--border);
  background: var(--bg-subtle);
  overflow: hidden;
}
.bubble__text {
  margin: 0;
  padding: var(--space-3) var(--space-4);
  white-space: pre-wrap;
  font-family: inherit;
  font-size: var(--text-sm);
  line-height: 1.5;
}
.bubble__btn {
  display: flex;
  justify-content: center;
  align-items: center;
  gap: 6px;
  padding: 10px;
  border-top: 1px solid var(--border);
  color: var(--fg);
  font-weight: 500;
  font-size: var(--text-sm);
  text-decoration: none;
}
.bubble__btn:hover {
  background: var(--surface-hover);
}
.len {
  margin: 8px 0 0;
}
</style>
