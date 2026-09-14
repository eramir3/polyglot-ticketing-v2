import { Logger } from '@nestjs/common';

async function bootstrap() {
  const { createApiGatewayApplication } = await import('./app/app.bootstrap.js');
  const app = await createApiGatewayApplication();
  app.enableShutdownHooks();
  const globalPrefix = 'api';
  const port = Number(process.env.PORT ?? 3000);
  await app.listen(port);
  Logger.log(
    `API gateway is running on: http://localhost:${port}/${globalPrefix}`,
  );
}

bootstrap();
