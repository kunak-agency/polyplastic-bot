<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { ChartColumn } from 'lucide-vue-next'
import PageContainer from '@/components/common/PageContainer.vue'
import PageHead from '@/components/common/PageHead.vue'
import SectionCard from '@/components/common/SectionCard.vue'
import EmptyState from '@/components/common/EmptyState.vue'

const { t } = useI18n()
</script>

<template>
  <PageContainer>
    <PageHead :title="t('analytics.title')">
      <template #subtitle>{{ t('analytics.subtitle') }}</template></PageHead
    >
    <SectionCard>
      <EmptyState
        :icon="ChartColumn"
        :title="t('analytics.empty.title')"
        :subtitle="t('analytics.empty.description')"
      />
    </SectionCard>
  </PageContainer>
</template>
