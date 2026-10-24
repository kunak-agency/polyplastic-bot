<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Sparkles } from 'lucide-vue-next'
import type { ScenarioStep } from '@/types/api'

// "AI, check it" on a step whose texts rewrite-vy generated. Shown while the
// step text still equals what the generator wrote (an edit by hand hides it)
// or when the rewrite touched branch and survey replies of the step.
const props = defineProps<{ step: ScenarioStep }>()
const { t } = useI18n()

const mark = computed(() => props.step.ai_rewrite ?? null)
const nested = computed(() => (mark.value?.fields ?? []).filter((f) => f.includes('.')))
const shown = computed(() => {
  const m = mark.value
  if (!m) return false
  const vy = !!m.text_vy && m.text_vy === props.step.text_vy
  const ty = !!m.text_ty && m.text_ty === props.step.text_ty
  return vy || ty || nested.value.length > 0
})
const rule = computed(() => mark.value?.source === 'rule')
</script>

<template>
  <span
    v-if="shown"
    :class="['badge', rule ? 'solid-warning' : 'solid-info', 'ai-badge']"
    :title="t('rewrite.badgeTitle', { fields: (mark?.fields ?? []).join(', ') })"
  >
    <Sparkles :size="12" />
    {{ rule ? t('rewrite.badgeRule') : t('rewrite.badge') }}
  </span>
</template>

<style scoped>
.ai-badge {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  align-self: flex-start;
  margin-bottom: 4px;
}
</style>
