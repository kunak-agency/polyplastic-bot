<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import SectionCard from '@/components/common/SectionCard.vue'
import type { Indicators, SiteCellIndicator, SiteCode } from '@/types/api'
import { smallSites, unitValue } from '@/lib/indicators'

// Sites against the company average, never against each other (research
// 2026-10-24): fixed row and column order, no sorting, no "best/worst". The
// verdict in words comes from the server ("within fluctuation" for n < 30 and
// under 5 p.p.); a deviation of 10 p.p. and more links to the site's blockers.
const props = defineProps<{ data: Indicators; current: SiteCode | null }>()
const emit = defineEmits<{ blocker: [site: SiteCode] }>()
const { t } = useI18n()

const sites = computed(() => props.data.sites.sites)
const small = computed(() => smallSites(props.data))
const metricRows = computed(() => props.data.sites.rows.filter((r) => r.key !== 'cohort'))
const cohortRow = computed(() => props.data.sites.rows.find((r) => r.key === 'cohort'))

function shown(v: number, unit: string): string {
  const s = unitValue(v, unit)
  return unit === 'pct' ? `${s} %` : s
}

function verdict(c: SiteCellIndicator, unit: string): string {
  if (!c.verdict || c.verdict === 'hidden') return ''
  if (c.verdict === 'above' || c.verdict === 'below') {
    const d = Math.abs(c.diff ?? 0)
    const amount =
      unit === 'pct' ? t('overview.sites.pp', { n: Math.round(d) }) : unitValue(d, unit)
    return t(`overview.sites.verdict.${c.verdict}`, { d: amount })
  }
  return t(`overview.sites.verdict.${c.verdict}`)
}

function cellOf(row: Indicators['sites']['rows'][number], site: SiteCode): SiteCellIndicator {
  return (
    row.cells.find((c) => c.site === site) ?? {
      site,
      value: null,
      n: 0,
      hidden: true,
      diff: null,
      diff_pp: null,
      verdict: 'hidden',
      flag: false,
    }
  )
}
</script>

<template>
  <SectionCard
    :title="t('overview.sites.title')"
    :desc="t('overview.sites.desc')"
    body-padding="none"
  >
    <div class="wrap-x">
      <table class="tbl sites">
        <thead>
          <tr>
            <th>{{ t('overview.sites.metric') }}</th>
            <th v-for="s in sites" :key="s" :class="{ 'col--current': current === s }" scope="col">
              {{ t(`common.site.${s}`) }}
            </th>
            <th scope="col">{{ t('overview.sites.avg') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="cohortRow">
            <th scope="row">{{ t('overview.sites.rows.cohort') }}</th>
            <td v-for="s in sites" :key="s" :class="{ 'col--current': current === s }">
              <span class="v">{{ cellOf(cohortRow, s).value ?? 0 }}</span>
              <span v-if="small.has(s)" class="sub">
                {{
                  t(
                    'overview.sites.small',
                    {
                      n: cellOf(cohortRow, s).value ?? 0,
                      min: data.min_group,
                    },
                    cellOf(cohortRow, s).value ?? 0,
                  )
                }}
              </span>
            </td>
            <td>
              <span class="v">{{ cohortRow.avg ?? 0 }}</span>
            </td>
          </tr>
          <tr v-for="row in metricRows" :key="row.key">
            <th scope="row">{{ t(`overview.sites.rows.${row.key}`) }}</th>
            <td
              v-for="s in sites"
              :key="s"
              :class="{ 'col--current': current === s, 'cell--flag': cellOf(row, s).flag }"
            >
              <template v-if="cellOf(row, s).hidden || cellOf(row, s).value === null">
                <span class="v v--hidden">{{ t('overview.sites.hidden') }}</span>
              </template>
              <template v-else>
                <span class="v">{{ shown(cellOf(row, s).value!, row.unit) }}</span>
                <span class="sub">{{ t('overview.sites.n', { n: cellOf(row, s).n }) }}</span>
                <span v-if="verdict(cellOf(row, s), row.unit)" class="sub sub--verdict">
                  {{ verdict(cellOf(row, s), row.unit) }}
                </span>
              </template>
            </td>
            <td>
              <template v-if="row.avg !== null">
                <span class="v">{{ shown(row.avg, row.unit) }}</span>
                <span class="sub">{{ t('overview.sites.n', { n: row.avg_n }) }}</span>
              </template>
              <span v-else class="v v--hidden">{{ t('overview.sites.noAvg') }}</span>
            </td>
          </tr>
          <tr v-if="data.sites.flagged.length > 0" class="blockers">
            <th scope="row" />
            <td v-for="s in sites" :key="s">
              <button
                v-if="data.sites.flagged.includes(s)"
                type="button"
                class="blocker"
                @click="emit('blocker', s)"
              >
                {{ t('overview.sites.blocker', { site: t(`common.site.${s}`) }) }}
              </button>
            </td>
            <td />
          </tr>
        </tbody>
      </table>
    </div>
  </SectionCard>
</template>

<style scoped>
.wrap-x {
  overflow-x: auto;
}
.sites th[scope='row'] {
  font-weight: 500;
  color: var(--fg);
  white-space: normal;
  min-width: 180px;
  background: transparent;
  font-size: var(--text-sm);
}
.sites td {
  vertical-align: top;
  min-width: 140px;
}
.sites tr:hover td {
  background: transparent;
}
.v {
  display: block;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}
.v--hidden {
  font-weight: 400;
  color: var(--fg-faint);
}
.sub {
  display: block;
  font-size: var(--text-xs);
  color: var(--fg-subtle);
}
.sub--verdict {
  color: var(--fg-muted);
}
.cell--flag .sub--verdict {
  color: var(--priority-p1);
  font-weight: 500;
}
.col--current {
  background: var(--bg-subtle);
}
.blockers td {
  padding-top: 6px;
  padding-bottom: 10px;
}
.blocker {
  border: 0;
  padding: 0;
  background: none;
  font: inherit;
  font-size: var(--text-xs);
  color: var(--accent);
  text-decoration: underline;
  text-underline-offset: 3px;
  cursor: pointer;
}
</style>
