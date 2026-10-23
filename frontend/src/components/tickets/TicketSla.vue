<script setup lang="ts">
import { computed } from 'vue'
import { Clock } from 'lucide-vue-next'
import { slaState } from '@/lib/tickets'
import { useTicketFormat } from '@/composables/useTicketFormat'
import type { Ticket } from '@/types/api'

// SLA timer: counts down to the server's deadline; overdue is red and says so
// in words, the last hour is amber.
const props = defineProps<{ ticket: Pick<Ticket, 'status' | 'sla_due_at'>; now: number }>()
const { slaText, slaTitle } = useTicketFormat()
const kind = computed(() => slaState(props.ticket, props.now).kind)
</script>

<template>
  <span :class="['sla', `sla--${kind}`]" :title="slaTitle(ticket)">
    <Clock :size="12" aria-hidden="true" />
    <span class="tabular">{{ slaText(ticket, now) }}</span>
  </span>
</template>

<style scoped>
.sla {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: var(--text-sm);
  color: var(--fg-muted);
  white-space: nowrap;
}
.sla--soon {
  color: var(--warning);
  font-weight: 500;
}
.sla--overdue {
  color: var(--danger);
  font-weight: 600;
}
.sla--none {
  color: var(--fg-subtle);
}
</style>
