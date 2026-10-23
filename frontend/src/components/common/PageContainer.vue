<script setup lang="ts">
withDefaults(defineProps<{ width?: 'default' | 'narrow' | 'wide' }>(), { width: 'default' })
</script>

<template>
  <div :class="['page', width === 'narrow' && 'page-narrow', width === 'wide' && 'page-wide']">
    <slot />
  </div>
</template>
