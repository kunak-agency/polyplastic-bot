<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, RouterLink } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { breadcrumbTail } from '@/composables/useBreadcrumbs'
import SiteSwitcher from '@/components/app/SiteSwitcher.vue'
import ApiStatusBadge from '@/components/app/ApiStatusBadge.vue'

const route = useRoute()
const { t } = useI18n()

interface Crumb {
  text: string
  to?: string
}

// A path the user can walk back up — every crumb but the last is a link. The
// fixed part comes from route meta, the optional tail from the mounted screen.
const crumbs = computed<Crumb[]>(() => {
  const fixed = (route.meta.crumbs ?? []).map((c) => ({ text: t(c.key), to: c.to }))
  const tail = breadcrumbTail.value
  return tail ? [...fixed, { text: tail }] : fixed
})
</script>

<template>
  <header class="topbar">
    <nav class="crumbs" :aria-label="t('common.nav.breadcrumbs')">
      <template v-for="(c, i) in crumbs" :key="i">
        <span v-if="i > 0" class="sep">/</span>
        <RouterLink v-if="c.to && i < crumbs.length - 1" class="crumb-link" :to="{ name: c.to }">
          {{ c.text }}
        </RouterLink>
        <span v-else :class="{ cur: i === crumbs.length - 1 }">{{ c.text }}</span>
      </template>
    </nav>

    <div class="topbar-spacer" />

    <!-- Global site filter (ux-spec §3.0); the search box from the spec comes
         with the newcomers and tickets APIs. -->
    <SiteSwitcher />
    <ApiStatusBadge />
  </header>
</template>

<style scoped>
.crumb-link {
  color: var(--fg-muted);
}
.crumb-link:hover {
  color: var(--fg);
  text-decoration: underline;
}
</style>
