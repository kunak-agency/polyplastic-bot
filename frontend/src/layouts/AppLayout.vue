<script setup lang="ts">
import AppSidebar from '@/components/app/AppSidebar.vue'
import AppTopbar from '@/components/app/AppTopbar.vue'
import { Toaster } from '@/components/ui/sonner'
</script>

<template>
  <div class="app">
    <AppSidebar />
    <div class="main">
      <AppTopbar />
      <slot />
    </div>
    <Toaster position="bottom-right" />
  </div>
</template>
