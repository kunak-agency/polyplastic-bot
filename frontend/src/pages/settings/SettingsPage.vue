<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { Settings } from 'lucide-vue-next'
import PageContainer from '@/components/common/PageContainer.vue'
import PageHead from '@/components/common/PageHead.vue'
import SectionCard from '@/components/common/SectionCard.vue'
import EmptyState from '@/components/common/EmptyState.vue'

const { t } = useI18n()
</script>

<template>
  <PageContainer>
    <PageHead :title="t('settings.title')">
      <template #subtitle>{{ t('settings.subtitle') }}</template></PageHead
    >
    <SectionCard>
      <EmptyState
        :icon="Settings"
        :title="t('settings.empty.title')"
        :subtitle="t('settings.empty.description')"
      />
    </SectionCard>
  </PageContainer>
</template>
