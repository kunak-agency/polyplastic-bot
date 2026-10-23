<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Badge } from '@/components/ui/badge'
import type { BadgeVariants } from '@/components/ui/badge'
import type { TicketStatus } from '@/types/api'

const props = defineProps<{ status: TicketStatus }>()
const { t } = useI18n()

const VARIANT: Record<TicketStatus, BadgeVariants['variant']> = {
  open: 'warning',
  assigned: 'info',
  human_active: 'accent',
  resolved: 'success',
}
const variant = computed(() => VARIANT[props.status])
</script>

<template>
  <Badge :variant="variant">{{ t(`tickets.status.${status}`) }}</Badge>
</template>
