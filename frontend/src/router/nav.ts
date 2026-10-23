import type { Component } from 'vue'
import {
  BookOpen,
  ChartColumn,
  Inbox,
  LayoutDashboard,
  Route,
  Settings,
  Users,
} from 'lucide-vue-next'

export interface NavItem {
  id: string
  routeName: string
  // i18n key resolved to the display label at render time (see AppSidebar).
  labelKey: string
  // Icon component, not a name string: icons are bundled, never fetched.
  icon: Component
}

// Sitemap of the HR cabinet (ux-spec v1 §3.0), in its order. Every section is a
// route, not local state — a reload or a pasted link has to land on the same
// screen as the person who sent it.
export const NAV: NavItem[] = [
  {
    id: 'overview',
    routeName: 'overview',
    labelKey: 'common.nav.items.overview',
    icon: LayoutDashboard,
  },
  { id: 'newcomers', routeName: 'newcomers', labelKey: 'common.nav.items.newcomers', icon: Users },
  { id: 'tickets', routeName: 'tickets', labelKey: 'common.nav.items.tickets', icon: Inbox },
  { id: 'scenarios', routeName: 'scenarios', labelKey: 'common.nav.items.scenarios', icon: Route },
  {
    id: 'knowledge',
    routeName: 'knowledge',
    labelKey: 'common.nav.items.knowledge',
    icon: BookOpen,
  },
  {
    id: 'analytics',
    routeName: 'analytics',
    labelKey: 'common.nav.items.analytics',
    icon: ChartColumn,
  },
]

// Bottom of the rail: the last sitemap item, set apart as in most admin shells.
export const NAV_FOOTER: NavItem[] = [
  { id: 'settings', routeName: 'settings', labelKey: 'common.nav.items.settings', icon: Settings },
]
