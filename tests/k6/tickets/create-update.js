import http from 'k6/http';
import { check, sleep } from 'k6';
import { uuidv4 } from 'https://jslib.k6.io/k6-utils/1.4.0/index.js';
import { createVerifiedPerformanceUser } from '../shared/auth.js';

const baseUrl = __ENV.K6_BASE_URL || 'http://api-gateway:3000';
const mailpitUrl = __ENV.K6_MAILPIT_URL || 'http://mailpit:8025';
const testConfigs = JSON.parse(open('./create-update-configs.json'));
const profileName = readProfileName();
const profile = testConfigs[profileName];
const initialPrice = 5;
const firstUpdatePrice = 10;
const finalUpdatePrice = 15;

export const options = {
  scenarios: {
    [`tickets_create_update_${profileName}`]: profile.scenario,
  },
  ...(Object.keys(profile.thresholds).length === 0
    ? {}
    : { thresholds: profile.thresholds }),
};

export function setup() {
  return createVerifiedPerformanceUser({
    baseUrl,
    mailpitUrl,
    userName: 'Ticket Create and Update Performance User',
    userPrefix: 'tickets-create-update',
  });
}

export default function (performanceUser) {
  runLifecycle(performanceUser);

  if (profile.thinkTimeSeconds > 0) {
    sleep(profile.thinkTimeSeconds);
  }
}

function runLifecycle(performanceUser) {
  const baseTitle = `Performance ticket lifecycle ${runSuffix()}-${__VU}-${__ITER}`;
  const created = createTicket(performanceUser, baseTitle);
  if (!created) {
    return;
  }

  const firstUpdate = {
    id: created.id,
    price: firstUpdatePrice,
    title: `${baseTitle}-first-update`,
    userId: performanceUser.userId,
  };
  if (!updateTicket(performanceUser, firstUpdate)) {
    return;
  }

  const finalTicket = {
    ...firstUpdate,
    price: finalUpdatePrice,
    title: `${baseTitle}-second-update`,
  };
  if (!updateTicket(performanceUser, finalTicket)) {
    return;
  }

  const response = http.get(
    ticketUrl(finalTicket.id),
    requestParameters(performanceUser),
  );
  check(
    response,
    {
      'final retrieval returns HTTP 200': (result) => result.status === 200,
      'final retrieval returns the final ticket state': (result) =>
        hasTicket(result, finalTicket, 200),
    },
  );
}

function createTicket(performanceUser, title) {
  const expectedTicket = {
    price: initialPrice,
    title,
    userId: performanceUser.userId,
  };
  const idempotencyKey = uuidv4();
  const response = http.post(
    `${baseUrl}/api/tickets`,
    JSON.stringify({ price: expectedTicket.price, title: expectedTicket.title }),
    requestParameters(performanceUser, idempotencyKey),
  );
  const succeeded = check(
    response,
    {
      'creation returns HTTP 201': (result) => result.status === 201,
      'creation returns the created ticket': (result) =>
        hasTicket(result, expectedTicket, 201),
    },
  );
  if (!succeeded) {
    return undefined;
  }

  return response.json();
}

function updateTicket(performanceUser, expectedTicket) {
  const response = http.put(
    ticketUrl(expectedTicket.id),
    JSON.stringify({ price: expectedTicket.price, title: expectedTicket.title }),
    requestParameters(
      performanceUser,
      `ticket-update-${runSuffix()}-${__VU}-${__ITER}`,
    ),
  );
  return check(
    response,
    {
      'update returns HTTP 200': (result) => result.status === 200,
      'update returns the expected ticket': (result) =>
        hasTicket(result, expectedTicket, 200),
    },
  );
}

function hasTicket(response, expectedTicket, expectedStatus) {
  if (response.status !== expectedStatus) {
    return false;
  }

  try {
    const body = response.json();
    return (
      typeof body?.id === 'string' &&
      body.id.length > 0 &&
      (expectedTicket.id === undefined || body.id === expectedTicket.id) &&
      body.title === expectedTicket.title &&
      body.price === expectedTicket.price &&
      body.userId === expectedTicket.userId
    );
  } catch {
    return false;
  }
}

function requestParameters(performanceUser, idempotencyKey) {
  return {
    headers: {
      Cookie: performanceUser.sessionCookie,
      'Content-Type': 'application/json',
      ...(idempotencyKey === undefined
        ? {}
        : { 'Idempotency-Key': idempotencyKey }),
    },
  };
}

function ticketUrl(ticketId) {
  return `${baseUrl}/api/tickets/${encodeURIComponent(ticketId)}`;
}

function readProfileName() {
  const value = __ENV.K6_PROFILE || 'smoke';
  if (!Object.prototype.hasOwnProperty.call(testConfigs, value)) {
    throw new Error(
      `K6_PROFILE must be one of: ${Object.keys(testConfigs).join(', ')}`,
    );
  }

  return value;
}

function runSuffix() {
  const runId = String(__ENV.K6_TEST_ID || Date.now()).replace(
    /[^a-zA-Z0-9]/g,
    '-',
  );
  return `${runId}-${Math.floor(Math.random() * 1_000_000)}`;
}
