<script setup lang="ts">
import { storeToRefs } from 'pinia'
import { useI18n } from 'vue-i18n'
import { ToggleGroupItem, ToggleGroupRoot, type AcceptableValue } from 'reka-ui'
import { SITE_FILTERS, useSiteStore, type SiteFilter } from '@/stores/site'

const { t } = useI18n()
const site = useSiteStore()
const { current } = storeToRefs(site)

// A single-mode toggle group lets the active item be pressed off, which would
// leave no site selected; the filter always has a value, so that is ignored.
function onChange(value: AcceptableValue | AcceptableValue[]): void {
  if (typeof value === 'string' && (SITE_FILTERS as readonly string[]).includes(value)) {
    site.setSite(value as SiteFilter)
  }
}
</script>

<template>
  <ToggleGroupRoot
    type="single"
    class="site-switch"
    :model-value="current"
    :aria-label="t('common.site.label')"
    @update:model-value="onChange"
  >
    <ToggleGroupItem v-for="s in SITE_FILTERS" :key="s" :value="s" class="site-switch__item">
      {{ t(`common.site.${s}`) }}
    </ToggleGroupItem>
  </ToggleGroupRoot>
</template>

<style scoped>
.site-switch {
  display: inline-flex;
  gap: 2px;
  padding: 2px;
  border: 1px solid var(--border);
  border-radius: var(--radius-md, 8px);
  background: var(--bg-subtle);
}
.site-switch__item {
  padding: 4px 10px;
  border-radius: calc(var(--radius-md, 8px) - 2px);
  font-size: var(--text-sm);
  color: var(--fg-muted);
  background: transparent;
  border: 0;
  cursor: pointer;
  white-space: nowrap;
}
.site-switch__item:hover {
  color: var(--fg);
}
.site-switch__item[data-state='on'] {
  background: var(--bg);
  color: var(--fg);
  font-weight: 500;
  box-shadow: 0 1px 2px rgb(0 0 0 / 0.06);
}
.site-switch__item:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 1px;
}
</style>
