<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Risk } from '@/types/api'

// Colour never stands alone (ux-spec 3.1): the zone is named in text, and the
// reason is in the tooltip, or inline when `detailed`.
const props = withDefaults(
  defineProps<{ risk: Risk; reason?: string | null; detailed?: boolean }>(),
  { reason: null, detailed: false },
)
const { t } = useI18n()

const tip = computed(() =>
  props.reason ? `${t(`newcomers.risk.${props.risk}`)}: ${props.reason}` : undefined,
)
</script>

<template>
  <span :class="['risk', `risk--${risk}`]" :title="detailed ? undefined : tip">
    <span class="risk__dot" aria-hidden="true" />
    <span class="risk__label">{{ t(`newcomers.risk.${risk}`) }}</span>
    <span v-if="detailed && reason" class="risk__reason">— {{ reason }}</span>
  </span>
</template>

<style scoped>
.risk {
  display: inline-flex;
  align-items: baseline;
  gap: 6px;
  font-size: var(--text-sm);
  white-space: nowrap;
}
.risk__dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex: none;
  align-self: center;
}
.risk--green .risk__dot {
  background: var(--success);
}
.risk--yellow .risk__dot {
  background: var(--warning);
}
.risk--red .risk__dot {
  background: var(--danger);
}
.risk--red .risk__label {
  color: var(--danger);
  font-weight: 500;
}
.risk__reason {
  color: var(--fg-muted);
  white-space: normal;
}
</style>
