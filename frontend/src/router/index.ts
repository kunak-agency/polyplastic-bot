import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'

declare module 'vue-router' {
  interface RouteMeta {
    // title and crumbs hold i18n message keys, resolved to display text at render
    // time by the consumer (AppTopbar/useAppTitle) — keeps message lookups out of
    // module init, before i18n is installed.
    title?: string
    // `to` is the route a crumb walks back to; the last crumb never links.
    crumbs?: { key: string; to?: string }[]
  }
}

// No auth yet: there is no guard, no requiresAuth/publicOnly meta and no second
// layout. Roles (HR / head of unit / security) come with their own PRD.
// Detail screens sit under their section's plural path (/newcomers/:id,
// /scenarios/:id, /tickets/:id) rather than ux-spec's singular /newcomer/{id}:
// the sidebar item then stays highlighted on them.
const routes: RouteRecordRaw[] = [
  {
    path: '/',
    name: 'overview',
    component: () => import('@/pages/overview/OverviewPage.vue'),
    meta: {
      title: 'common.routes.overview',
      crumbs: [{ key: 'common.nav.items.overview' }],
    },
  },
  {
    path: '/newcomers',
    name: 'newcomers',
    component: () => import('@/pages/newcomers/NewcomersPage.vue'),
    meta: {
      title: 'common.routes.newcomers',
      crumbs: [{ key: 'common.nav.items.newcomers' }],
    },
  },
  {
    path: '/newcomers/:id',
    name: 'newcomer',
    component: () => import('@/pages/newcomers/NewcomerPage.vue'),
    meta: {
      title: 'common.routes.newcomer',
      crumbs: [{ key: 'common.nav.items.newcomers', to: 'newcomers' }],
    },
  },
  {
    path: '/newcomers/:id/invite',
    name: 'newcomer-invite',
    component: () => import('@/pages/newcomers/NewcomerInvitePage.vue'),
    meta: {
      title: 'common.routes.newcomerInvite',
      crumbs: [{ key: 'common.nav.items.newcomers', to: 'newcomers' }],
    },
  },
  {
    path: '/tickets',
    name: 'tickets',
    component: () => import('@/pages/tickets/TicketsPage.vue'),
    meta: {
      title: 'common.routes.tickets',
      crumbs: [{ key: 'common.nav.items.tickets' }],
    },
  },
  {
    path: '/tickets/:id',
    name: 'ticket',
    component: () => import('@/pages/tickets/TicketPage.vue'),
    meta: {
      title: 'common.routes.ticket',
      crumbs: [{ key: 'common.nav.items.tickets', to: 'tickets' }],
    },
  },
  {
    path: '/scenarios',
    name: 'scenarios',
    component: () => import('@/pages/scenarios/ScenariosPage.vue'),
    meta: {
      title: 'common.routes.scenarios',
      crumbs: [{ key: 'common.nav.items.scenarios' }],
    },
  },
  {
    path: '/scenarios/:id',
    name: 'scenario',
    component: () => import('@/pages/scenarios/ScenarioEditorPage.vue'),
    meta: {
      title: 'common.routes.scenario',
      crumbs: [{ key: 'common.nav.items.scenarios', to: 'scenarios' }],
    },
  },
  {
    path: '/knowledge',
    name: 'knowledge',
    component: () => import('@/pages/knowledge/KnowledgePage.vue'),
    meta: {
      title: 'common.routes.knowledge',
      crumbs: [{ key: 'common.nav.items.knowledge' }],
    },
  },
  {
    path: '/media',
    name: 'media',
    component: () => import('@/pages/media/MediaPage.vue'),
    meta: { title: 'media.routeTitle', crumbs: [{ key: 'media.nav' }] },
  },
  {
    path: '/analytics',
    name: 'analytics',
    component: () => import('@/pages/analytics/AnalyticsPage.vue'),
    meta: {
      title: 'common.routes.analytics',
      crumbs: [{ key: 'common.nav.items.analytics' }],
    },
  },
  {
    path: '/manager',
    name: 'manager',
    component: () => import('@/pages/manager/ManagerPage.vue'),
    meta: {
      title: 'common.routes.manager',
      crumbs: [{ key: 'common.nav.items.manager' }],
    },
  },
  {
    path: '/settings',
    name: 'settings',
    component: () => import('@/pages/settings/SettingsPage.vue'),
    meta: {
      title: 'common.routes.settings',
      crumbs: [{ key: 'common.nav.items.settings' }],
    },
  },
  // Dev-only emulator of the newcomer's MAX dialog; absent from production builds.
  ...(import.meta.env.DEV
    ? [
        {
          path: '/dev/chat',
          name: 'dev-chat',
          component: () => import('@/pages/dev/DevChatPage.vue'),
          meta: { title: 'devchat.title', crumbs: [{ key: 'devchat.nav' }] },
        },
      ]
    : []),
  {
    path: '/:pathMatch(.*)*',
    name: 'not-found',
    component: () => import('@/pages/NotFound.vue'),
    meta: { title: 'common.routes.notFound' },
  },
]

export const router = createRouter({
  history: createWebHistory(),
  routes,
})
