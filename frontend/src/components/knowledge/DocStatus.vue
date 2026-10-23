<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Badge } from '@/components/ui/badge'
import type { BadgeVariants } from '@/components/ui/badge'
import type { KnowledgeDocStatus } from '@/types/api'

const props = defineProps<{ status: KnowledgeDocStatus }>()
const { t } = useI18n()

const VARIANT: Record<KnowledgeDocStatus, BadgeVariants['variant']> = {
  ready: 'success',
  needs_review: 'warning',
  not_indexed: 'danger',
}
const variant = computed(() => VARIANT[props.status])
</script>

<template>
  <Badge :variant="variant" :title="t(`knowledge.statusHint.${status}`)">
    <span class="dot" aria-hidden="true" />{{ t(`knowledge.status.${status}`) }}
  </Badge>
</template>
