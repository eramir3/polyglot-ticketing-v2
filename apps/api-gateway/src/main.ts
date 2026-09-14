import { Logger } from '@nestjs/common';

async function bootstrap() {
  const shutdownTracing = startTracing('api-gateway');
  const { createApiGatewayApplication } = await import('./app/app.bootstrap.js');
  const loadTestMetricsToken = process.env.LOAD_TEST_METRICS_TOKEN ?? '';
  const app = await createApiGatewayApplication();
  app.enableShutdownHooks();
  app.use((request: any, response: any, next: () => void) => {
    const started = performance.now();
    response.once('finish', () => {
      const route =
        typeof request.route?.path === 'string'
          ? request.route.path
          : 'unmatched';
    });
    next();
  });
  const globalPrefix = 'api';
  const port = Number(process.env.PORT ?? 3000);
  await app.listen(port);
  process.once('SIGINT', () => void shutdownTracing());
  process.once('SIGTERM', () => void shutdownTracing());
  Logger.log(
    `API gateway is running on: http://localhost:${port}/${globalPrefix}`,
  );
}

bootstrap();
