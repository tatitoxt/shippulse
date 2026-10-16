# ShipPulse

ShipPulse es un laboratorio de una plataforma para observar servicios y coordinar despliegues. El repositorio combina una interfaz web, servicios en Go y componentes de infraestructura local.

```bash
git clone https://github.com/tatitoxt/shippulse.git && cd shippulse
docker compose up -d
make build && make seed
```

## Qué explora

- Catálogo de servicios y dependencias.
- Eventos de despliegue e historial de estado.
- Ingesta de telemetría con OpenTelemetry.
- Alertas e indicadores de disponibilidad.

## Arranque local

Requiere Docker Compose, Go 1.22+ y Node.js 20+.

Después se pueden iniciar el API y la interfaz en terminales separadas:

```bash
cd services/control-plane && go run ./cmd/server
cd apps/web && npm install && npm run dev
```

La aplicación está en desarrollo. Los valores de demo deben configurarse en el entorno local; no pongas credenciales reales en issues, capturas ni archivos versionados. Consultá `make help` y la documentación del repo si un comando no está disponible en tu checkout.

## Licencia

MIT. Ver [LICENSE](LICENSE).
