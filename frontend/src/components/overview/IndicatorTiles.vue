<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import type { IndicatorTile } from '@/types/api'
import { splitMinutes, tileTarget, vsPrev } from '@/lib/indicators'

// Six outcome tiles of the period (research 2026-10-24 §KPI): the number, its
// n, the comparison with the previous period in words. Below min_group —
// "too little data"; outcomes with no data yet say when they will appear; a
// number from the synthetic demo flow is marked "demo".
const props = defineProps<{ tiles: IndicatorTile[]; minGroup: number }>()
const { t } = useI18n()

function value(tile: IndicatorTile): string {
  if (tile.value === null) return '—'
  if (tile.unit === 'min') {
    const { h, m } = splitMinutes(tile.value)
    return h > 0 ? t('overview.tiles.hours', { h, m }) : t('overview.tiles.minutes', { m })
  }
  return t('overview.tiles.percent', { v: tile.value })
}

function note(tile: IndicatorTile): string {
  switch (tile.status) {
    case 'later_90':
      return t('overview.tiles.later90')
    case 'no_marker':
      return t('overview.tiles.noMarker')
    case 'few':
      return tile.n === 0 ? t('overview.tiles.none') : t('overview.tiles.few')
  }
  return t(`overview.tiles.hints.${tile.key}`, { num: tile.num, n: tile.n })
}

function compare(tile: IndicatorTile): string {
  if (tile.status === 'few' && tile.n > 0) {
    return t('overview.tiles.fewN', { n: tile.n, min: props.minGroup })
  }
  const v = vsPrev(tile)
  return v ? t(`overview.tiles.vsPrev.${v.key}`, { d: v.d }) : ''
}

function to(tile: IndicatorTile) {
  const target = tileTarget(tile.key)
  return target ? { name: target.name, query: target.query } : null
}
</script>

<template>
  <section class="tiles" :aria-label="t('overview.tiles.label')">
    <component
      :is="to(tile) ? RouterLink : 'div'"
      v-for="tile in tiles"
      :key="tile.key"
      :to="to(tile) ?? undefined"
      :class="['tile', { 'tile--link': !!to(tile), 'tile--empty': tile.value === null }]"
    >
      <span class="tile__head">
        <span class="tile__label">{{ t(`overview.tiles.names.${tile.key}`) }}</span>
        <span v-if="tile.demo" class="badge solid-warning" :title="t('overview.tiles.demoHint')">
          {{ t('overview.tiles.demo') }}
        </span>
      </span>
      <span class="tile__value">{{ value(tile) }}</span>
      <span class="tile__hint">{{ note(tile) }}</span>
      <span
        v-if="tile.key === 'first_response' && tile.sla_n !== undefined && tile.value !== null"
        class="tile__hint"
      >
        {{
          tile.sla_pct !== null && tile.sla_pct !== undefined
            ? t('overview.tiles.sla', { pct: tile.sla_pct, n: tile.sla_n })
            : t('overview.tiles.slaFew')
        }}
      </span>
      <span v-if="compare(tile)" class="tile__cmp">{{ compare(tile) }}</span>
    </component>
  </section>
</template>

<style scoped>
.tiles {
  display: grid;
  grid-template-columns: repeat(6, minmax(0, 1fr));
  gap: var(--space-3);
}
@media (max-width: 1280px) {
  .tiles {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }
}
@media (max-width: 720px) {
  .tiles {
    grid-template-columns: repeat(2, minmax(0, 1fr));
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
  text-decoration: none;
  transition:
    border-color 0.12s,
    box-shadow 0.12s;
}
.tile--link:hover {
  border-color: var(--border-strong);
  box-shadow: var(--shadow-sm);
}
.tile--link:hover .tile__value {
  text-decoration: underline;
  text-decoration-thickness: 1px;
  text-underline-offset: 4px;
}
.tile__head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 6px;
}
.tile__label {
  font-size: var(--text-sm);
  color: var(--fg-muted);
}
.tile__value {
  font-size: var(--text-3xl);
  font-weight: 600;
  letter-spacing: var(--tracking-tight);
  line-height: var(--leading-tight);
  font-variant-numeric: tabular-nums;
}
.tile--empty .tile__value {
  color: var(--fg-faint);
}
.tile__hint {
  font-size: var(--text-xs);
  color: var(--fg-subtle);
}
.tile__cmp {
  font-size: var(--text-xs);
  color: var(--fg-muted);
  margin-top: auto;
  padding-top: 4px;
}
</style>
