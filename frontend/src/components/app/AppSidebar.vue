<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { PanelLeftClose, PanelLeftOpen } from 'lucide-vue-next'
import { storeToRefs } from 'pinia'
import { NAV, NAV_FOOTER, type NavItem } from '@/router/nav'
import AppLogo from '@/components/common/AppLogo.vue'
import UserMenu from '@/components/app/UserMenu.vue'
import { useTicketsStore } from '@/stores/tickets'
import { useSiteStore } from '@/stores/site'
import { useAuthStore } from '@/stores/auth'
import { canOpen, homeRoute } from '@/lib/roles'

// By default the sidebar is a docked 220 px panel naming every section; the
// page gets the rest of the width. The toggle folds it to a 56 px icon rail
// with the names in the tooltips; the choice is remembered per browser.
const route = useRoute()
const { t } = useI18n()
const STORAGE_KEY = 'korsik.sidebar.collapsed'
function readCollapsed(): boolean {
  try {
    return localStorage.getItem(STORAGE_KEY) === '1'
  } catch {
    return false
  }
}
const collapsed = ref(readCollapsed())
watch(collapsed, (v) => {
  try {
    localStorage.setItem(STORAGE_KEY, v ? '1' : '0')
  } catch {
    // storage blocked: the choice lasts until reload
  }
})

// Badge of open (not resolved) tickets, for the site picked in the topbar —
// the same list the queue shows. Refreshed every minute and on site change;
// ticket actions refresh it themselves.
const auth = useAuthStore()
const { viewer } = storeToRefs(auth)
// The menu by role (lib/roles.ts): manager — only "My newcomers", recruiter —
// all but "Team"; with auth off — as before, without the user-bound screens.
const nav = computed(() => NAV.filter((it) => canOpen(viewer.value, it.routeName)))
const navFooter = computed(() => NAV_FOOTER.filter((it) => canOpen(viewer.value, it.routeName)))
const home = computed(() => homeRoute(viewer.value))

const tickets = useTicketsStore()
const { openCount, loaded: ticketsLoaded } = storeToRefs(tickets)
const site = useSiteStore()
const { current: currentSite } = storeToRefs(site)
const REFRESH_MS = 60_000
let refreshTimer: ReturnType<typeof setInterval> | undefined
function refreshTickets(): void {
  // The head of unit has no tickets (403): no badge to fetch.
  if (!canOpen(viewer.value, 'tickets')) return
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

// A detail screen belongs to its section: its first crumb links back to it.
function isActive(item: NavItem): boolean {
  return route.name === item.routeName || route.meta.crumbs?.[0]?.to === item.routeName
}
</script>

<template>
  <!-- The slot holds the panel's width in the page grid; the panel itself is
       fixed, so it stays put while the page scrolls. -->
  <div :class="['sidebar-slot', { 'sidebar-slot--collapsed': collapsed }]">
    <aside :class="['sidebar', { 'sidebar--collapsed': collapsed }]">
      <RouterLink class="sidebar-brand" :to="{ name: home }" :aria-label="t('common.appName')">
        <AppLogo :size="32" :wordmark="!collapsed" />
      </RouterLink>

      <nav class="sidebar-nav" :aria-label="t('common.nav.label')">
        <RouterLink
          v-for="it in nav"
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
        <UserMenu :collapsed="collapsed" />
        <RouterLink
          v-for="it in navFooter"
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
