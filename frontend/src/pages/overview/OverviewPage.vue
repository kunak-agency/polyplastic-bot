<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { LayoutDashboard } from 'lucide-vue-next'
import PageContainer from '@/components/common/PageContainer.vue'
import PageHead from '@/components/common/PageHead.vue'
import SectionCard from '@/components/common/SectionCard.vue'
import EmptyState from '@/components/common/EmptyState.vue'

const { t } = useI18n()
</script>

<template>
  <PageContainer>
    <PageHead :title="t('overview.title')">
      <template #subtitle>{{ t('overview.subtitle') }}</template></PageHead
    >
    <SectionCard>
      <EmptyState
        :icon="LayoutDashboard"
        :title="t('overview.empty.title')"
        :subtitle="t('overview.empty.description')"
      />
    </SectionCard>
  </PageContainer>
</template>
