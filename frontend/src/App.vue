<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, RouterView } from 'vue-router'
import AppLayout from '@/layouts/AppLayout.vue'
import { useAppTitle } from '@/composables/useAppTitle'

const route = useRoute()

// The page transition belongs between sections, not between tabs of one section:
// the key is the section root, so switching a child tab of a nested section swaps only the panel
// under the strip instead of remounting the head, the tabs and the focus in them.
const transitionKey = computed(() => route.matched[0]?.path ?? route.path)

useAppTitle()
</script>

<template>
  <AppLayout>
    <RouterView v-slot="{ Component }">
      <Transition name="page-fade" mode="out-in">
        <component :is="Component" :key="transitionKey" />
      </Transition>
    </RouterView>
  </AppLayout>
</template>
