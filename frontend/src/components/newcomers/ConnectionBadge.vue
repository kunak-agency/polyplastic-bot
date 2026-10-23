<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { Badge } from '@/components/ui/badge'
import type { Connection } from '@/types/api'

defineProps<{ connection: Connection }>()
const { t } = useI18n()

const VARIANT = { invited: 'default', connected: 'success', stopped: 'warning' } as const
</script>

<template>
  <Badge :variant="VARIANT[connection]">{{ t(`newcomers.connection.${connection}`) }}</Badge>
</template>
