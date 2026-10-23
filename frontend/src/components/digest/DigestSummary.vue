<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Sparkles, FileText } from 'lucide-vue-next'
import SectionCard from '@/components/common/SectionCard.vue'
import type { Digest } from '@/types/api'
import { signed, totalsDelta } from '@/lib/digest'

// Section 1 of the digest (research block C): the connected text the LLM wrote
// over aggregates, next to the tiles the code counted. The source line says
// which one produced the text.
const props = defineProps<{ digest: Digest }>()
const { t } = useI18n()

const tot = computed(() => props.digest.totals)
const dl = computed(() => totalsDelta(props.digest.totals, props.digest.previous))
const hasPrev = computed(() => props.digest.previous.on_pulse > 0)
</script>

<template>
  <SectionCard :title="t('analytics.summary.title')">
    <p class="summary-text">{{ digest.text.summary }}</p>
    <p class="source txt-sm muted">
      <component :is="digest.text.source === 'llm' ? Sparkles : FileText" :size="13" />
      {{ t(`analytics.summary.${digest.text.source}`) }}
    </p>
    <div class="tiles">
      <div class="tile">
        <span class="tile__label">{{ t('analytics.summary.onPulse') }}</span>
        <span class="tile__value">{{ tot.on_pulse }}</span>
        <span class="tile__hint">{{ t('analytics.summary.onPulseHint') }}</span>
      </div>
      <div class="tile">
        <span class="tile__label">{{ t('analytics.summary.answered') }}</span>
        <span class="tile__value"
          >{{ tot.response_rate_pct ?? '—'
          }}<small v-if="tot.response_rate_pct !== null">%</small></span
        >
        <span class="tile__hint">
          {{ t('analytics.summary.answeredHint', { n: tot.answered, total: tot.on_pulse }) }}
          <template v-if="hasPrev && dl.response_rate_pct !== null">
            · {{ t('analytics.summary.vsPrev', { delta: signed(dl.response_rate_pct) }) }}
          </template>
        </span>
      </div>
      <div class="tile">
        <span class="tile__label">{{ t('analytics.summary.w') }}</span>
        <span class="tile__value">{{ tot.w_avg ?? '—' }}</span>
        <span class="tile__hint">
          <template v-if="tot.w_avg === null">{{
            t('analytics.summary.wHidden', { n: digest.min_group })
          }}</template>
          <template v-else-if="dl.w_avg !== null">{{
            t('analytics.summary.vsPrev', { delta: signed(dl.w_avg) })
          }}</template>
          <template v-else>{{ t('analytics.summary.wHint') }}</template>
        </span>
      </div>
      <div class="tile">
        <span class="tile__label">{{ t('analytics.summary.problems') }}</span>
        <span class="tile__value">{{ tot.problems }}</span>
        <span class="tile__hint">
          {{ t('analytics.summary.problemsHint', { n: tot.critical }) }}
          <template v-if="hasPrev">
            · {{ t('analytics.summary.vsPrev', { delta: signed(dl.problems) }) }}
          </template>
        </span>
      </div>
    </div>
  </SectionCard>
</template>

<style scoped>
.summary-text {
  margin: 0;
  font-size: var(--text-lg);
  line-height: 1.5;
  max-width: 72ch;
}
.source {
  display: flex;
  align-items: center;
  gap: 6px;
  margin: 8px 0 var(--space-4);
}
.tiles {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: var(--space-3);
}
@media (max-width: 900px) {
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
.tile__value small {
  font-size: var(--text-lg);
  margin-left: 2px;
}
.tile__hint {
  font-size: var(--text-xs);
  color: var(--fg-subtle);
}
</style>
