<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { EyeOff, Lock } from 'lucide-vue-next'
import TicketSla from '@/components/tickets/TicketSla.vue'
import TicketStatus from '@/components/tickets/TicketStatus.vue'
import TicketPriority from '@/components/tickets/TicketPriority.vue'
import { useTicketFormat } from '@/composables/useTicketFormat'
import { formatDayOffset } from '@/lib/calendar'
import { initials, isRestricted } from '@/lib/tickets'
import type { Ticket } from '@/types/api'

// One ticket of the queue (ux-spec 3.4): coloured priority edge, category,
// newcomer · site · D+N, assignee, SLA timer. `tile` is the kanban card; the
// list shows the row form with the status and the quote.
const props = withDefaults(defineProps<{ ticket: Ticket; now: number; tile?: boolean }>(), {
  tile: false,
})
const { t } = useI18n()
const { category } = useTicketFormat()
const restricted = computed(() => isRestricted(props.ticket))
</script>

<template>
  <RouterLink
    :to="{ name: 'ticket', params: { id: ticket.id } }"
    :class="['tcard', `tcard--${ticket.priority}`, { 'tcard--tile': tile }]"
    :aria-label="t('tickets.card.open', { number: ticket.number })"
  >
    <div class="tcard__top">
      <span class="tcard__num tabular">#{{ ticket.number }}</span>
      <TicketPriority :priority="ticket.priority" />
      <span class="tcard__cat" :title="t(`tickets.categoryFull.${ticket.category}`)">
        {{ category(ticket) }}
      </span>
      <span v-if="restricted" class="tcard__flag" :title="t('tickets.detail.restricted.banner')">
        <Lock :size="12" aria-hidden="true" />{{ t('tickets.card.restricted') }}
      </span>
      <span v-if="ticket.anonymous" class="tcard__flag" :title="t('tickets.card.anonymousHint')">
        <EyeOff :size="12" aria-hidden="true" />{{ t('tickets.card.anonymous') }}
      </span>
      <span v-if="!tile" class="tcard__spacer" />
      <TicketStatus v-if="!tile" :status="ticket.status" />
    </div>

    <div class="tcard__who">
      {{ ticket.newcomer_name }} · {{ t(`common.site.${ticket.site}`) }} ·
      <span class="tabular">{{ formatDayOffset(ticket.day) }}</span>
    </div>
    <p v-if="!tile" class="tcard__summary">{{ ticket.summary }}</p>

    <div class="tcard__foot">
      <span
        v-if="ticket.assignee_name"
        class="tcard__assignee"
        :title="t('tickets.card.assignee', { name: ticket.assignee_name })"
      >
        <span class="tcard__avatar" aria-hidden="true">{{ initials(ticket.assignee_name) }}</span>
        <span :class="{ 'sr-only': tile }">{{ ticket.assignee_name }}</span>
      </span>
      <span v-else class="tcard__unassigned">{{ t('tickets.card.unassigned') }}</span>
      <span class="tcard__spacer" />
      <TicketSla v-if="tile || ticket.status !== 'resolved'" :ticket="ticket" :now="now" />
    </div>
  </RouterLink>
</template>

<style scoped>
.tcard {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 10px 12px 10px 14px;
  border: 1px solid var(--border);
  border-left: 4px solid var(--priority-p2);
  border-radius: var(--radius-md);
  background: var(--surface);
  color: var(--fg);
  text-decoration: none;
  transition:
    border-color 0.12s,
    box-shadow 0.12s;
}
.tcard:hover {
  border-color: var(--border-strong);
  box-shadow: var(--shadow-sm);
}
.tcard--P0 {
  border-left-color: var(--priority-p0);
}
.tcard--P1 {
  border-left-color: var(--priority-p1);
}
.tcard--P2 {
  border-left-color: var(--priority-p2);
}
.tcard:hover.tcard--P0 {
  border-left-color: var(--priority-p0);
}
.tcard:hover.tcard--P1 {
  border-left-color: var(--priority-p1);
}
.tcard:hover.tcard--P2 {
  border-left-color: var(--priority-p2);
}
.tcard__top {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
}
.tcard__num {
  font-weight: 600;
  font-size: var(--text-sm);
  color: var(--fg-muted);
}
.tcard__cat {
  font-weight: 500;
}
.tcard__flag {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  font-size: var(--text-xs);
  color: var(--fg-muted);
  padding: 0 5px;
  border-radius: var(--radius-xs);
  background: var(--bg-muted);
}
.tcard__spacer {
  flex: 1;
}
.tcard__who {
  font-size: var(--text-sm);
  color: var(--fg-muted);
}
.tcard__summary {
  margin: 0;
  font-size: var(--text-sm);
  color: var(--fg-subtle);
  display: -webkit-box;
  -webkit-line-clamp: 2;
  line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.tcard__foot {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 4px 8px;
}
.tcard__assignee {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: var(--text-sm);
  color: var(--fg-muted);
}
.tcard__avatar {
  display: inline-grid;
  place-items: center;
  width: 22px;
  height: 22px;
  border-radius: 50%;
  background: var(--bg-strong);
  color: var(--fg);
  font-size: 10px;
  font-weight: 600;
}
.tcard__unassigned {
  white-space: nowrap;
  font-size: var(--text-sm);
  color: var(--warning);
  font-weight: 500;
}
.tcard--tile .tcard__cat {
  font-size: var(--text-sm);
}
</style>
