# media-proxy

A fast HTTP proxy server that fetches media files from an upstream HTTP source and transforms them on-the-fly using [libvips](https://www.libvips.org/). Supports image resizing, format conversion, PDF rendering, placeholder generation, HMAC-signed URLs, and filesystem caching.

## Features

- **Image transformation** — resize, crop, and convert images (JPEG, PNG, AVIF, WebP)
- **Content-type negotiation** — auto-selects output format from the request `Accept` header
- **PDF support** — render specific pages at custom DPI
- **Metadata endpoint** — returns image dimensions, page count, format, and placeholder hashes
- **Placeholder generation** — [BlurHash](https://github.com/woltapp/blurhash), [ThumbHash](https://evanw.github.io/thumbhash/), and tiny WebP placeholders (PotatoWebp)
- **HMAC-SHA1 URL signing** — protects endpoints from unauthorized use
- **Two-level filesystem cache** — separate caches for original fetched files and processed results
- **Prometheus metrics** — request duration, active requests, loader duration, libvips stats

## Quick Start

```bash
docker run -p 8080:8080 -p 8081:8081 \
  -e BASE_URL=https://your-upstream-storage.example.com/ \
  -e SECRET=your-secret-key \
  blesswinsamuel/media-proxy
```

Then request a transformed image:

```
GET http://localhost:8080/{signature}/media/path/to/image.jpg?resize[width]=400&outputFormat=webp
```

## Configuration

All options can be set via environment variable or CLI flag.

| Flag | Env var | Default | Description |
|------|---------|---------|-------------|
| `--env` | `GO_ENV` | `development` | Runtime environment |
| `--log-level` | `LOG_LEVEL` | `info` | Log level (`trace`, `debug`, `info`, `warn`, `error`, `fatal`, `panic`) |
| `--config` | `CONFIG` | — | Path to an INI config file |
| `--host` | `HOST` | `localhost` | Host/address to listen on |
| `--port` | `PORT` | `8080` | Port for the main proxy server |
| `--metrics-port` | `METRICS_PORT` | `8081` | Port for the health/metrics server |
| `--base-url` | `BASE_URL` | — | Upstream base URL that media paths are appended to |
| `--enable-loader-cache` | `ENABLE_LOADER_CACHE` | `true` | Cache original fetched files to disk |
| `--enable-result-cache` | `ENABLE_RESULT_CACHE` | `true` | Cache processed results to disk |
| `--cache-dir` | `CACHE_DIR` | `/tmp/cache` | Root directory for on-disk caches |
| `--enable-unsafe` | `ENABLE_UNSAFE` | `false` | Disable signature validation (development only) |
| `--secret` | `SECRET` | — | HMAC secret used to validate URL signatures. Required unless `ENABLE_UNSAFE=true` |
| `--concurrency` | `CONCURRENCY` | `8` | Maximum number of concurrent transform requests |

Environment files are loaded in the following order (later files take precedence):
`.env.{GO_ENV}.local` → `.env.local` → `.env.{GO_ENV}` → `.env`

## API Reference

### Media transform

```
GET /{signature}/media/{path}[?params]
```

Fetches the file at `{BASE_URL}/{path}`, applies the requested transformations, and returns the processed image.

| Query parameter | Type | Description |
|-----------------|------|-------------|
| `raw` | `bool` | Return the original file without any processing |
| `read[dpi]` | `int` | DPI to use when rasterizing (e.g. PDFs) |
| `read[page]` | `int` | 1-based page number to render (multi-page files/PDFs) |
| `resize[width]` | `int` | Target width in pixels (auto-calculates height if omitted) |
| `resize[height]` | `int` | Target height in pixels (auto-calculates width if omitted) |
| `resize[crop]` | `string` | Smart-crop gravity: `none`, `centre`, `entropy`, `attention`, `low`, `high` |
| `resize[size]` | `string` | Resize constraint: `both` (default), `up`, `down`, `force` |
| `outputFormat` | `string` | Output format: `jpeg`, `png`, `avif`, `webp`. Defaults to format negotiated via `Accept` header |

**Response headers:**
- `Content-Type` — reflects the output format
- `Cache-Control: public, max-age=31536000, immutable`

### Metadata

```
GET /{signature}/metadata/{path}[?params]
```

Returns a JSON object with image metadata and optional placeholder data.

| Query parameter | Type | Description |
|-----------------|------|-------------|
| `read[dpi]` | `int` | DPI to use when loading (e.g. PDFs) |
| `read[page]` | `int` | 1-based page number |
| `thumbhash` | `bool` | Include a [ThumbHash](https://evanw.github.io/thumbhash/) (base64) |
| `blurhash` | `bool` | Include a [BlurHash](https://github.com/woltapp/blurhash) string |
| `potatowebp` | `bool` | Include a tiny WebP placeholder (base64) |

**Example response:**

```json
{
  "width": 1920,
  "height": 1080,
  "noOfPages": 1,
  "format": "jpeg",
  "thumbhash": "3OcRJYB4h4h...",
  "blurhash": "LEHV6nWB2yk8pyo0...",
  "potatowebp": "UklGRlYAAABXRUJQ..."
}
```

## URL Signing

Each request must include a valid HMAC-SHA1 signature in the URL path (unless `ENABLE_UNSAFE=true`).

**Algorithm:**

1. Build the signing input: `{requestType}/{path}` — e.g. `media/photos/cat.jpg?resize[width]=400`
2. Compute `HMAC-SHA1(secret, input)`, base64url-encode the result, and take the first 40 characters
3. Use that value as the `{signature}` path segment

**JavaScript example:**

```js
const crypto = require('crypto')

function sign(secret, requestType, path, query = '') {
  const input = query
    ? `${requestType}/${path}?${query}`
    : `${requestType}/${path}`
  return crypto
    .createHmac('sha1', secret)
    .update(input)
    .digest('base64')
    .slice(0, 40)
    .replace(/\+/g, '-')
    .replace(/\//g, '_')
}

const sig = sign('my-secret', 'media', 'photos/cat.jpg', 'resize[width]=400')
// → use as: GET /{sig}/media/photos/cat.jpg?resize[width]=400
```

## Caching

When caching is enabled, files are stored under `CACHE_DIR`:

| Cache | Path | Controlled by |
|-------|------|---------------|
| Original files | `{CACHE_DIR}/original/` | `ENABLE_LOADER_CACHE` |
| Processed results | `{CACHE_DIR}/result/` | `ENABLE_RESULT_CACHE` |
| Metadata results | `{CACHE_DIR}/metadata/` | `ENABLE_RESULT_CACHE` |

Cache keys are derived from the media path and query parameters. Files are stored indefinitely — clear the directory to invalidate the cache.

## Observability

A separate HTTP server runs on `METRICS_PORT` (default `8081`):

| Path | Description |
|------|-------------|
| `GET /health` | Returns `200 OK` when the server is ready |
| `GET /metrics` | Prometheus metrics endpoint |

**Exposed Prometheus metrics:**

| Metric | Type | Description |
|--------|------|-------------|
| `media_proxy_request_duration_seconds` | Histogram | Duration of proxy requests, labelled by method, path, status code |
| `media_proxy_active_requests` | Gauge | Number of requests currently being processed |
| `media_proxy_active_conns` | Gauge | Number of active TCP connections |
| `media_proxy_network_conns_count_total` | Gauge | TCP connection count by state |
| `media_proxy_loader_duration_seconds` | Histogram | Duration of upstream fetch requests |
| `media_proxy_loader_response_size_bytes` | Histogram | Size of upstream responses in bytes |

## Cache Warmer

The `cache-warmer` utility pre-populates the cache by crawling a website's sitemap and fetching all media URLs served by the proxy.

```bash
WEBSITE_URL=https://example.com ASSETS_URL=https://media.example.com go run ./cache-warmer
```

| Env var | Description |
|---------|-------------|
| `WEBSITE_URL` | Base URL of the website. The warmer fetches `{WEBSITE_URL}/sitemap.xml` to discover pages |
| `ASSETS_URL` | Base URL of the media proxy. Only URLs starting with this prefix are fetched |

## Development

**Prerequisites:** [libvips](https://www.libvips.org/) must be installed.

```bash
# macOS
brew install vips

# Debian/Ubuntu
apt-get install libvips-dev
```

**Run locally:**

```bash
SECRET=mylocalsecret go run .
# or, using Task:
task start
```

**Run tests:**

```bash
task test        # unit tests
task test:e2e    # end-to-end tests
```

**Build multi-arch Docker image:**

```bash
task podman-buildx
```

The Docker image is based on Alpine and includes `vips` and `vips-poppler` for PDF support.

