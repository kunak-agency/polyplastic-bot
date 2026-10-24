<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Indicators } from '@/types/api'

// Topics of the period against the previous one, in words (research
// 2026-10-24): "growing", "falling", "new", "as before". Only topics with at
// least min_group people on one side; the server hides the rest.
const props = defineProps<{ trends: Indicators['topics']['trends']; minGroup: number }>()
const { t } = useI18n()

const visible = computed(() => props.trends.filter((x) => x.trend !== null))
</script>

<template>
  <div class="trends">
    <div class="trends__title txt-sm">{{ t('overview.topics.trendsTitle') }}</div>
    <p v-if="visible.length === 0" class="txt-sm muted">
      {{ t('overview.topics.none', { min: minGroup }) }}
    </p>
    <ul v-else class="trends__list">
      <li v-for="x in visible" :key="x.category" class="trend">
        <span class="trend__name">{{ x.category }} · {{ x.name }}</span>
        <span class="trend__n">
          {{
            x.count !== null
              ? t('overview.topics.people', x.count)
              : t('overview.topics.hidden', { min: minGroup })
          }}
        </span>
        <span :class="['trend__word', `trend__word--${x.trend}`]">
          {{ t(`overview.topics.trend.${x.trend}`) }}
        </span>
      </li>
    </ul>
  </div>
</template>

<style scoped>
.trends {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}
.trends__title {
  font-weight: 500;
  color: var(--fg-muted);
}
.trends__list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
  gap: 4px var(--space-4);
}
.trend {
  display: flex;
  align-items: baseline;
  gap: 8px;
  font-size: var(--text-sm);
  min-width: 0;
}
.trend__name {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.trend__n {
  color: var(--fg-subtle);
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}
.trend__word {
  font-weight: 500;
  white-space: nowrap;
}
.trend__word--up,
.trend__word--new {
  color: var(--priority-p1);
}
.trend__word--down {
  color: var(--success);
}
.trend__word--same {
  color: var(--fg-muted);
}
</style>
