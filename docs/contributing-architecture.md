# Contributing Architecture

This document explains how future contributors should add capabilities without touching unrelated parts of the system.

## If You Want to Add a Client Protocol

Examples:

- a new OpenAI endpoint variant
- a corporate custom agent protocol
- a new Anthropic-compatible client behavior

Add a Client Adapter.

Expected work:

1. implement protocol detection
2. parse request into Canonical IR
3. encode canonical unary responses
4. encode canonical stream events
5. encode errors in the client protocol's expected format
6. add conformance fixtures

You should not need to edit provider adapters.

## If You Want to Add a Provider

Examples:

- Gemini
- Ollama
- LM Studio
- vLLM
- private MaaS endpoint

Add a Provider Adapter.

Expected work:

1. build upstream requests from Canonical IR
2. apply provider-specific path/body conventions
3. parse unary responses into Canonical IR
4. parse streaming responses into canonical stream events
5. normalize upstream errors
6. declare capabilities
7. add conformance fixtures

You should not need to edit client adapters.

## If You Want to Add Authentication Behavior

Examples:

- custom header auth
- query token auth
- HMAC auth
- cloud signing

Add or extend an Upstream Auth Profile.

Expected work:

1. define config schema
2. implement secret resolution
3. apply headers/query/signature to upstream request
4. redact secret values from UI/API responses
5. add tests

You should not need to edit adapters unless the auth is inseparable from the provider protocol.

## If You Want to Add Routing Behavior

Examples:

- agent-based route
- project-based route
- fallback behavior
- latency-based selection

Add to Model Resolver or Channel Manager.

Expected work:

1. add config schema
2. compile config into runtime snapshot
3. keep data-plane reads immutable/low-contention
4. record routing decision in telemetry
5. add tests

## If You Want to Add Observability

Examples:

- JSONL logs
- OpenTelemetry
- Langfuse exporter
- local request timeline

Add a Telemetry Sink or Event Consumer.

Expected work:

1. consume normalized telemetry events
2. avoid blocking the request data plane
3. redact secrets
4. add config toggle
5. add tests

## Data Plane Rule

The hot path should read from immutable runtime snapshots.

Avoid adding global locks around request routing or streaming. Configuration updates should compile a new snapshot and atomically swap it in.

## UX Rule

Do not expose advanced config by default.

Every new advanced feature should answer:

- Can a local user ignore this safely?
- Does the UI hide it unless needed?
- Is there a short-form config for common usage?
