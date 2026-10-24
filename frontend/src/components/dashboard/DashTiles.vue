<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import type { DashTile } from '@/types/api'
import { tileOf, tileText, type TileConfig } from '@/lib/dashboard'

// Tiles from a config (lib/dashboard.ts) and the server's tile map. A tile
// with ids toggles the filter of the list below; one with `to` links away.
// Zeros are grey, `red` comes from the server's thresholds.
const props = defineProps<{
  config: readonly TileConfig[]
  tiles: Record<string, DashTile>
  active?: string | null
}>()
const emit = defineEmits<{ pick: [id: string | null] }>()
const { t } = useI18n()

function cls(cfg: TileConfig) {
  const v = tileOf(props.tiles, cfg)
  return [
    'tile',
    {
      'tile--zero': v.value === 0 && !v.secondary,
      'tile--red': v.red,
      'tile--on': props.active === cfg.id,
    },
  ]
}
function filters(cfg: TileConfig): boolean {
  return !!props.tiles[cfg.id]?.ids
}
</script>

<template>
  <section class="tiles" :aria-label="t('dashboard.tilesLabel')">
    <template v-for="cfg in config" :key="cfg.id">
      <button
        v-if="filters(cfg) || !cfg.to"
        type="button"
        :class="cls(cfg)"
        :disabled="!filters(cfg)"
        :aria-pressed="active === cfg.id"
        @click="emit('pick', active === cfg.id ? null : cfg.id)"
      >
        <span class="tile__label">{{ t(cfg.labelKey) }}</span>
        <span class="tile__value">{{ tileText(tileOf(tiles, cfg), cfg) }}</span>
        <span v-if="cfg.hintKey" class="tile__hint">{{ t(cfg.hintKey) }}</span>
      </button>
      <RouterLink v-else :class="cls(cfg)" :to="cfg.to">
        <span class="tile__label">{{ t(cfg.labelKey) }}</span>
        <span class="tile__value">{{ tileText(tileOf(tiles, cfg), cfg) }}</span>
        <span v-if="cfg.hintKey" class="tile__hint">{{ t(cfg.hintKey) }}</span>
      </RouterLink>
    </template>
  </section>
</template>

<style scoped>
.tiles {
  display: grid;
  grid-template-columns: repeat(6, minmax(0, 1fr));
  gap: var(--space-3);
}
@media (max-width: 1100px) {
  .tiles {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }
}
.tile {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: var(--space-3) var(--space-4);
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  background: var(--surface);
  color: var(--fg);
  text-align: left;
  text-decoration: none;
  font: inherit;
  cursor: pointer;
}
.tile:disabled {
  cursor: default;
}
.tile:hover:not(:disabled) {
  border-color: var(--border-strong);
  box-shadow: var(--shadow-sm);
}
.tile--on {
  border-color: var(--fg);
  box-shadow: 0 0 0 1px var(--fg);
}
.tile__label {
  font-size: var(--text-sm);
  color: var(--fg-muted);
}
.tile__value {
  font-size: var(--text-3xl);
  font-weight: 600;
  line-height: var(--leading-tight);
  font-variant-numeric: tabular-nums;
}
.tile--zero .tile__value {
  color: var(--fg-faint);
}
.tile--red {
  border-color: color-mix(in oklab, var(--danger) 45%, var(--border));
}
.tile--red .tile__value {
  color: var(--danger);
}
.tile__hint {
  font-size: var(--text-xs);
  color: var(--fg-subtle);
}
</style>
