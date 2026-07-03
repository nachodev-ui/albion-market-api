---
layout: home

hero:
  name: Albion Market API
  text: Datos de mercado centralizados
  tagline: Ingesta segura, consultas rápidas y operación reproducible sobre PostgreSQL.
  actions:
    - theme: brand
      text: Comenzar
      link: /guide/getting-started
    - theme: alt
      text: Referencia HTTP
      link: /api/endpoints
    - theme: alt
      text: Releases
      link: /release/
    - theme: alt
      text: Ver en GitHub
      link: https://github.com/nachodev-ui/albion-market-api

features:
  - icon: ⚡
    title: Lectura rápida
    details: Precios actuales e historial consolidados en tablas calientes optimizadas para el frontend.
  - icon: 🔐
    title: Ingesta segura
    details: Bearer tokens rotables, comparación constante, límites de payload y controles HTTPS.
  - icon: 🧱
    title: Auditoría durable
    details: Escritura raw separada del modelo de lectura, idempotencia por request_id y retención segura.
  - icon: ♻️
    title: Recuperación comprobada
    details: Backups custom, SHA256 y restauraciones automáticas en bases desechables.
  - icon: 📈
    title: Operable
    details: Estado, métricas, logs estructurados, mantenimiento programado y revisión reproducible de índices.
  - icon: 📦
    title: Distribución verificable
    details: Imágenes por digest, SBOM, firma keyless, attestations y releases SemVer reproducibles.
  - icon: 📚
    title: Documentación como código
    details: Markdown versionado, búsqueda local, validación en pull requests y publicación desde main.
---

## Flujo general

```text
Albion Data Client → receiver local → forwarder → API → PostgreSQL → consumidores
```

La API conserva la auditoría raw, actualiza modelos de lectura e impide que una captura antigua reemplace datos más recientes.
