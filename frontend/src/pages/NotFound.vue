<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import { FileQuestion } from 'lucide-vue-next'
import PageContainer from '@/components/common/PageContainer.vue'
import SectionCard from '@/components/common/SectionCard.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import { Button } from '@/components/ui/button'

const { t } = useI18n()
</script>

<template>
  <PageContainer width="narrow">
    <SectionCard>
      <EmptyState
        :icon="FileQuestion"
        :title="t('common.notFound.title')"
        :subtitle="t('common.notFound.description')"
      >
        <template #action>
          <RouterLink to="/">
            <Button variant="primary">{{ t('common.notFound.toHome') }}</Button>
          </RouterLink>
        </template>
      </EmptyState>
    </SectionCard>
  </PageContainer>
</template>
