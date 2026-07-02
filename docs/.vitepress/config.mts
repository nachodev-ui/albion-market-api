import { defineConfig } from 'vitepress'

export default defineConfig({
  lang: 'es-CL',
  title: 'Albion Market API',
  description: 'Documentación de la API centralizada de precios e historial de Albion Online.',
  base: '/albion-market-api/',
  cleanUrls: true,
  lastUpdated: true,
  sitemap: {
    hostname: 'https://nachodev-ui.github.io/albion-market-api/'
  },
  head: [
    ['meta', { name: 'theme-color', content: '#2563eb' }],
    ['link', { rel: 'icon', href: '/albion-market-api/favicon.svg', type: 'image/svg+xml' }]
  ],
  themeConfig: {
    logo: '/favicon.svg',
    siteTitle: 'Albion Market API',
    nav: [
      { text: 'Guía', link: '/guide/overview' },
      { text: 'API', link: '/api/endpoints' },
      { text: 'PostgreSQL', link: '/database/' },
      { text: 'Operación', link: '/operations/' },
      { text: 'Despliegue', link: '/deployment/' },
      { text: 'Seguridad', link: '/security/' }
    ],
    sidebar: [
      {
        text: 'Guía',
        items: [
          { text: 'Descripción general', link: '/guide/overview' },
          { text: 'Inicio rápido', link: '/guide/getting-started' },
          { text: 'Arquitectura', link: '/guide/architecture' },
          { text: 'Configuración', link: '/guide/configuration' }
        ]
      },
      {
        text: 'API HTTP',
        items: [
          { text: 'Referencia de endpoints', link: '/api/endpoints' },
          { text: 'Contrato OpenAPI', link: '/api/openapi' },
          { text: 'Integración con frontend', link: '/api/frontend-consumption' },
          { text: 'Historial centralizado', link: '/api/market-history' }
        ]
      },
      {
        text: 'PostgreSQL',
        items: [
          { text: 'Visión general', link: '/database/' },
          { text: 'Auditoría', link: '/database/audit' },
          { text: 'Retención', link: '/database/retention' },
          { text: 'Backup y restauración', link: '/database/backup-restore' },
          { text: 'Revisión de índices', link: '/database/index-review' }
        ]
      },
      {
        text: 'Operación',
        items: [
          { text: 'Visión general', link: '/operations/' },
          { text: 'Observabilidad', link: '/operations/observability' },
          { text: 'Rendimiento', link: '/operations/performance' }
        ]
      },
      {
        text: 'Despliegue',
        items: [
          { text: 'Contenedores seguros', link: '/deployment/' }
        ]
      },
      {
        text: 'Seguridad y pruebas',
        items: [
          { text: 'Seguridad', link: '/security/' },
          { text: 'Secretos y autenticación', link: '/security/secrets' },
          { text: 'Pruebas', link: '/testing/' },
          { text: 'Contratos OpenAPI', link: '/testing/contracts' },
          { text: 'End-to-end', link: '/testing/end-to-end' }
        ]
      },
      {
        text: 'Referencia',
        items: [
          { text: 'Migraciones', link: '/reference/migrations' },
          { text: 'Scripts', link: '/reference/scripts' },
          { text: 'Mantener la documentación', link: '/contributing/documentation' }
        ]
      }
    ],
    search: {
      provider: 'local',
      options: {
        translations: {
          button: { buttonText: 'Buscar', buttonAriaLabel: 'Buscar' },
          modal: {
            noResultsText: 'No se encontraron resultados',
            resetButtonTitle: 'Limpiar búsqueda',
            footer: {
              selectText: 'seleccionar',
              navigateText: 'navegar',
              closeText: 'cerrar'
            }
          }
        }
      }
    },
    socialLinks: [
      { icon: 'github', link: 'https://github.com/nachodev-ui/albion-market-api' }
    ],
    editLink: {
      pattern: 'https://github.com/nachodev-ui/albion-market-api/edit/develop/docs/:path',
      text: 'Editar esta página en GitHub'
    },
    lastUpdated: {
      text: 'Última actualización',
      formatOptions: { dateStyle: 'medium', timeStyle: 'short' }
    },
    outline: { level: [2, 3], label: 'En esta página' },
    docFooter: { prev: 'Anterior', next: 'Siguiente' },
    returnToTopLabel: 'Volver arriba',
    sidebarMenuLabel: 'Menú',
    darkModeSwitchLabel: 'Apariencia',
    lightModeSwitchTitle: 'Cambiar a tema claro',
    darkModeSwitchTitle: 'Cambiar a tema oscuro',
    footer: {
      message: 'Documentación mantenida como código junto a la API.',
      copyright: 'Albion Market API'
    }
  }
})
