<script setup lang="ts">
// A lucide mark plus the brand word. The icon is a bundled component, so the
// CSP stays untouched and nothing is fetched.
import { useI18n } from 'vue-i18n'
import { Sprout } from 'lucide-vue-next'

// The rail shows the mark alone: at 56 px there is no room for the word.
withDefaults(defineProps<{ size?: number; wordmark?: boolean }>(), { size: 20, wordmark: true })

// The wordmark is UI copy, so it lives in the catalogue like every other string
// — check:cyrillic enforces that even for a brand name.
const { t } = useI18n()
</script>

<template>
  <span class="app-logo" :style="{ fontSize: `${size}px` }">
    <Sprout class="app-logo__mark" :size="size" aria-hidden="true" />
    <span v-if="wordmark" class="app-logo__name">{{ t('common.brand') }}</span>
  </span>
</template>

<style scoped>
.app-logo {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  line-height: 1;
  white-space: nowrap;
  color: var(--fg);
}
.app-logo__mark {
  display: block;
  flex: none;
}
.app-logo__name {
  font-family: 'Golos Text', Inter, 'Helvetica Neue', system-ui, sans-serif;
  font-weight: 900;
  letter-spacing: -0.4px;
  text-transform: uppercase;
  text-rendering: optimizelegibility;
  -webkit-font-smoothing: antialiased;
}
</style>
