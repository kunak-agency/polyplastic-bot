<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { TicketPriority } from '@/types/api'

// The code is what HR says aloud ("a P1"); the meaning is in the tooltip.
defineProps<{ priority: TicketPriority }>()
const { t } = useI18n()
</script>

<template>
  <span :class="['prio', `prio--${priority}`]" :title="t(`tickets.priority.${priority}`)">
    {{ priority }}
  </span>
</template>

<style scoped>
.prio {
  display: inline-flex;
  align-items: center;
  height: 18px;
  padding: 0 5px;
  border-radius: var(--radius-xs);
  font-size: var(--text-xs);
  font-weight: 600;
  font-family: var(--font-mono);
  border: 1px solid currentColor;
}
.prio--P0 {
  color: var(--priority-p0);
}
.prio--P1 {
  color: var(--priority-p1);
}
.prio--P2 {
  color: var(--fg-subtle);
}
</style>
