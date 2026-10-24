<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import {
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuPortal,
  DropdownMenuRoot,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from 'reka-ui'
import { Ellipsis, FileDown, PencilLine } from 'lucide-vue-next'
import { useScenarioExport } from '@/composables/useScenarioExport'
import type { Scenario } from '@/types/api'

// Row menu of the scenario list: open, download the draft as Excel and, once
// published, the latest published version (api_contract.md v4, export).
const props = defineProps<{ scenario: Scenario }>()

const { t } = useI18n()
const router = useRouter()
const { exporting, download } = useScenarioExport()
</script>

<template>
  <DropdownMenuRoot :modal="false">
    <DropdownMenuTrigger
      class="row-menu__trigger"
      :aria-label="t('scenarios.rowMenu.label', { name: props.scenario.name })"
    >
      <Ellipsis :size="16" />
    </DropdownMenuTrigger>
    <DropdownMenuPortal>
      <DropdownMenuContent class="row-menu" align="end" :side-offset="4">
        <DropdownMenuItem
          class="row-menu__item"
          @select="router.push({ name: 'scenario', params: { id: props.scenario.id } })"
        >
          <PencilLine :size="14" />{{ t('scenarios.rowMenu.open') }}
        </DropdownMenuItem>
        <DropdownMenuSeparator class="row-menu__sep" />
        <DropdownMenuItem
          class="row-menu__item"
          :disabled="exporting"
          @select="download(props.scenario)"
        >
          <FileDown :size="14" />{{ t('scenarios.export.menuDraft') }}
        </DropdownMenuItem>
        <DropdownMenuItem
          v-if="props.scenario.version > 0"
          class="row-menu__item"
          :disabled="exporting"
          @select="download(props.scenario, { version: props.scenario.version })"
        >
          <FileDown :size="14" />{{
            t('scenarios.export.menuVersion', { version: props.scenario.version })
          }}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenuPortal>
  </DropdownMenuRoot>
</template>

<style scoped>
.row-menu__trigger {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border: 0;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--fg-muted);
  cursor: pointer;
}
.row-menu__trigger:hover,
.row-menu__trigger[data-state='open'] {
  background: var(--surface-hover);
  color: var(--fg);
}
.row-menu__trigger:focus-visible {
  outline: 2px solid var(--ring);
  outline-offset: 1px;
}
</style>

<style>
/* Portalled out of the component, so not scoped. */
.row-menu {
  z-index: 50;
  min-width: 220px;
  padding: 4px;
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--bg-elevated);
  box-shadow: var(--shadow-popover);
}
.row-menu__item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 8px;
  border-radius: var(--radius-sm);
  font-size: var(--text-sm);
  color: var(--fg);
  cursor: pointer;
  outline: none;
  user-select: none;
}
.row-menu__item[data-highlighted] {
  background: var(--surface-hover);
}
.row-menu__item[data-disabled] {
  color: var(--fg-faint);
  cursor: default;
}
.row-menu__sep {
  height: 1px;
  margin: 4px 0;
  background: var(--border-subtle);
}
</style>
