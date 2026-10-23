<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink } from 'vue-router'
import { useI18n } from 'vue-i18n'
import type { Overview } from '@/types/api'
import { TILE_TARGETS, stageTarget, type DrillTarget } from '@/lib/overview'

// Tiles of ux-spec 3.1: each one drills into the registry or the queue with a
// preset filter (Enboarder pattern); "Overdue" filters the risk list here.
const props = defineProps<{ tiles: Overview['tiles']; stages: Overview['stages'] }>()
const emit = defineEmits<{ overdue: [] }>()
const { t } = useI18n()

function to(target: DrillTarget) {
  return target.kind === 'route' ? { name: target.name, query: target.query } : undefined
}

const slaTo = { name: 'tickets', query: { view: 'overdue' } }
const pulse = computed(() => props.tiles.pulse_response)
</script>

<template>
  <section class="tiles" :aria-label="t('overview.tiles.label')">
    <div class="tile tile--wide">
      <RouterLink class="tile__main" :to="to(TILE_TARGETS.inAdaptation)!">
        <span class="tile__label">{{ t('overview.tiles.inAdaptation') }}</span>
        <span class="tile__value">{{ tiles.in_adaptation }}</span>
      </RouterLink>
      <span class="tile__stages">
        <template v-for="(s, i) in stages" :key="s.stage">
          <span v-if="i > 0" class="tile__sep" aria-hidden="true">·</span>
          <RouterLink class="tile__stage" :to="to(stageTarget(s.stage))!">
            {{ t(`overview.stages.${s.stage}`) }} <b>{{ s.count }}</b>
          </RouterLink>
        </template>
      </span>
    </div>

    <RouterLink class="tile" :to="to(TILE_TARGETS.startingThisWeek)!">
      <span class="tile__label">{{ t('overview.tiles.startingThisWeek') }}</span>
      <span class="tile__value">{{ tiles.starting_this_week }}</span>
    </RouterLink>

    <button
      type="button"
      :class="['tile', { 'tile--warn': tiles.overdue > 0 }]"
      :title="t('overview.tiles.filterRisks')"
      @click="emit('overdue')"
    >
      <span class="tile__label">{{ t('overview.tiles.overdue') }}</span>
      <span class="tile__value">{{ tiles.overdue }}</span>
      <span class="tile__hint">{{ t('overview.tiles.overdueHint') }}</span>
    </button>

    <div class="tile">
      <RouterLink class="tile__main" :to="to(TILE_TARGETS.tickets)!">
        <span class="tile__label">{{ t('overview.tiles.tickets') }}</span>
        <span class="tile__value">{{ tiles.tickets_open }}</span>
      </RouterLink>
      <RouterLink
        v-if="tiles.tickets_sla_overdue > 0"
        class="tile__hint tile__hint--danger"
        :to="slaTo"
      >
        {{ t('overview.tiles.ticketsSla', { n: tiles.tickets_sla_overdue }) }}
      </RouterLink>
      <span v-else class="tile__hint">{{ t('overview.tiles.ticketsSlaNone') }}</span>
    </div>

    <div class="tile tile--static">
      <span class="tile__label">{{ t('overview.tiles.pulse') }}</span>
      <span class="tile__value">
        <template v-if="pulse.percent !== null">{{ pulse.percent }}%</template>
        <template v-else>—</template>
      </span>
      <span class="tile__hint">
        {{
          pulse.sent > 0
            ? t('overview.tiles.pulseHint', {
                answered: pulse.answered,
                sent: pulse.sent,
                days: pulse.period_days,
              })
            : t('overview.tiles.pulseNone', { days: pulse.period_days })
        }}
      </span>
    </div>
  </section>
</template>

<style scoped>
.tiles {
  display: grid;
  grid-template-columns: 2fr repeat(4, minmax(0, 1fr));
  gap: var(--space-3);
}
@media (max-width: 1100px) {
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
  text-align: left;
  text-decoration: none;
  font: inherit;
  transition:
    border-color 0.12s,
    box-shadow 0.12s;
}
button.tile {
  cursor: pointer;
}
.tile:hover {
  border-color: var(--border-strong);
  box-shadow: var(--shadow-sm);
}
.tile__main {
  display: flex;
  flex-direction: column;
  gap: 4px;
  color: inherit;
  text-decoration: none;
}
.tile__main:hover .tile__value {
  text-decoration: underline;
  text-decoration-thickness: 1px;
  text-underline-offset: 4px;
}
.tile--static {
  cursor: default;
}
.tile--static:hover {
  border-color: var(--border);
  box-shadow: none;
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
.tile--warn .tile__value {
  color: var(--priority-p1);
}
.tile__hint {
  font-size: var(--text-xs);
  color: var(--fg-subtle);
}
.tile__hint--danger {
  color: var(--danger);
  font-weight: 500;
  text-decoration: none;
}
.tile__hint--danger:hover {
  text-decoration: underline;
}
.tile__stages {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: 4px 6px;
  font-size: var(--text-sm);
  color: var(--fg-muted);
}
.tile__sep {
  color: var(--fg-faint);
}
.tile__stage {
  color: inherit;
  text-decoration: none;
  white-space: nowrap;
}
.tile__stage b {
  color: var(--fg);
  font-weight: 600;
}
.tile__stage:hover {
  color: var(--fg);
  text-decoration: underline;
}
</style>
