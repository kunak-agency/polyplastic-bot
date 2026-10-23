<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { PanelLeftClose, PanelLeftOpen } from 'lucide-vue-next'
import { storeToRefs } from 'pinia'
import { NAV, NAV_FOOTER, type NavItem } from '@/router/nav'
import AppLogo from '@/components/common/AppLogo.vue'
import { useTicketsStore } from '@/stores/tickets'
import { useSiteStore } from '@/stores/site'

// At rest the sidebar is a 56 px icon rail with the names in the tooltips;
// expanded it names every section in a panel laid OVER the page, so the content
// never loses width to it. Any navigation, a click outside or Escape folds it
// back.
const route = useRoute()
const { t } = useI18n()
const collapsed = ref(true)

// Badge of open (not resolved) tickets, for the site picked in the topbar —
// the same list the queue shows. Refreshed every minute and on site change;
// ticket actions refresh it themselves.
const tickets = useTicketsStore()
const { openCount, loaded: ticketsLoaded } = storeToRefs(tickets)
const site = useSiteStore()
const { current: currentSite } = storeToRefs(site)
const REFRESH_MS = 60_000
let refreshTimer: ReturnType<typeof setInterval> | undefined
function refreshTickets(): void {
  void tickets.load(site.apiSite())
}
watch(currentSite, refreshTickets)
onMounted(() => {
  refreshTickets()
  refreshTimer = setInterval(refreshTickets, REFRESH_MS)
})
onBeforeUnmount(() => clearInterval(refreshTimer))

function badge(item: NavItem): number {
  return item.id === 'tickets' && ticketsLoaded.value ? openCount.value : 0
}

watch(
  () => route.fullPath,
  () => (collapsed.value = true),
)

function onKey(e: KeyboardEvent): void {
  if (e.key === 'Escape') collapsed.value = true
}
onMounted(() => document.addEventListener('keydown', onKey))
onBeforeUnmount(() => document.removeEventListener('keydown', onKey))

// A detail screen belongs to its section: its first crumb links back to it.
function isActive(item: NavItem): boolean {
  return route.name === item.routeName || route.meta.crumbs?.[0]?.to === item.routeName
}
</script>

<template>
  <!-- The slot holds the rail's width in the page grid; the panel itself is
       fixed, so expanding it covers the page instead of pushing it. -->
  <div class="sidebar-slot">
    <div v-if="!collapsed" class="sidebar-backdrop" aria-hidden="true" @click="collapsed = true" />
    <aside :class="['sidebar', { 'sidebar--collapsed': collapsed }]">
      <RouterLink
        class="sidebar-brand"
        :to="{ name: 'overview' }"
        :aria-label="t('common.appName')"
      >
        <AppLogo :size="32" :wordmark="false" />
      </RouterLink>

      <nav class="sidebar-nav" :aria-label="t('common.nav.label')">
        <RouterLink
          v-for="it in NAV"
          :key="it.id"
          :to="{ name: it.routeName }"
          :class="['nav-item', { active: isActive(it) }]"
          :title="collapsed ? t(it.labelKey) : undefined"
          :aria-current="isActive(it) ? 'page' : undefined"
        >
          <component :is="it.icon" class="ico" :size="18" aria-hidden="true" />
          <span v-if="!collapsed" class="nav-item__label">{{ t(it.labelKey) }}</span>
          <span v-else class="sr-only">{{ t(it.labelKey) }}</span>
          <span
            v-if="badge(it) > 0"
            :class="['nav-badge tabular', { 'nav-badge--rail': collapsed }]"
            :title="t('tickets.nav.badge', { n: badge(it) })"
          >
            {{ badge(it) }}
            <span class="sr-only">{{ t('tickets.nav.badge', { n: badge(it) }) }}</span>
          </span>
        </RouterLink>
      </nav>

      <div class="sidebar-foot">
        <RouterLink
          v-for="it in NAV_FOOTER"
          :key="it.id"
          :to="{ name: it.routeName }"
          :class="['nav-item', { active: isActive(it) }]"
          :title="collapsed ? t(it.labelKey) : undefined"
          :aria-current="isActive(it) ? 'page' : undefined"
        >
          <component :is="it.icon" class="ico" :size="18" aria-hidden="true" />
          <span v-if="!collapsed" class="nav-item__label">{{ t(it.labelKey) }}</span>
          <span v-else class="sr-only">{{ t(it.labelKey) }}</span>
        </RouterLink>
        <button
          type="button"
          class="nav-item sidebar-toggle"
          :title="collapsed ? t('common.nav.expand') : undefined"
          :aria-expanded="!collapsed"
          @click="collapsed = !collapsed"
        >
          <component
            :is="collapsed ? PanelLeftOpen : PanelLeftClose"
            class="ico"
            :size="18"
            aria-hidden="true"
          />
          <span v-if="!collapsed" class="nav-item__label">{{ t('common.nav.collapse') }}</span>
          <span v-else class="sr-only">{{ t('common.nav.expand') }}</span>
        </button>
      </div>
    </aside>
  </div>
</template>

<style scoped>
.nav-item {
  position: relative;
}
.nav-badge {
  margin-left: auto;
  min-width: 18px;
  height: 18px;
  padding: 0 5px;
  border-radius: 999px;
  background: var(--danger);
  color: #fff;
  font-size: 10px;
  font-weight: 600;
  line-height: 18px;
  text-align: center;
}
.nav-badge--rail {
  position: absolute;
  top: 2px;
  right: 2px;
  margin: 0;
  min-width: 16px;
  height: 16px;
  padding: 0 4px;
  line-height: 16px;
}
</style>
