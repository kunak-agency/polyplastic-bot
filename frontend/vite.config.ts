import { defineConfig, loadEnv, type Plugin } from 'vite'
import { fileURLToPath, URL } from 'node:url'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'

// ИНВАРИАНТ: ноль внешних origin. В политике только 'self' и адрес API —
// шрифты лежат в public/fonts, иконки приезжают Vue-компонентами, графики
// (echarts) бандлятся, всё остальное тоже. Любой CDN (шрифт, тайлы карты,
// аналитика) — это правка политики И решение, записанное в README, а не тихая
// строка в конфиге.
//
// Нарушение здесь тихое: ресурс просто не грузится, в интерфейсе пусто, а
// причина видна только строкой CSP в консоли.
function apiOrigins(apiBase: string): string[] {
  try {
    return [new URL(apiBase).origin]
  } catch {
    // A relative base ("/api") is same-origin: 'self' already covers it.
    return []
  }
}

// The call page (call.html) opens a WebSocket /api/call, so its connect-src also
// carries the ws(s) twin of the API origin: same host and port, scheme mapped
// http→ws / https→wss — no new origin is invented. With a relative base
// ("/api", the deployed build) the socket is same-origin and 'self' covers it
// (CSP3: 'self' matches ws/wss of the page's host). The cabinet's policy is
// unchanged. Worklets of the call are bundled same-origin files (script-src 'self').
function wsTwins(apiBase: string): string[] {
  try {
    const url = new URL(apiBase)
    const scheme = url.protocol === 'https:' ? 'wss:' : url.protocol === 'http:' ? 'ws:' : ''
    return scheme ? [`${scheme}//${url.host}`] : []
  } catch {
    return []
  }
}

function buildCsp(apiBase: string, withSocket = false): string {
  const connect = ["'self'", ...apiOrigins(apiBase), ...(withSocket ? wsTwins(apiBase) : [])].join(
    ' ',
  )
  return [
    "default-src 'self'",
    "script-src 'self'",
    // 'unsafe-inline' is required by the inline style attributes Vue and the
    // Tailwind runtime emit; scripts stay strict.
    "style-src 'self' 'unsafe-inline'",
    "img-src 'self' data: blob:",
    // Предпросмотр документа базы знаний: файл ФЕТЧИТСЯ через клиент API и
    // показывается как blob:-адрес, который приложение создало само, — новый
    // origin в политику при этом не приезжает.
    "frame-src 'self' blob:",
    "font-src 'self' data:",
    `connect-src ${connect}`,
    "object-src 'none'",
    // frame-ancestors здесь НЕТ намеренно: из <meta> браузер эту директиву не
    // читает и пишет ошибку в консоль. Запрет встраивания в чужой фрейм отдаёт
    // заголовком сервер, который раздаёт интерфейс.
    "base-uri 'self'",
    "form-action 'self'",
  ].join('; ')
}

function cspPlugin(apiBase: string): Plugin {
  const pagePolicy = buildCsp(apiBase)
  const callPolicy = buildCsp(apiBase, true)
  return {
    name: 'polyplastic-csp-meta',
    transformIndexHtml(html, ctx) {
      const policy = /(^|\/)call\.html$/.test(ctx.path) ? callPolicy : pagePolicy
      const meta = `    <meta http-equiv="Content-Security-Policy" content="${policy}" />\n`
      return html.replace(/<head>\s*/i, (match) => `${match}${meta}`)
    },
  }
}

// A hackathon build must never be indexed, in any environment.
// The meta tag is defense-in-depth next to public/robots.txt and any CDN header.
function noindexPlugin(): Plugin {
  return {
    name: 'polyplastic-noindex-meta',
    transformIndexHtml(html) {
      const meta = `    <meta name="robots" content="noindex, nofollow" />\n`
      return html.replace(/<head>\s*/i, (match) => `${match}${meta}`)
    },
  }
}

// https://vite.dev/config/
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '')
  const apiBase = env.VITE_API_BASE_URL || 'http://localhost:8081/api'
  // Dev proxy target: the API origin when the base is absolute, else the local API.
  const proxyTarget = apiOrigins(apiBase)[0] ?? 'http://localhost:8081'

  return {
    plugins: [vue(), tailwindcss(), cspPlugin(apiBase), noindexPlugin()],
    resolve: {
      alias: {
        '@': fileURLToPath(new URL('./src', import.meta.url)),
      },
    },
    server: {
      port: 5173,
      // В разработке API — на :8081 (agency/system/api_contract.md). Прокси нужен,
      // только если VITE_API_BASE_URL задан относительным ("/api").
      // ws: true — WebSocket /api/call (call.html) goes through the same proxy.
      proxy: {
        '/api': { target: proxyTarget, ws: true },
      },
    },
    build: {
      // Two pages: the cabinet (index.html) and the call to Korsik (call.html,
      // served on call.plexet.ru). Shared chunks are split by Rollup.
      rollupOptions: {
        input: {
          main: fileURLToPath(new URL('./index.html', import.meta.url)),
          call: fileURLToPath(new URL('./call.html', import.meta.url)),
        },
      },
    },
  }
})
