<script setup lang="ts">
withDefaults(
  defineProps<{
    rows?: number
    variant?: 'card' | 'list' | 'table'
  }>(),
  { rows: 3, variant: 'list' },
)
</script>

<template>
  <div class="loading-state" :class="`loading-state--${variant}`">
    <div v-for="i in rows" :key="i" class="loading-state__row" />
  </div>
</template>

<style scoped>
.loading-state {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 12px;
}
.loading-state__row {
  height: 14px;
  border-radius: 6px;
  background: linear-gradient(
    90deg,
    var(--bg-muted) 0%,
    var(--bg-strong) 50%,
    var(--bg-muted) 100%
  );
  background-size: 200% 100%;
  animation: shimmer 1.4s ease-in-out infinite;
}
.loading-state--card .loading-state__row {
  height: 80px;
}
.loading-state--table .loading-state__row {
  height: 36px;
}
@keyframes shimmer {
  0% {
    background-position: 200% 0;
  }
  100% {
    background-position: -200% 0;
  }
}
</style>
