// Smoke-test the real Compose service definitions with fresh, isolated data.
// No fixed container names, host ports, production volume names, or credentials
// are carried into this temporary stack.
import { execFileSync, spawn } from 'node:child_process';
import { randomUUID } from 'node:crypto';
import { mkdtemp, writeFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const project = `ticket-temporal-smoke-${randomUUID().slice(0, 8)}`;
const directory = await mkdtemp(join(tmpdir(), `${project}-`));
const filename = join(directory, 'compose.json');
const password = randomUUID();
const env = {
  ...process.env,
  IDENTITY_DB_PASSWORD: password,
  TICKETS_DB_PASSWORD: password,
  ORDERS_DB_PASSWORD: password,
  BETTER_AUTH_SECRET: randomUUID(),
  BETTER_AUTH_URL: 'http://localhost:3000',
};
const original = JSON.parse(
  execFileSync('docker', ['compose', 'config', '--format', 'json'], {
    env,
    encoding: 'utf8',
  }),
);
const names = [
  'temporal',
  'temporal-namespace',
  'tickets-db',
  'tickets-migrate',
  'tickets',
  'orders-db',
  'orders-migrate',
  'orders',
];
const services = Object.fromEntries(
  names.map((name) => {
    const service = original.services[name];
    delete service.container_name;
    delete service.ports;
    if (
      ['tickets', 'tickets-migrate', 'orders', 'orders-migrate'].includes(name)
    ) {
      service.image = `${project}-${name.replace('-migrate', '')}:test`;
      if (name.endsWith('-migrate')) delete service.build;
    }
    return [name, service];
  }),
);
const volumes = Object.fromEntries(
  ['temporal-data', 'tickets-db-data', 'orders-db-data'].map((name) => [
    name,
    {},
  ]),
);
await writeFile(
  filename,
  JSON.stringify({
    name: project,
    services,
    volumes,
    networks: { default: {} },
  }),
);
const compose = ['compose', '--project-name', project, '-f', filename];

async function docker(args) {
  await new Promise((resolve, reject) => {
    const child = spawn('docker', args, { stdio: 'inherit', env });
    child.once('error', reject);
    child.once('exit', (code) =>
      code === 0
        ? resolve()
        : reject(new Error(`Docker command failed (${code})`)),
    );
  });
}

try {
  await docker([...compose, 'build', 'tickets', 'orders']);
  await docker([...compose, 'up', '-d', '--build', 'tickets', 'orders']);
  const output = execFileSync(
    'docker',
    [...compose, 'ps', '--all', '--format', 'json'],
    { env, encoding: 'utf8' },
  );
  const rows = output.trim().startsWith('[')
    ? JSON.parse(output)
    : output
        .trim()
        .split('\n')
        .map((line) => JSON.parse(line));
  for (const name of names) {
    const row = rows.find((row) => row.Service === name);
    const oneShot = name.endsWith('-migrate') || name === 'temporal-namespace';
    if (
      !row ||
      (oneShot
        ? row.State !== 'exited' || row.ExitCode !== 0
        : row.State !== 'running')
    ) {
      throw new Error(
        `Unexpected startup state for ${name}: ${JSON.stringify(row)}`,
      );
    }
  }
  await docker([
    ...compose,
    'exec',
    '-T',
    'temporal',
    'temporal',
    'operator',
    'namespace',
    'describe',
    '--namespace',
    'default',
  ]);
  console.log('Fresh-volume Temporal/Tickets/Orders Compose startup passed.');
} catch (error) {
  await docker([...compose, 'logs', '--tail', '60']);
  throw error;
} finally {
  try {
    await docker([...compose, 'down', '--volumes', '--remove-orphans']);
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
}
