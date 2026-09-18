import { ChildProcessWithoutNullStreams, spawn } from 'node:child_process';
import { randomUUID } from 'node:crypto';
import { createConnection, createServer } from 'node:net';
import { join } from 'node:path';
import { INestApplication, INestMicroservice } from '@nestjs/common';
import {
  PostgreSqlContainer,
  StartedPostgreSqlContainer,
} from '@testcontainers/postgresql';
import { Client } from 'pg';
import { GenericContainer, StartedTestContainer, Wait } from 'testcontainers';
import { createApiGatewayApplication } from '../src/app/app.bootstrap';
import { createIdentityMicroservice } from '../../identity/src/app/app.bootstrap';
import { migrateIdentityDatabase } from '../../identity/src/migrate-identity-database';

describe('payments endpoint', () => {
  let apiGateway: INestApplication;
  let identity: INestMicroservice;
  let identityDatabase: StartedPostgreSqlContainer;
  let ordersDatabase: StartedPostgreSqlContainer;
  let paymentsDatabase: StartedPostgreSqlContainer;
  let mailpit: StartedTestContainer;
  let ordersProcess: ChildProcessWithoutNullStreams;
  let paymentsProcess: ChildProcessWithoutNullStreams;
  let temporal: StartedTestContainer;
  let gatewayUrl: string;
  let identityDatabaseUrl: string;
  let ordersDatabaseUrl: string;
  let sessionCookie: string;
  let userId: string;

  beforeAll(async () => {
    const [gatewayPort, identityGrpcPort, ordersGrpcPort, paymentsGrpcPort] =
      await Promise.all([
        getAvailablePort(),
        getAvailablePort(),
        getAvailablePort(),
        getAvailablePort(),
      ]);
    [identityDatabase, ordersDatabase, paymentsDatabase] = await Promise.all([
      new PostgreSqlContainer('postgres:17-alpine')
        .withDatabase('identity')
        .withUsername('identity')
        .withPassword('identity-test-password')
        .start(),
      new PostgreSqlContainer('postgres:17-alpine')
        .withDatabase('orders')
        .withUsername('orders')
        .withPassword('orders-test-password')
        .start(),
      new PostgreSqlContainer('postgres:17-alpine')
        .withDatabase('payments')
        .withUsername('payments')
        .withPassword('payments-test-password')
        .start(),
    ]);
    temporal = await new GenericContainer('temporalio/temporal:1.9.1')
      .withCommand([
        'server',
        'start-dev',
        '--ip',
        '0.0.0.0',
        '--db-filename',
        '/tmp/temporal.db',
        '--headless',
      ])
      .withExposedPorts(7233)
      .withHealthCheck({
        test: [
          'CMD',
          'temporal',
          'operator',
          'cluster',
          'health',
          '--address',
          '127.0.0.1:7233',
        ],
        interval: 1_000,
        timeout: 5_000,
        retries: 60,
      })
      .withWaitStrategy(Wait.forHealthCheck())
      .withStartupTimeout(120_000)
      .start();
    mailpit = await new GenericContainer('axllent/mailpit:v1.28')
      .withExposedPorts(1025)
      .withWaitStrategy(Wait.forListeningPorts())
      .start();

    gatewayUrl = `http://127.0.0.1:${gatewayPort}`;
    identityDatabaseUrl = identityDatabase.getConnectionUri();
    ordersDatabaseUrl = withSslDisabled(ordersDatabase.getConnectionUri());
    const paymentsDatabaseUrl = withSslDisabled(
      paymentsDatabase.getConnectionUri(),
    );
    const temporalAddress = `${temporal.getHost()}:${temporal.getMappedPort(7233)}`;
    Object.assign(process.env, {
      BETTER_AUTH_SECRET: 'integration-test-secret-at-least-32-characters',
      BETTER_AUTH_URL: gatewayUrl,
      DATABASE_URL: identityDatabaseUrl,
      EMAIL_VERIFICATION_URL: `${gatewayUrl}/api/auth/verify-email`,
      IDENTITY_GRPC_URL: `127.0.0.1:${identityGrpcPort}`,
      ORDERS_GRPC_URL: `127.0.0.1:${ordersGrpcPort}`,
      PAYMENTS_GRPC_URL: `127.0.0.1:${paymentsGrpcPort}`,
      SMTP_FROM: 'no-reply@polyglot-ticketing.test',
      SMTP_HOST: mailpit.getHost(),
      SMTP_PORT: mailpit.getMappedPort(1025).toString(),
      TEMPORAL_ADDRESS: temporalAddress,
      TEMPORAL_NAMESPACE: 'default',
      TICKETING_USER_APP_ORIGIN: 'http://localhost:3001',
    });

    await migrateIdentityDatabase();
    await runGoCommand(
      ['run', './cmd/migrate'],
      { DATABASE_URL: ordersDatabaseUrl },
      ordersDirectory(),
    );
    await runGoCommand(
      ['run', './cmd/migrate'],
      { DATABASE_URL: paymentsDatabaseUrl },
      paymentsDirectory(),
    );
    ordersProcess = startOrdersService(
      ordersGrpcPort,
      ordersDatabaseUrl,
      temporalAddress,
    );
    await waitForPort(ordersGrpcPort, ordersProcess, 'Orders');
    paymentsProcess = startPaymentsService(
      paymentsGrpcPort,
      paymentsDatabaseUrl,
      ordersGrpcPort,
      temporalAddress,
    );
    await waitForPort(paymentsGrpcPort, paymentsProcess, 'Payments');
    identity = await createIdentityMicroservice(identityGrpcPort);
    await identity.listen();
    apiGateway = await createApiGatewayApplication();
    await apiGateway.listen(gatewayPort, '127.0.0.1');
    ({ sessionCookie, userId } = await createAuthenticatedUser());
  });

  afterAll(async () => {
    await apiGateway?.close();
    await identity?.close();
    await stopService(paymentsProcess);
    await stopService(ordersProcess);
    await temporal?.stop();
    await mailpit?.stop();
    await paymentsDatabase?.stop();
    await ordersDatabase?.stop();
    await identityDatabase?.stop();
  });

  it('requires an authenticated user', async () => {
    const response = await postPayment({ orderId: randomUUID() });

    expect(response.status).toBe(401);
    expect(response.body).toEqual({
      errors: [expect.objectContaining({ code: 'UNAUTHENTICATED' })],
    });
  });

  it('rejects an invalid order ID before calling Payments', async () => {
    const response = await postPayment(
      { orderId: 'not-a-uuid' },
      sessionCookie,
    );

    expect(response.status).toBe(400);
    expect(response.body).toEqual({
      errors: [
        expect.objectContaining({
          code: 'INVALID_ARGUMENT',
          field: 'orderId',
        }),
      ],
    });
  });

  it('returns 404 when the order does not belong to the caller', async () => {
    const response = await postPayment(
      { orderId: randomUUID() },
      sessionCookie,
    );

    expect(response.status).toBe(404);
    expect(response.body).toEqual({
      errors: [expect.objectContaining({ code: 'NOT_FOUND' })],
    });
  });

  it('creates a payment and returns it on an owner retry', async () => {
    const orderId = await seedCreatedOrder(userId);

    const created = await postPayment({ orderId }, sessionCookie);
    const replayed = await postPayment({ orderId }, sessionCookie);

    expect(created.status).toBe(201);
    expect(created.body).toEqual({ id: expect.any(String), orderId });
    expect(replayed.status).toBe(200);
    expect(replayed.body).toEqual(created.body);
  });

  it('rejects a payment for an order that is no longer payable', async () => {
    const orderId = await seedCreatedOrder(userId, 'Canceled');

    const response = await postPayment({ orderId }, sessionCookie);

    expect(response.status).toBe(409);
    expect(response.body).toEqual({
      errors: [expect.objectContaining({ code: 'ALREADY_EXISTS' })],
    });
  });

  async function createAuthenticatedUser(): Promise<{
    sessionCookie: string;
    userId: string;
  }> {
    const email = `payments-${randomUUID()}@example.com`;
    const password = 'password123';
    const signupResponse = await postJson('/api/auth/signup', {
      email,
      name: 'Payments Integration User',
      password,
    });
    expect(signupResponse.status).toBe(201);
    const signup = signupResponse.body as { userId: string };

    const database = new Client({ connectionString: identityDatabaseUrl });
    await database.connect();
    try {
      await database.query(
        'UPDATE "user" SET "emailVerified" = true WHERE id = $1',
        [signup.userId],
      );
    } finally {
      await database.end();
    }

    const signinResponse = await postJson('/api/auth/signin', {
      email,
      password,
    });
    expect(signinResponse.status).toBe(201);
    const setCookie = signinResponse.headers.get('set-cookie');
    if (!setCookie) {
      throw new Error('Expected signin to set the session cookie.');
    }

    return { sessionCookie: setCookie.split(';', 1)[0], userId: signup.userId };
  }

  async function seedCreatedOrder(
    ownerID: string,
    status: 'Canceled' | 'Created' = 'Created',
  ): Promise<string> {
    const orderID = randomUUID();
    const ticketID = randomUUID();
    const database = new Client({ connectionString: ordersDatabaseUrl });
    await database.connect();
    try {
      await database.query(
        'INSERT INTO tickets (id, title, price, aggregate_version) VALUES ($1, $2, $3, $4)',
        [ticketID, 'Projected concert ticket', 10_000, 1],
      );
      await database.query(
        `INSERT INTO orders (id, expires_at, user_id, ticket_id, status)
         VALUES ($1, NOW() + INTERVAL '15 minutes', $2, $3, $4)`,
        [orderID, ownerID, ticketID, status],
      );
    } finally {
      await database.end();
    }
    return orderID;
  }

  function postPayment(
    body: { orderId: string },
    cookie?: string,
  ): Promise<HttpResponse> {
    return postJson('/api/payments', body, cookie);
  }

  async function postJson(
    path: string,
    body: Record<string, string>,
    cookie?: string,
  ): Promise<HttpResponse> {
    const response = await fetch(`${gatewayUrl}${path}`, {
      body: JSON.stringify(body),
      headers: {
        'content-type': 'application/json',
        ...(cookie === undefined ? {} : { cookie }),
      },
      method: 'POST',
    });
    return {
      body: await response.json(),
      headers: response.headers,
      status: response.status,
    };
  }
});

