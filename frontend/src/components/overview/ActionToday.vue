<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ArrowRight, CircleCheck } from 'lucide-vue-next'
import SectionCard from '@/components/common/SectionCard.vue'
import type { Overview } from '@/types/api'
import { actionTarget } from '@/lib/overview'

// "Need action today" (ux-spec 3.1): only non-zero items, in the server's
// order of urgency; each one opens the list it is about.
const props = defineProps<{ items: Overview['action_today'] }>()
const emit = defineEmits<{ overdue: [] }>()
const { t } = useI18n()

const rows = computed(() => props.items.map((a) => ({ ...a, target: actionTarget(a.kind) })))
</script>

<template>
  <SectionCard :title="t('overview.actions.title')">
    <p v-if="rows.length === 0" class="none muted">
      <CircleCheck :size="16" aria-hidden="true" />{{ t('overview.actions.none') }}
    </p>
    <ul v-else class="actions">
      <li v-for="a in rows" :key="a.kind">
        <RouterLink
          v-if="a.target.kind === 'route'"
          class="action"
          :to="{ name: a.target.name, query: a.target.query }"
        >
          <span>{{ t(`overview.actions.${a.kind}`, a.count) }}</span>
          <ArrowRight :size="14" aria-hidden="true" />
        </RouterLink>
        <button v-else type="button" class="action" @click="emit('overdue')">
          <span>{{ t(`overview.actions.${a.kind}`, a.count) }}</span>
          <ArrowRight :size="14" aria-hidden="true" />
        </button>
      </li>
    </ul>
  </SectionCard>
</template>

<style scoped>
.none {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 0;
}
.actions {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
}
.action {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 6px 12px;
  border: 1px solid var(--border);
  border-radius: var(--radius-full);
  background: var(--surface);
  color: var(--fg);
  font: inherit;
  font-size: var(--text-base);
  text-decoration: none;
  cursor: pointer;
}
.action:hover {
  border-color: var(--border-strong);
  background: var(--surface-hover);
}
</style>
