<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { BookOpen } from 'lucide-vue-next'
import PageContainer from '@/components/common/PageContainer.vue'
import PageHead from '@/components/common/PageHead.vue'
import SectionCard from '@/components/common/SectionCard.vue'
import EmptyState from '@/components/common/EmptyState.vue'

const { t } = useI18n()
</script>

<template>
  <PageContainer>
    <PageHead :title="t('knowledge.title')">
      <template #subtitle>{{ t('knowledge.subtitle') }}</template></PageHead
    >
    <SectionCard>
      <EmptyState
        :icon="BookOpen"
        :title="t('knowledge.empty.title')"
        :subtitle="t('knowledge.empty.description')"
      />
    </SectionCard>
  </PageContainer>
</template>
