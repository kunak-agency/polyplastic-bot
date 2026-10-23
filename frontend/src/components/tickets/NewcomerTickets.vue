<script setup lang="ts">
import { ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { useI18n } from 'vue-i18n'
import SectionCard from '@/components/common/SectionCard.vue'
import ErrorState from '@/components/common/ErrorState.vue'
import LoadingState from '@/components/common/LoadingState.vue'
import TicketSla from '@/components/tickets/TicketSla.vue'
import TicketStatus from '@/components/tickets/TicketStatus.vue'
import TicketPriority from '@/components/tickets/TicketPriority.vue'
import { Button } from '@/components/ui/button'
import { apiListTickets } from '@/api/tickets'
import { useNow } from '@/composables/useNow'
import { useTicketFormat } from '@/composables/useTicketFormat'
import { errorMessage } from '@/lib/apiError'
import { formatDayOffset } from '@/lib/calendar'
import { ticketsOfNewcomer } from '@/lib/tickets'
import type { SiteCode, Ticket } from '@/types/api'

// "Tickets" block of the trajectory (ux-spec 3.3): GET /tickets?newcomer_id=
// (contract v2 clarifications); the list is sorted here, the latest first.
const props = defineProps<{ newcomerId: string; site: SiteCode }>()
const { t } = useI18n()
const now = useNow()
const { category } = useTicketFormat()

const items = ref<Ticket[] | null>(null)
const error = ref<unknown>(null)

async function load(): Promise<void> {
  error.value = null
  try {
    const r = await apiListTickets({ newcomer_id: props.newcomerId })
    items.value = ticketsOfNewcomer(r.items, props.newcomerId)
  } catch (e) {
    error.value = e
  }
}
watch(() => props.newcomerId, load, { immediate: true })
</script>

<template>
  <SectionCard>
    <template #title>
      {{ t('tickets.newcomer.title') }}
      <span v-if="items" class="muted tabular">({{ items.length }})</span>
    </template>
    <template #headActions>
      <RouterLink :to="{ name: 'tickets' }" class="muted txt-sm nt-all">{{
        t('tickets.newcomer.all')
      }}</RouterLink>
    </template>
    <LoadingState v-if="!items && !error" :rows="2" />
    <ErrorState v-else-if="error" :message="errorMessage(error, 'tickets.newcomer.loadFailed')">
      <template #retry>
        <Button size="sm" @click="load">{{ t('common.actions.retry') }}</Button>
      </template>
    </ErrorState>
    <p v-else-if="items && items.length === 0" class="muted txt-sm nt-empty">
      {{ t('tickets.newcomer.empty') }}
    </p>
    <ul v-else-if="items" class="nt">
      <li v-for="tk in items" :key="tk.id" :class="['nt__row', `nt__row--${tk.priority}`]">
        <RouterLink :to="{ name: 'ticket', params: { id: tk.id } }" class="nt__link">
          <span class="tabular fw-semi">#{{ tk.number }}</span>
          {{ category(tk) }}
        </RouterLink>
        <span class="muted txt-sm tabular">{{ formatDayOffset(tk.day) }}</span>
        <TicketPriority :priority="tk.priority" />
        <TicketStatus :status="tk.status" />
        <span class="grow" />
        <TicketSla :ticket="tk" :now="now" />
      </li>
    </ul>
  </SectionCard>
</template>

<style scoped>
.nt {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.nt__row {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  padding: 6px 10px;
  border: 1px solid var(--border);
  border-left: 3px solid var(--priority-p2);
  border-radius: var(--radius-sm);
}
.nt__row--P0 {
  border-left-color: var(--priority-p0);
}
.nt__row--P1 {
  border-left-color: var(--priority-p1);
}
.nt__link {
  color: var(--fg);
  display: inline-flex;
  gap: 6px;
}
.nt__link:hover {
  text-decoration: underline;
}
.nt-all:hover {
  color: var(--fg);
  text-decoration: underline;
}
.nt-empty {
  margin: 0;
}
</style>
