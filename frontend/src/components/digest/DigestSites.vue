<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import SectionCard from '@/components/common/SectionCard.vue'
import type { Digest } from '@/types/api'

// Section 6 "by site": code-built table; W is hidden below min_group respondents.
defineProps<{ digest: Digest }>()
const { t } = useI18n()
</script>

<template>
  <SectionCard :title="t('analytics.sites.title')" body-padding="none">
    <div class="table-wrap">
      <table class="tbl">
        <thead>
          <tr>
            <th>{{ t('analytics.sites.site') }}</th>
            <th class="num">{{ t('analytics.sites.onPulse') }}</th>
            <th class="num">{{ t('analytics.sites.answered') }}</th>
            <th class="num">{{ t('analytics.sites.w') }}</th>
            <th class="num">{{ t('analytics.sites.problems') }}</th>
            <th>{{ t('analytics.sites.top') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="s in digest.sites" :key="s.site">
            <td>{{ t(`common.site.${s.site}`) }}</td>
            <td class="num">{{ s.on_pulse }}</td>
            <td class="num">{{ s.answered }}</td>
            <td class="num">
              <span v-if="s.w_avg !== null">{{ s.w_avg }}</span>
              <span v-else class="muted">{{ t('analytics.sites.hidden') }}</span>
            </td>
            <td class="num">{{ s.problems }}</td>
            <td class="top">{{ s.top_theme ?? '—' }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </SectionCard>
</template>

<style scoped>
.num {
  text-align: right;
  font-variant-numeric: tabular-nums;
}
.top::first-letter {
  text-transform: uppercase;
}
</style>
