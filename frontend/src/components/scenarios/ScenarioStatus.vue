<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { Badge } from '@/components/ui/badge'
import type { Scenario } from '@/types/api'

// Contract v1 has only draft|published plus the version number: a draft with a
// version above zero is read as "edited after publishing vN".
defineProps<{ scenario: Pick<Scenario, 'status' | 'version'> }>()
const { t } = useI18n()
</script>

<template>
  <Badge v-if="scenario.status === 'published'" variant="success">
    {{ t('scenarios.status.published', { version: scenario.version }) }}
  </Badge>
  <Badge v-else-if="scenario.version > 0" variant="warning">
    {{ t('scenarios.status.draftOf', { version: scenario.version }) }}
  </Badge>
  <Badge v-else>{{ t('scenarios.status.draft') }}</Badge>
</template>
