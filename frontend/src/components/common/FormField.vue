<script setup lang="ts">
withDefaults(
  defineProps<{
    label?: string
    hint?: string
    error?: string
    htmlFor?: string
    required?: boolean
  }>(),
  {
    label: undefined,
    hint: undefined,
    error: undefined,
    htmlFor: undefined,
    required: false,
  },
)
</script>

<template>
  <div :class="['field', { 'field--err': !!error }]">
    <slot name="label">
      <label v-if="label" class="field-label" :for="htmlFor">
        {{ label }}<span v-if="required" class="required">*</span>
      </label>
    </slot>
    <slot />
    <span v-if="error" class="field-err">{{ error }}</span>
    <span v-else-if="hint" class="field-hint">{{ hint }}</span>
  </div>
</template>
