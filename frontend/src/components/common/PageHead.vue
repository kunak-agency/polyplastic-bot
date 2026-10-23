<script setup lang="ts">
defineProps<{ title?: string }>()
</script>

<template>
  <div class="page-head">
    <div>
      <h1 class="page-title">
        <slot name="title">{{ title }}</slot>
      </h1>
      <p v-if="$slots.subtitle" class="page-subtitle">
        <slot name="subtitle" />
      </p>
    </div>
    <div v-if="$slots.actions" class="row gap-2">
      <slot name="actions" />
    </div>
  </div>
</template>