interface HttpResponse {
  body: unknown;
  headers: Headers;
  status: number;
}

function startOrdersService(
  grpcPort: number,
  databaseUrl: string,
  temporalAddress: string,
): ChildProcessWithoutNullStreams {
  return spawn('go', ['run', './cmd/orders'], {
    cwd: ordersDirectory(),
    detached: true,
    env: {
      ...process.env,
      DATABASE_URL: databaseUrl,
      GRPC_PORT: grpcPort.toString(),
      TEMPORAL_ADDRESS: temporalAddress,
      TEMPORAL_NAMESPACE: 'default',
      TICKETS_GRPC_URL: '127.0.0.1:1',
    },
    stdio: 'pipe',
  });
}

function startPaymentsService(
  grpcPort: number,
  databaseUrl: string,
  ordersGrpcPort: number,
  temporalAddress: string,
): ChildProcessWithoutNullStreams {
  return spawn('go', ['run', './cmd/payments'], {
    cwd: paymentsDirectory(),
    detached: true,
    env: {
      ...process.env,
      DATABASE_URL: databaseUrl,
      GRPC_PORT: grpcPort.toString(),
      ORDERS_GRPC_URL: `127.0.0.1:${ordersGrpcPort}`,
      PAYMENT_PROCESSOR_OUTCOME: 'success',
      PAYMENTS_TEMPORAL_TASK_QUEUE: 'payment-processing',
      TEMPORAL_ADDRESS: temporalAddress,
      TEMPORAL_NAMESPACE: 'default',
    },
    stdio: 'pipe',
  });
}

