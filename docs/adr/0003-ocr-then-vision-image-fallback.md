# ADR-0003: Use OCR-then-Vision preprocessing for incompatible image requests

## Status

Superseded by ADR-0008

## Decision date

2026-07-20

## Recorded date

2026-08-02 (retrospective)

## Context

Clients may send image-bearing requests through a public model alias that resolves to a text-only model. Rejecting every such request prevents useful text extraction, while blindly forwarding images relies on undocumented upstream behavior. Protocol adapters are the wrong place to implement degradation because the policy must behave consistently across OpenAI Chat Completions, OpenAI Responses, and Anthropic Messages.

The pipeline must preserve conversation ordering, avoid turning image text into trusted instructions, bound resource use, and keep the original request available when a verified Vision fallback is selected.

## Decision

Implement image degradation as an opt-in `RequestPreprocessor` operating on Canonical IR after model resolution and before provider concurrency and upstream encoding.

The routing policy is:

- Requests without images continue directly.
- When the selected model effectively supports image input and its provider adapter can transport images, preserve the original image-bearing request.
- An `unknown` model capability does not automatically trigger OCR and is never eligible as a Vision fallback target. Compatibility mode may pass the original request to its originally resolved target.
- When the selected model is effectively `unsupported` and degradation is enabled, validate and decode embedded images under fixed count, per-image, total-byte, text-length, and timeout limits.
- Run the built-in OCR provider by default or an explicitly configured HTTP OCR provider. Replace each successfully recognized image block at its original message position with escaped text marked as untrusted user-derived content.
- Treat partial multi-image failure, empty or insufficient text, low confidence, provider failure, and timeout as unusable OCR according to deterministic thresholds.
- When OCR does not produce a usable request, an optional Vision fallback may receive the untouched original Canonical IR only if it resolves to a different target, its effective capability under ADR-0002 is `supported`, and its adapter transports images.
- Do not recursively run OCR or another fallback after switching to the Vision target.
- Invalid image format, MIME mismatch, or size-limit violations are client errors and cannot be bypassed through Vision fallback.

The preprocessor must not mutate the original Canonical IR. OCR behavior remains disabled unless explicitly enabled.

## Consequences

### Positive

- Text-only models can serve image requests when readable text is the important content.
- One policy works consistently across client and provider protocols.
- Image order and surrounding user text remain semantically aligned.
- The original image request remains available for a verified Vision fallback.
- Deterministic limits, errors, and telemetry make degradation observable and testable.
- The embedded OCR path works offline without Python, a system Tesseract installation, a second container, or a runtime model download.

### Negative

- OCR adds latency, CPU and memory use, and recognition errors before the LLM request.
- Extracted text loses non-textual visual information, layout fidelity, and some language coverage.
- The built-in WASM worker and optional external provider add lifecycle, timeout, and failure modes.
- A Vision fallback can change cost, latency, provider, and model behavior relative to the originally resolved target.
- Configuration and telemetry must describe both the original and effective target without confusing client authorization.

### Neutral

- Client authorization remains based on the originally requested public model; the fallback target is an internal routing detail.
- Successful OCR results may be cached in memory by image hash under bounded size and lifetime controls.
- The external HTTP OCR provider remains an extension point for accuracy, additional languages, or accelerated deployments.

## Alternatives considered

### Reject all images sent to text-only models

This is simple and explicit but was rejected as the only behavior because OCR can satisfy many document, screenshot, and code-image requests without requiring a more expensive vision model.

### Always route image requests to a vision model

This avoids OCR accuracy loss but was rejected because it changes cost and provider choice for requests that only need extracted text, and it requires every installation to configure a trusted vision target.

### Implement OCR inside each provider adapter

This was rejected because degradation is gateway policy, not wire encoding. Adapter-local OCR would duplicate behavior, mix credentials and parsing with transformation policy, and drift across protocols.

### Send original images to unknown targets

Compatibility passthrough may remain for the originally selected model, but unknown targets were rejected for fallback selection. A fallback deliberately changes the destination and therefore requires positive capability evidence.

### Download remote image URLs in the OCR path

This was rejected for the initial design because a secure downloader requires host allowlists, post-DNS private-address blocking, redirect revalidation, MIME sniffing, byte limits, and scheme restrictions.

## Security and operational considerations

- OCR accepts only supported embedded base64/data-URL image forms and MIME types under fixed count and decoded-byte limits.
- The OCR path does not fetch remote URLs. Direct Vision transport may preserve a remote URL only according to the existing provider adapter behavior.
- OCR text remains user content, is escaped, and is explicitly marked untrusted. It is never promoted to a system or developer instruction.
- Telemetry stores routing and aggregate transformation metadata but excludes images, image locations, OCR text, and secrets.
- External OCR authentication uses secret references and a dedicated timeout. OCR credentials are not passed to the LLM provider, and LLM credentials are not passed to the built-in OCR worker.
- The built-in OCR worker is serialized, receives only the image payload, runs in a short-lived process, and releases WASM memory on exit.
- The request is rejected before the upstream LLM call when validation is unsafe and no verified degradation path exists.
- Failure telemetry must identify the original and effective target without returning OCR content in error details.

## References

- [Architecture — Pipeline Hooks](../architecture.md)
- [Configuration Schema — OCR Image Fallback](../config-schema.md)
- [OCR Image Fallback Design](../ocr-fallback-design.md)
- [`0326666` — design OCR image fallback](https://github.com/a448582655/vibe-proxy/commit/0326666)
- [`68b781f` — add HTTP OCR image fallback](https://github.com/a448582655/vibe-proxy/commit/68b781f)
- [`052207e` — add OCR-to-Vision fallback telemetry](https://github.com/a448582655/vibe-proxy/commit/052207e)
- [`6f188b2` — add the OCR fallback control plane](https://github.com/a448582655/vibe-proxy/commit/6f188b2)
- [`73ef6c1` — add embedded built-in OCR](https://github.com/a448582655/vibe-proxy/commit/73ef6c1)
- [ADR-0001: Canonical IR adapter boundary](0001-canonical-ir-adapter-boundary.md)
- [ADR-0002: Model capability resolution precedence](0002-model-capability-resolution-precedence.md)
