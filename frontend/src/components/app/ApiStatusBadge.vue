<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { storeToRefs } from 'pinia'
import { useI18n } from 'vue-i18n'
import { useDocumentVisibility, useIntervalFn } from '@vueuse/core'
import { useHealthStore } from '@/stores/health'

// Probe period. The badge is a hint for the demo, not monitoring: 30 s is often
// enough to notice a restarted backend and rare enough not to spam its log.
const POLL_MS = 30_000

const { t } = useI18n()
const health = useHealthStore()
const { status, last } = storeToRefs(health)

// A hidden tab does not poll; it re-checks as soon as it is shown again.
const visibility = useDocumentVisibility()
useIntervalFn(() => {
  if (visibility.value === 'visible') void health.check()
}, POLL_MS)
onMounted(() => void health.check())

function componentLabel(value: string | undefined): string {
  return value === 'ok' || value === 'down'
    ? t(`common.apiStatus.component.${value}`)
    : t('common.apiStatus.component.unknown')
}

const details = computed(() =>
  last.value
    ? t('common.apiStatus.details', {
        db: componentLabel(last.value.db),
        rag: componentLabel(last.value.rag),
      })
    : status.value === 'down'
      ? t('common.apiStatus.noAnswer')
      : undefined,
)
</script>

<template>
  <span
    :class="['api-status', `api-status--${status}`]"
    role="status"
    :aria-label="t('common.apiStatus.label')"
    :title="details"
  >
    <span class="api-status__dot" aria-hidden="true" />
    {{ t(`common.apiStatus.${status}`) }}
  </span>
</template>

<style scoped>
.api-status {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 3px 10px;
  border-radius: 999px;
  border: 1px solid var(--border);
  font-size: var(--text-xs);
  color: var(--fg-muted);
  white-space: nowrap;
}
.api-status__dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--fg-faint);
}
.api-status--ok .api-status__dot {
  background: var(--success);
}
.api-status--degraded {
  background: var(--warning-soft);
}
.api-status--degraded .api-status__dot {
  background: var(--warning);
}
.api-status--down {
  background: var(--danger-soft);
  color: var(--fg);
}
.api-status--down .api-status__dot {
  background: var(--danger);
}
</style>
