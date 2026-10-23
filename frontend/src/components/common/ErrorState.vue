<script setup lang="ts">
import type { Component } from 'vue'
import { TriangleAlert } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

withDefaults(
  defineProps<{
    icon?: Component
    title?: string
    message?: string
  }>(),
  { icon: () => TriangleAlert, title: undefined, message: undefined },
)
</script>

<template>
  <div class="error-state">
    <component :is="icon" :size="24" class="error-state__icon" />
    <div class="error-state__title">{{ title ?? t('common.errorState.defaultTitle') }}</div>
    <div v-if="message" class="error-state__message">{{ message }}</div>
    <div v-if="$slots.retry" class="error-state__retry">
      <slot name="retry" />
    </div>
  </div>
</template>

<style scoped>
.error-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 6px;
  padding: 24px;
  text-align: center;
}
.error-state__icon {
  color: var(--danger);
}
.error-state__title {
  font-weight: 600;
  color: var(--fg);
}
.error-state__message {
  font-size: var(--text-sm);
  color: var(--fg-muted);
}
.error-state__retry {
  margin-top: 8px;
}
</style>
