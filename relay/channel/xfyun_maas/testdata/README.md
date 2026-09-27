# Live MaaS fixtures

Captured on 2026-09-28 (Asia/Shanghai) from the official HTTPS endpoints with an
authorized test account. Requests used synthetic prompts only. Headers and APIKEYs
are not saved. Request/call IDs and timestamps are replaced consistently within
each fixture. The generated image bytes in `image.json` are replaced with a 1x1
test PNG; both live image models separately returned decodable 768x768 PNGs.

- Chat, Responses, Messages: `spark-x2.5-1.7b`, including function calls, cache
  usage, reasoning and native terminal events. `*-thinking.sse` retain every event.
- Vision: `xophunyuanocr`, reading the synthetic `ocr.png` (HELLO 123).
- Embedding: `xop3qwen8bembedding`, two identical inputs, dimensions=32.
- Rerank: `xop3qwen8breranker`, three synthetic documents.
- Image envelope/schema error: `xopzimageturbo`.
- `model-field-error.json`: HTTP 400 when sending model_id instead of model.
- `image-schema-error.json`: HTTP 200 with business code 10004 when omitting
  header.patch_id. A public model requires patch_id=["0"].

Offline regression tests replay these fixtures. The opt-in controller test
`TestXfyunMaasLiveGateway` additionally exercises the full gateway, aliases and
settlement using an in-memory database and synthetic prices. It requires
XFYUN_MAAS_LIVE_TEST=1 and XFYUN_MAAS_API_KEY, and calls all seven documented test
models. Live requests never run during the default test suite.
