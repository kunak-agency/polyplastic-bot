<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { storeToRefs } from 'pinia'
import { useI18n } from 'vue-i18n'
import { RefreshCw } from 'lucide-vue-next'
import PageContainer from '@/components/common/PageContainer.vue'
import PageHead from '@/components/common/PageHead.vue'
import SectionCard from '@/components/common/SectionCard.vue'
import ErrorState from '@/components/common/ErrorState.vue'
import LoadingState from '@/components/common/LoadingState.vue'
import RecruiterBoard from '@/components/dashboard/RecruiterBoard.vue'
import HrLeadBoard from '@/components/dashboard/HrLeadBoard.vue'
import { Button } from '@/components/ui/button'
import { apiGetHrLeadDashboard, apiGetRecruiterDashboard } from '@/api/team'
import { apiListNewcomers } from '@/api/newcomers'
import { useSiteStore } from '@/stores/site'
import { useAuthStore } from '@/stores/auth'
import { errorMessage } from '@/lib/apiError'
import type { HrLeadDashboard, Newcomer, RecruiterDashboard } from '@/types/api'

// The first screen of the day, by role (research 2026-10-24-role-dashboards):
// hr_lead gets the team view, a recruiter his own newcomers.
const { t } = useI18n()
const site = useSiteStore()
const { current: currentSite } = storeToRefs(site)
const auth = useAuthStore()
const lead = computed(() => auth.viewer === 'hr_lead')

const recruiter = ref<RecruiterDashboard | null>(null)
const hr = ref<HrLeadDashboard | null>(null)
const mine = ref<Newcomer[]>([])
const loading = ref(false)
const error = ref<unknown>(null)
const data = computed(() => (lead.value ? hr.value : recruiter.value))

async function load(): Promise<void> {
  loading.value = true
  error.value = null
  try {
    if (lead.value) {
      hr.value = await apiGetHrLeadDashboard(site.apiSite())
    } else {
      const [dash, list] = await Promise.all([
        apiGetRecruiterDashboard(site.apiSite()),
        apiListNewcomers({ site: site.apiSite(), mine: true }),
      ])
      recruiter.value = dash
      mine.value = list.items
    }
  } catch (e) {
    error.value = e
  } finally {
    loading.value = false
  }
}
onMounted(load)
watch(currentSite, load)
</script>

<template>
  <PageContainer width="wide">
    <PageHead :title="t('dashboard.title')">
      <template #subtitle>
        {{ auth.user ? t(lead ? 'dashboard.subtitleLead' : 'dashboard.subtitle') : '' }}
      </template>
      <template #actions>
        <Button variant="outline" size="sm" :disabled="loading" @click="load">
          <RefreshCw :size="14" aria-hidden="true" />{{ t('dashboard.refresh') }}
        </Button>
      </template>
    </PageHead>

    <SectionCard v-if="!data && !error">
      <LoadingState variant="card" :rows="4" />
    </SectionCard>
    <SectionCard v-else-if="!data">
      <ErrorState :message="errorMessage(error, 'dashboard.loadError')">
        <template #retry>
          <Button size="sm" @click="load">{{ t('common.actions.retry') }}</Button>
        </template>
      </ErrorState>
    </SectionCard>
    <div v-else :class="{ stale: loading }">
      <HrLeadBoard v-if="lead && hr" :data="hr" />
      <RecruiterBoard v-else-if="recruiter" :data="recruiter" :mine="mine" @reload="load" />
    </div>
  </PageContainer>
</template>

<style scoped>
.stale {
  opacity: 0.6;
  transition: opacity 0.15s;
}
</style>
