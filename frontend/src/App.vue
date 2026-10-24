<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, RouterView } from 'vue-router'
import AppLayout from '@/layouts/AppLayout.vue'
import { useAppTitle } from '@/composables/useAppTitle'
import { useAuthStore } from '@/stores/auth'

const route = useRoute()
// Nothing renders until /auth/me answered: the shell would otherwise fire its
// own requests (ticket badge) before the guard decided where to go.
const auth = useAuthStore()
const ready = computed(() => auth.status !== 'unknown')

// The page transition belongs between sections, not between tabs of one section:
// the key is the section root, so switching a child tab of a nested section swaps only the panel
// under the strip instead of remounting the head, the tabs and the focus in them.
const transitionKey = computed(() => route.matched[0]?.path ?? route.path)

useAppTitle()
</script>

<template>
  <!-- The login screen has no sidebar and no topbar. -->
  <template v-if="!ready" />
  <RouterView v-else-if="route.meta.public" />
  <AppLayout v-else>
    <RouterView v-slot="{ Component }">
      <Transition name="page-fade" mode="out-in">
        <component :is="Component" :key="transitionKey" />
      </Transition>
    </RouterView>
  </AppLayout>
</template>
