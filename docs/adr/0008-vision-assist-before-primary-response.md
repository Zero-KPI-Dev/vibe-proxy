# ADR-0008: Use Vision as bounded evidence assistance before the primary response

## Status

Accepted

## Decision date

2026-08-03

## Recorded date

2026-08-03

## Supersedes

[ADR-0003](0003-ocr-then-vision-image-fallback.md)

## Context

ADR-0003 allowed an OCR failure to move the untouched Canonical request to a Vision model. That takeover behavior is useful for short, image-centric requests, but it is unsafe as the default for agent conversations. The primary text model may support a 1M-token context and an established provider prompt cache while the configured Vision model supports only 16K tokens. Moving the entire request can therefore fail validation, discard the primary model's behavioral characteristics, and produce a high time-to-first-token because the Vision provider has no reusable cache for the long conversation.

The gateway still needs non-textual evidence when built-in or external OCR produces no usable text. It also needs to keep explicit takeover available for users who prefer the Vision model to answer directly.

## Decision

`vision_fallback_model` remains the configured image-capable helper target. A separate `vision_fallback_strategy` selects one of three behaviors:

- `assist` is the default, including for existing configurations that omit the field.
- `takeover` preserves ADR-0003's direct route switch as an explicit opt-in.
- `reject` performs no Vision invocation after unusable OCR.

In `assist` mode the gateway must:

1. keep the original resolved provider/model as the final answering target;
2. build a bounded unary Vision helper request containing only the image blocks and text from the latest image-bearing user message, never the full conversation or tools;
3. ask the Vision model for concise factual evidence and treat its output as untrusted user-derived data;
4. replace the original image blocks in a cloned Canonical request with a protected `<vibe-proxy-vision>` evidence block at the image position;
5. send the full original conversation plus that evidence to the originally resolved model;
6. place safety text adjacent to the replacement evidence instead of rewriting the leading system prompt, preserving the historical prompt prefix for provider cache reuse;
7. cache successful evidence in a bounded in-memory LRU keyed by prompt version, Vision target, image digests, and local image question.

The helper request has independent input-text and output-token limits. It is always unary even when the primary request streams. It participates in the configured provider's timeout, authentication, and concurrency limit.

In `takeover` mode the untouched image-bearing Canonical request becomes the primary Vision request. When models.dev exposes an input or context limit, vibe-proxy applies a conservative preflight estimate and rejects clearly oversized requests with `vision_takeover_context_exceeded` instead of sending a request known not to fit. Unknown limits do not make an explicit takeover invalid.

The trace model records OCR invocation input/result, Vision helper request/result, the effective Canonical request after evidence injection, the final primary upstream request, and the primary Canonical response when structured/raw capture is enabled. Metadata-only capture remains the default.

## Consequences

### Positive

- Long agent context and the original model's answer behavior are retained.
- A small-context Vision model only sees the image-local task it can handle.
- The primary provider can reuse the unchanged historical prompt prefix.
- Explicit takeover and reject policies remain available.
- Observability makes it unambiguous which model analyzed the image and which model answered.

### Negative

- Assist uses two LLM calls after OCR failure, adding latency and cost.
- Vision evidence can omit details needed by the primary model.
- The first evidence cache is process-local and is lost on restart or configuration reload.
- Approximate takeover token preflight cannot replace provider-native tokenization.

### Neutral

- OCR success still replaces images with OCR evidence and calls the original model.
- Client authorization remains based on the originally requested public model.
- The Vision helper uses an existing configured provider auth profile; no new credential type is introduced.

## Alternatives considered

### Always let the Vision model answer

Rejected as the default because it moves the complete context to a potentially smaller model and forfeits primary-provider cache locality. Retained as explicit `takeover`.

### Send the complete conversation to Vision, then return evidence to the primary model

Rejected because it retains the context-limit and cache problems while also paying for two calls.

### Run OCR only and reject non-text images

Available through `reject`, but not selected as the default because it cannot provide even limited object, layout, or scene evidence.

### Add Vision evidence as a new system message

Rejected because changing the leading system prefix invalidates provider prompt caches and raises the trust level of model-generated evidence.

## Security and operational considerations

- Vision output is escaped, wrapped, and kept at user-content trust level.
- Instructions visible inside an image and instructions emitted by the helper are not system or developer instructions.
- Helper requests omit conversation history, tools, response format, session metadata, and unrelated user messages.
- Payload capture remains policy-controlled, size-bounded, redacted, local, and disabled beyond metadata by default.
- Provider authentication is applied only after capture of the sanitized helper wire body, so secrets do not enter diagnostics.
- Malformed or over-limit images remain client errors and cannot bypass validation through assist or takeover.

## References

- [Architecture — Pipeline Hooks](../architecture.md)
- [Configuration Schema — OCR Image Fallback](../config-schema.md)
- [OCR Image Fallback Design](../ocr-fallback-design.md)
- [ADR-0001: Canonical IR adapter boundary](0001-canonical-ir-adapter-boundary.md)
- [ADR-0002: Model capability resolution precedence](0002-model-capability-resolution-precedence.md)
- [ADR-0007: Local-first observability trace model](0007-local-first-observability-trace-model.md)
