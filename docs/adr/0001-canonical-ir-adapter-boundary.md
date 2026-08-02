# ADR-0001: Use Canonical IR between client and provider adapters

## Status

Accepted

## Decision date

2026-07-05

## Recorded date

2026-08-02 (retrospective)

## Context

vibe-proxy accepts multiple client protocols and forwards requests to providers that may use different protocols. Translating every client protocol directly to every provider protocol would require a separate implementation for each protocol pair, duplicate streaming behavior, and make routing, preprocessing, and telemetry depend on wire formats.

The gateway also needs to preserve tools, images, usage, reasoning, errors, and provider-specific fields when the downstream and upstream protocols do not have identical concepts.

## Decision

Use a protocol-neutral Canonical Intermediate Representation (Canonical IR) as the internal boundary between client and provider adapters.

- Client adapters detect and decode external requests into canonical requests, then encode canonical responses, errors, and stream events into the client protocol.
- Provider adapters build upstream requests from Canonical IR, then normalize upstream responses, errors, and streaming frames into canonical structures.
- Routing, authentication, preprocessing, channel selection, streaming orchestration, and telemetry operate on canonical structures rather than protocol-specific payloads.
- Provider adapters emit canonical stream events and client adapters render them. Shared streaming behavior such as time-to-first-token tracking, trailing usage, accumulation, disconnect handling, and backpressure belongs outside individual protocol pairs.
- Common semantics become typed canonical fields. Useful protocol-specific information that cannot be normalized is preserved in namespaced vendor extensions.
- Unsupported or lossy transformations are explicit and observable rather than silently discarded.

Client adapters do not select providers or apply upstream authentication. Provider adapters do not render client-facing protocol responses.

## Consequences

### Positive

- Supporting `N` client protocols and `M` provider protocols requires approximately `N + M` adapters instead of `N × M` pairwise translators.
- Routing, preprocessing, telemetry, and stream handling can be implemented once against stable internal structures.
- Protocol responsibilities are testable through adapter conformance fixtures.
- Cross-protocol requests can preserve normalized tools, images, usage, and errors without binding the runtime to one vendor.

### Negative

- Canonical IR becomes a compatibility surface that must evolve carefully.
- A concept with no faithful canonical representation may require a new typed field or an explicitly lossy transformation.
- Every adapter must distinguish protocol parsing from gateway policy, even when a vendor SDK encourages combining them.
- Normalization can hide useful provider detail unless vendor extensions and raw diagnostic data are maintained deliberately.

### Neutral

- Vendor-specific capabilities remain possible through namespaced extensions, but consumers must not assume another protocol can reproduce them.
- Synthetic unary-to-stream or stream-to-unary conversion remains a shared runtime policy, not an adapter-specific guarantee.

## Alternatives considered

### Direct client-to-provider translators

Each supported protocol pair could have its own translator. This was rejected because the number of implementations grows multiplicatively, shared behaviors drift between pairs, and every new protocol requires changes across unrelated providers or clients.

### Use one provider's native schema as the internal representation

The gateway could normalize everything into OpenAI, Anthropic, or another vendor's payload. This was rejected because the chosen vendor's semantics would become privileged, unsupported concepts would be lossy by default, and provider-specific wire concerns would leak into routing and preprocessing.

### Operate as a raw reverse proxy

Passing bytes through with minimal parsing would reduce normalization code. This was rejected because cross-protocol translation, model resolution, safe preprocessing, normalized telemetry, and consistent error handling all require a protocol-aware internal representation.

## Security and operational considerations

- Parsing untrusted client payloads remains inside client adapters; provider adapters never receive raw client HTTP requests.
- Upstream authentication and secret resolution stay outside adapters so protocol extensions cannot select or expose credentials.
- Vendor extensions must be namespaced and treated as untrusted data. They do not bypass routing, authorization, image validation, or secret-redaction policy.
- Conformance tests are required because a canonical schema change can affect every adapter even when the compiler accepts the change.
- Lossy transformations and unsupported fields must be visible in telemetry without logging secrets or sensitive message content.

## References

- [Architecture](../architecture.md)
- [Canonical IR](../canonical-ir.md)
- [Adapter SDK](../adapter-sdk.md)
- [`ebca59b` — introduce the canonical adapter runtime pipeline](https://github.com/a448582655/vibe-proxy/commit/ebca59b)
- [`6d157af` — add Responses and Anthropic client adapters](https://github.com/a448582655/vibe-proxy/commit/6d157af)
- [`386d8cc` — introduce the canonical stream engine](https://github.com/a448582655/vibe-proxy/commit/386d8cc)
