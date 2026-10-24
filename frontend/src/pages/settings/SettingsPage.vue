<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Settings } from 'lucide-vue-next'
import PageContainer from '@/components/common/PageContainer.vue'
import PageHead from '@/components/common/PageHead.vue'
import SectionCard from '@/components/common/SectionCard.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import UsersPanel from '@/components/team/UsersPanel.vue'
import { useAuthStore } from '@/stores/auth'

const { t } = useI18n()
// Users and roles — for the HR lead with auth on (api_contract_auth.md).
const auth = useAuthStore()
const showUsers = computed(() => auth.viewer === 'hr_lead')
</script>

<template>
  <PageContainer :width="showUsers ? 'wide' : 'default'">
    <PageHead :title="t('settings.title')">
      <template #subtitle>{{ t('settings.subtitle') }}</template></PageHead
    >
    <UsersPanel v-if="showUsers" />
    <SectionCard v-else>
      <EmptyState
        :icon="Settings"
        :title="t('settings.empty.title')"
        :subtitle="t('settings.empty.description')"
      />
    </SectionCard>
  </PageContainer>
</template>
