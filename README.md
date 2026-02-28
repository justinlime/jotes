# Jotes

Jotes is a lightweight Go web application for browsing and rendering notes from a single directory on disk. It focuses on the existing server-rendered interface, note previews, and fast local navigation.

## Features

- Single root notes directory exposed at `/`
- Server-rendered directory browsing with breadcrumbs
- Preview pages for Markdown, Org, HTML, plain text, code, images, and generic files
- Bundled CodeMirror 6 editor with syntax highlighting for common text and code formats
- Cookie-persisted editor settings, including Vim mode, line wrapping, and line numbers, with no server-side preference storage
- Unified backend search for filename and content matches
- Local-only bundled assets with no external CDN dependencies
- Filesystem watcher that keeps cached directory listings fresh when notes are added, removed, or renamed

## Configuration

Jotes supports four runtime settings. Command-line flags take precedence over environment variables.

| Flag | Environment | Default | Description |
| --- | --- | --- | --- |
| `--host` | `JOTES_HOST` | `0.0.0.0` | Interface or hostname to bind |
| `--port` | `JOTES_PORT` | `7887` | HTTP port to listen on |
| `--dir` | `JOTES_DIR` | — | Single root directory to expose as `/` |
| `--data-dir` | `JOTES_DATA_DIR` | `/etc/jotes` | Runtime data directory for files such as `jotes.db` |

When running Jotes as a non-root user, set `--data-dir` or `JOTES_DATA_DIR` to a writable location.

A single positional argument may be used instead of `--dir`:

```bash
jotes --data-dir ./jotes-data /srv/notes
```

## Editor asset build

Jotes bundles its CodeMirror 6 editor into `static/js/editor.js` and then embeds that built asset into the Go binary. Rebuild the editor bundle whenever you change anything under `frontend/editor/` or update the JavaScript dependencies:

```bash
npm install
npm run build:editor
```

The editor settings menu stores Vim mode and other display preferences in browser cookies only. Nothing about those settings is persisted on the server.

## Running locally

Serve a notes directory with default host and port:

```bash
jotes --data-dir ./jotes-data --dir /srv/notes
```

Bind to a custom interface and port:

```bash
jotes --host 127.0.0.1 --port 8080 --data-dir ./jotes-data --dir /srv/notes
```

Use environment variables instead of flags:

```bash
export JOTES_HOST=0.0.0.0
export JOTES_PORT=7887
export JOTES_DATA_DIR=./jotes-data
export JOTES_DIR=/srv/notes
jotes
```

## Docker

Build the image:

```bash
docker build -t jotes .
```

Run the container:

```bash
docker run -d \
  --name jotes \
  -p 7887:7887 \
  -v /srv/notes:/data/notes \
  -v /srv/jotes-data:/var/lib/jotes \
  -e JOTES_DATA_DIR=/var/lib/jotes \
  -e JOTES_DIR=/data/notes \
  jotes
```

Example Compose service:

```yaml
services:
  jotes:
    build: .
    container_name: jotes
    ports:
      - "7887:7887"
    volumes:
      - /srv/notes:/data/notes
      - /srv/jotes-data:/var/lib/jotes
    environment:
      JOTES_HOST: 0.0.0.0
      JOTES_PORT: 7887
      JOTES_DATA_DIR: /var/lib/jotes
      JOTES_DIR: /data/notes
```

## Notes on previews

- Markdown, Org, and HTML files are rendered as documents when possible.
- Other text-like files use a read-only CodeMirror preview.
- Images are shown inline.
- Unknown binary formats fall back to a metadata card.

## Filesystem freshness

Jotes watches the served directory tree with `inotify` so cached directory listings stay fresh when files are created, removed, or renamed. Search requests read current filesystem state directly, so newly added notes become searchable without waiting for a prebuilt index. Jotes uses `ripgrep` (`rg`) for unified filename and content search, so non-container installs should ensure `rg` is available on `PATH`. Large trees may require increasing `fs.inotify.max_user_watches`.

## Build and test policy

Builds and tests must run from `/tmp` so the repository stays free of generated artifacts. A typical workflow is:

```bash
rm -rf /tmp/jotes-build
mkdir -p /tmp/jotes-build
cp -R . /tmp/jotes-build/
cd /tmp/jotes-build
npm install
npm run build:editor
go build ./...
go test ./...
```
