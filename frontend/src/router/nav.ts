import type { Component } from 'vue'
import {
  BookOpen,
  ChartColumn,
  Images,
  Inbox,
  LayoutDashboard,
  Route,
  Settings,
  Gauge,
  UserCheck,
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
// Visibility by role is not here but in lib/roles.ts (canOpen), keyed by
// routeName, so the guard and the menu read one table.
export const NAV: NavItem[] = [
  // Ordered by how often a person opens it: today's decisions and the queue first, the
  // reference sections last.
  {
    id: 'dashboard',
    routeName: 'dashboard',
    labelKey: 'common.nav.items.dashboard',
    icon: Gauge,
  },
  { id: 'tickets', routeName: 'tickets', labelKey: 'common.nav.items.tickets', icon: Inbox },
  { id: 'newcomers', routeName: 'newcomers', labelKey: 'common.nav.items.newcomers', icon: Users },
  // Head of unit's "My employees" (case role 03).
  { id: 'manager', routeName: 'manager', labelKey: 'common.nav.items.manager', icon: UserCheck },
  {
    id: 'analytics',
    routeName: 'analytics',
    labelKey: 'common.nav.items.analytics',
    icon: ChartColumn,
  },
  {
    id: 'overview',
    routeName: 'overview',
    labelKey: 'common.nav.items.overview',
    icon: LayoutDashboard,
  },
  { id: 'scenarios', routeName: 'scenarios', labelKey: 'common.nav.items.scenarios', icon: Route },
  {
    id: 'knowledge',
    routeName: 'knowledge',
    labelKey: 'common.nav.items.knowledge',
    icon: BookOpen,
  },
  { id: 'media', routeName: 'media', labelKey: 'media.nav', icon: Images },
]

// Bottom of the rail: the last sitemap item, set apart as in most admin shells.
export const NAV_FOOTER: NavItem[] = [
  // The dev chat (/dev/chat) stays a route for demos but is not in the menu.
  { id: 'settings', routeName: 'settings', labelKey: 'common.nav.items.settings', icon: Settings },
]
