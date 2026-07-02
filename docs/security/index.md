# Seguridad

La API distingue rutas públicas de lectura y rutas autenticadas de ingesta.

## Controles principales

- tokens Bearer dedicados y rotables;
- comparación constante de credenciales;
- secretos desde variables o archivos montados;
- HTTPS obligatorio para ingesta en producción;
- límites de cuerpos, cabeceras y tiempos;
- CORS explícito y rate limiting por IP;
- respuestas sin detalles internos;
- rechazo en CI de archivos de secretos versionados.

Consulta [secretos y autenticación](./secrets.md) para configuración y rotación.
