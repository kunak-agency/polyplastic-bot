<script setup lang="ts">
import type { Component } from 'vue'
import { Inbox } from 'lucide-vue-next'

withDefaults(
  defineProps<{
    icon?: Component
    title: string
    subtitle?: string
  }>(),
  { icon: () => Inbox, subtitle: undefined },
)
</script>

<template>
  <div class="empty-state">
    <component :is="icon" v-if="icon" :size="24" class="empty-state__icon" />
    <div class="empty-state__title">{{ title }}</div>
    <div v-if="subtitle" class="empty-state__subtitle">{{ subtitle }}</div>
    <div v-if="$slots.action" class="empty-state__action">
      <slot name="action" />
    </div>
  </div>
</template>

<style scoped>
.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 32px 24px;
  color: var(--fg-muted);
  text-align: center;
}
.empty-state__icon {
  color: var(--fg-faint);
  margin-bottom: 4px;
}
.empty-state__title {
  font-weight: 500;
  color: var(--fg);
}
.empty-state__subtitle {
  font-size: var(--text-sm);
  color: var(--fg-subtle);
}
.empty-state__action {
  margin-top: 8px;
}
</style>
