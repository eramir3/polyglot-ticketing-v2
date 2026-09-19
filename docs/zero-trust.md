# Zero-Trust Service-Traffic Policy

## Status and scope

This is the target-state policy for service-to-service traffic in the
Kubernetes deployment. It applies to gRPC calls among API Gateway, Identity,
Tickets, Orders, and Payments. Browser/API security, database security,
Temporal security, supply-chain security, and incident response are outside
this document's scope.

Local Docker Compose currently uses plaintext internal gRPC for developer
convenience. It is an isolated development exception and is not compliant with
this policy. It must not be used as the production security model.

## Policy requirements

- Every service call must authenticate the calling workload. Network location,
  a Kubernetes Service name, and request fields are not proof of identity.
- Every RPC must be authorized for the authenticated caller using least
  privilege. Calls not listed below are denied.
- Service traffic must be encrypted in transit using mutual TLS (mTLS).
- Kubernetes namespaces must begin with default-deny ingress and egress
  `NetworkPolicy` rules, then add only the pod-label and target-port paths
  required by this matrix. NetworkPolicy is a network boundary; it does not
  replace mTLS or RPC authorization.
- Workload identity and end-user identity are different. mTLS identifies the
  calling service. Existing gateway/session behavior remains responsible for
  authenticating the end user until a separately designed, verifiable user
  assertion is introduced.

## gRPC authorization matrix

| Authenticated caller | Target service | Permitted RPCs |
| --- | --- | --- |
| API Gateway | Identity | `SignUp`, `SignIn`, `SignOut`, `CurrentUser`, `VerifyEmail` |
| API Gateway | Tickets | `CreateTicket`, `UpdateTicket`, `GetTicket`, `ListTickets` |
| API Gateway | Orders | `CreateOrder`, `GetOrder`, `ListOrders`, `CancelOrder` |
| API Gateway | Payments | `CreatePayment` |
| Tickets | Orders | `EnsureTicketProjection` |
| Orders | Tickets | `ReserveTicketForOrder`, `ReleaseTicketReservation` |
| Payments | Orders | `StartPayment`, `ResolvePayment` |

No service may call an RPC outside this table. In particular, API Gateway must
not invoke Orders' internal projection or payment-resolution RPCs, and Tickets
must not invoke Orders' customer-facing order RPCs.

## Workload credentials

- Every workload has a distinct certificate identity. Shared client
  certificates and static shared API keys are prohibited.
- The deployment platform injects the trust bundle, certificate, and private
  key at runtime. Private keys are never committed to source control, embedded
  in container images, or passed through protobuf or gRPC metadata.
- Certificates must be short-lived and rotated by the selected platform PKI or
  service mesh. Services must reject untrusted, expired, or hostname/identity
  mismatched certificates.
- An authenticated caller that is not authorized for an RPC receives
  `PERMISSION_DENIED`. A client without a valid trusted certificate must not
  establish a gRPC connection.

## Kubernetes rollout gate

Before production traffic is accepted on Kubernetes:

1. Select a workload-identity and mTLS implementation, such as platform PKI or
   a service mesh, that can issue and rotate unique workload certificates.
2. Enforce the authorization matrix at the application gRPC interceptor layer
   or an equivalent Layer-7 policy engine.
3. Apply and test default-deny NetworkPolicies plus explicit allowed paths for
   the matrix and required DNS resolution.
4. Verify allowed calls succeed and each disallowed caller/RPC pair fails.
5. Update this policy before adding, removing, or repurposing a service RPC or
   service-to-service relationship.
