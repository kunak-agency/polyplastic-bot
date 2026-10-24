<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink } from 'vue-router'
import { useI18n } from 'vue-i18n'
import type { TodayActionId, TodayRow } from '@/types/api'
import { TODAY_ACTIONS, sortToday } from '@/lib/dashboard'
import { dayLabel } from '@/lib/overview'

// "Today": up to 10 rows, a red/yellow mark, the reason in words and the
// buttons from TODAY_ACTIONS. API actions (remind, take) go to the page.
const props = defineProps<{ rows: TodayRow[]; busy?: string | null }>()
const emit = defineEmits<{ call: [row: TodayRow, call: 'remind' | 'take'] }>()
const { t } = useI18n()

const list = computed(() => sortToday(props.rows))

// The "no recruiter" row stands for several newcomers: one caption with the
// count ("No recruiter — 5 newcomers") instead of a placeholder name.
function aggregate(row: TodayRow): number | null {
  if (row.kind !== 'no_recruiter' || row.newcomer_name) return null
  const n = row.count ?? row.reason.match(/\d+/)?.[0]
  return n === undefined || n === null ? null : Number(n)
}

// The server already names the recruiter in some reasons: do not repeat it.
function showRecruiter(row: TodayRow): boolean {
  return !!row.recruiter_name && !row.reason.includes(row.recruiter_name)
}

function buttons(row: TodayRow) {
  return row.actions
    .map((id: TodayActionId) => {
      const cfg = TODAY_ACTIONS[id]
      const target = cfg?.target(row)
      return cfg && target ? { id, cfg, target } : null
    })
    .filter((b) => b !== null)
}
</script>

<template>
  <section class="today card" :aria-label="t('dashboard.today.title')">
    <header class="today__head">
      <h2 class="today__title">{{ t('dashboard.today.title') }}</h2>
      <span class="muted txt-sm tabular">{{ list.length }}</span>
      <div class="today__spacer" />
      <slot name="aside" />
    </header>
    <slot v-if="list.length === 0" name="empty">
      <p class="muted txt-sm today__none">{{ t('dashboard.today.empty') }}</p>
    </slot>
    <ul v-else class="today__list">
      <li v-for="r in list" :key="r.id" class="today__row">
        <span
          :class="['today__mark', `today__mark--${r.zone}`]"
          :title="t(`dashboard.zone.${r.zone}`)"
        >
          <span class="sr-only">{{ t(`dashboard.zone.${r.zone}`) }}</span>
        </span>
        <div v-if="aggregate(r) !== null" class="today__text">
          <span class="fw-medium">{{
            t('dashboard.noRecruiterRow', { n: aggregate(r) }, aggregate(r) ?? 0)
          }}</span>
        </div>
        <div v-else class="today__text">
          <span class="fw-medium">{{
            r.newcomer_name ?? r.theme ?? t(`dashboard.kind.${r.kind}`)
          }}</span>
          <span v-if="r.day !== null" class="muted txt-sm tabular">{{ dayLabel(r.day) }}</span>
          <span class="today__reason txt-sm">{{ r.reason }}</span>
          <span v-if="showRecruiter(r)" class="muted txt-sm">· {{ r.recruiter_name }}</span>
        </div>
        <div class="today__actions">
          <template v-for="b in buttons(r)" :key="b.id">
            <RouterLink
              v-if="b.target.kind === 'go'"
              :class="['btn btn-sm', b.cfg.primary ? 'btn-primary' : 'btn-outline']"
              :to="b.target.to"
            >
              {{ t(b.cfg.labelKey) }}
            </RouterLink>
            <button
              v-else
              type="button"
              :class="['btn btn-sm', b.cfg.primary ? 'btn-primary' : 'btn-outline']"
              :disabled="busy === r.id"
              @click="emit('call', r, b.target.call)"
            >
              {{ t(b.cfg.labelKey) }}
            </button>
          </template>
        </div>
      </li>
    </ul>
  </section>
</template>

<style scoped>
.today {
  padding: var(--space-3) var(--space-4);
}
.today__head {
  display: flex;
  align-items: baseline;
  gap: var(--space-2);
  margin-bottom: var(--space-2);
}
.today__spacer {
  flex: 1;
}
.today__title {
  margin: 0;
  font-size: var(--text-md);
  font-weight: 600;
}
.today__none {
  margin: 0;
}
.today__list {
  list-style: none;
  margin: 0;
  padding: 0;
}
.today__row {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: 8px 0;
  border-top: 1px solid var(--border-subtle);
}
.today__row:first-child {
  border-top: none;
}
.today__mark {
  width: 10px;
  height: 10px;
  border-radius: 50%;
  flex-shrink: 0;
}
.today__mark--red {
  background: var(--danger);
}
.today__mark--yellow {
  background: var(--warning);
}
.today__text {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-wrap: wrap;
  gap: 2px 10px;
  align-items: baseline;
}
.today__reason {
  color: var(--fg-muted);
}
.today__actions {
  display: flex;
  gap: 6px;
  flex-shrink: 0;
}
</style>