async function runGoCommand(
  args: string[],
  environment: NodeJS.ProcessEnv,
  cwd: string,
): Promise<void> {
  const command = spawn('go', args, {
    cwd,
    env: { ...process.env, ...environment },
    stdio: 'pipe',
  });
  const output = collectProcessOutput(command);
  await new Promise<void>((resolve, reject) => {
    command.once('error', reject);
    command.once('exit', (code) => {
      if (code === 0) {
        resolve();
        return;
      }
      reject(
        new Error(
          `go ${args.join(' ')} failed with exit code ${code}: ${output()}`,
        ),
      );
    });
  });
}

async function waitForPort(
  port: number,
  service: ChildProcessWithoutNullStreams,
  serviceName: string,
): Promise<void> {
  const deadline = Date.now() + 15_000;
  while (Date.now() < deadline) {
    if (service.exitCode !== null) {
      throw new Error(`${serviceName} exited before accepting gRPC requests.`);
    }
    if (await canConnect(port)) {
      return;
    }
    await delay(100);
  }
  throw new Error(`${serviceName} did not start within 15 seconds.`);
}

function canConnect(port: number): Promise<boolean> {
  return new Promise((resolve) => {
    const connection = createConnection({ host: '127.0.0.1', port });
    connection.once('connect', () => {
      connection.end();
      resolve(true);
    });
    connection.once('error', () => resolve(false));
  });
}

async function stopService(
  service: ChildProcessWithoutNullStreams | undefined,
): Promise<void> {
  if (!service || service.exitCode !== null) {
    return;
  }
  const exited = new Promise<void>((resolve) =>
    service.once('exit', () => resolve()),
  );
  terminateProcessGroup(service, 'SIGTERM');
  const stopped = await Promise.race([
    exited.then(() => true),
    delay(5_000).then(() => false),
  ]);
  if (!stopped) {
    terminateProcessGroup(service, 'SIGKILL');
    await exited;
  }
}

function terminateProcessGroup(
  childProcess: ChildProcessWithoutNullStreams,
  signal: NodeJS.Signals,
): void {
  if (childProcess.pid === undefined) {
    return;
  }
  try {
    globalThis.process.kill(-childProcess.pid, signal);
  } catch {
    childProcess.kill(signal);
  }
}

function collectProcessOutput(
  process: ChildProcessWithoutNullStreams,
): () => string {
  let output = '';
  process.stdout.on('data', (chunk: Buffer) => {
    output += chunk.toString();
  });
  process.stderr.on('data', (chunk: Buffer) => {
    output += chunk.toString();
  });
  return () => output;
}

function getAvailablePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const server = createServer();
    server.once('error', reject);
    server.listen(0, '127.0.0.1', () => {
      const address = server.address();
      if (address === null || typeof address === 'string') {
        reject(new Error('Unable to allocate a TCP port.'));
        return;
      }
      server.close((error) => (error ? reject(error) : resolve(address.port)));
    });
  });
}

function withSslDisabled(databaseUrl: string): string {
  return databaseUrl.includes('?')
    ? `${databaseUrl}&sslmode=disable`
    : `${databaseUrl}?sslmode=disable`;
}

function delay(milliseconds: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, milliseconds));
}

function ordersDirectory(): string {
  return join(process.cwd(), 'apps/orders');
}

function paymentsDirectory(): string {
  return join(process.cwd(), 'apps/payments');
}
