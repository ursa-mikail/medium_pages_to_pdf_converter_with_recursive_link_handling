# 📰 Medium Harvester

> Convert any Medium page — plus all its linked articles — into offline PDFs,
> cross-linked so clicking a URL in the master PDF opens the matching local PDF.

---

## ✨ Features

| Feature | Detail |
|---|---|
| **Master page → PDF** | The URL you provide is saved as `main/main.pdf` |
| **1-level link crawl** | Every Medium article linked from the master page is also converted |
| **Cross-linked PDFs** | Links in the master PDF are rewritten to point to local sibling PDFs |
| **Cookie auth** | Paste your Medium browser cookies to unlock member-only & paywalled content |
| **Live log stream** | Server-Sent Events stream real-time progress to the UI |
| **Download panel** | All generated PDFs appear as download cards when the job completes |

---

## 🏗️ Architecture

```
medium-harvester/
├── backend/                  Go 1.22
│   ├── main.go               HTTP server entry point (chi router)
│   ├── internal/
│   │   ├── server/
│   │   │   └── handler.go    REST + SSE handlers
│   │   └── crawler/
│   │       ├── job.go        Chromium-based crawler & PDF engine
│   │       └── patcher.go    Link map sidecar writer
│   └── Dockerfile
│
├── frontend/                 React 18 + TypeScript + Vite
│   ├── src/
│   │   ├── App.tsx           Main UI component
│   │   ├── App.module.css    CSS Modules (dark editorial theme)
│   │   ├── types.ts          Shared TypeScript types
│   │   ├── index.css         Global styles & CSS variables
│   │   └── main.tsx          React entry point
│   ├── nginx.conf            Nginx reverse proxy config
│   └── Dockerfile
│
├── output/                   Generated PDFs land here (Docker volume)
│   └── <job-id>/
│       ├── main/
│       │   ├── main.pdf           Master page PDF
│       │   └── main.pdf.links.txt Link map sidecar
│       └── pages/
│           ├── page_01_<slug>.pdf  Linked article PDFs
│           └── ...
│
├── docker-compose.yml
├── up.sh                     Start all services
└── down.sh                   Stop + optional cleanup
```

---

## 🚀 Quick Start

### Prerequisites

- [Docker](https://docs.docker.com/get-docker/) ≥ 24
- [Docker Compose](https://docs.docker.com/compose/) v2+

### 1. Start

```bash
chmod +x up.sh down.sh
./up.sh
```

| Service | URL |
|---|---|
| Frontend UI | http://localhost:3000 |
| Backend API | http://localhost:8080 |

### 2. Use

1. Open **http://localhost:3000**
2. Paste a Medium article or list URL
3. *(Optional)* Switch to the **Auth Cookies** tab and add your Medium cookies
4. Click **Harvest**
5. Watch the live log stream
6. Download any PDF when the job finishes

### 3. Stop

```bash
./down.sh           # stop containers
./down.sh --clean   # stop + remove images + prune
```

---

## 🍪 Getting Your Medium Cookies

Medium uses cookies for authentication. To access member-only articles:

1. Log in to [medium.com](https://medium.com) in your browser
2. Open **DevTools** → **Application** → **Cookies** → `https://www.medium.com`
3. Copy the values for cookies such as:
   - `uid` — your user ID
   - `sid` — your session ID
   - `lightstep_guid/...` — optional telemetry (can skip)
4. Paste each name/value pair into the **Auth Cookies** tab in the UI

> **Privacy**: cookies are sent only to the backend container running locally.
> They are never stored on disk or logged.

---

## 🌐 API Reference

### `POST /api/harvest`

Start a new harvest job.

```jsonc
// Request
{
  "url": "https://medium.com/@author/article-slug-abc123",
  "cookies": [
    { "name": "uid",  "value": "...", "domain": ".medium.com", "path": "/" },
    { "name": "sid",  "value": "...", "domain": ".medium.com", "path": "/" }
  ]
}

// Response 200
{ "jobID": "job-1713900000000" }
```

### `GET /api/jobs/:jobID`

Poll job state + full log history.

```jsonc
{
  "id": "job-1713900000000",
  "url": "https://medium.com/...",
  "state": "done",          // queued | running | done | failed
  "logs": [
    { "time": "12:34:56", "level": "ok", "message": "Master page PDF saved → main/main.pdf" }
  ],
  "files": [
    { "name": "main.pdf",            "path": "main/main.pdf",          "url": "https://..." },
    { "name": "page_01_author_slug.pdf", "path": "pages/page_01_....pdf", "url": "https://..." }
  ]
}
```

### `GET /api/jobs/:jobID/stream`

Server-Sent Events stream. Each `data:` event is a `LogEntry` JSON object.
A final `event: done` signals completion.

### `GET /api/files/:jobID/*`

Download a generated PDF by its relative path (e.g. `main/main.pdf`).

---

## 🔧 Configuration

All configuration is via environment variables in `docker-compose.yml`:

| Variable | Default | Description |
|---|---|---|
| `PORT` | `8080` | Backend listen port |
| `OUTPUT_DIR` | `/app/output` | Where PDFs are written |

---

## 🛠️ Local Development (without Docker)

### Backend

```bash
cd backend
go mod tidy
OUTPUT_DIR=./output go run main.go
```

Requires Google Chrome or Chromium installed locally. Set `CHROME_BIN` if not on `$PATH`.

### Frontend

```bash
cd frontend
npm install
npm run dev
```

Proxies `/api/*` to `http://localhost:8080` (see `vite.config.ts`).

---

## 📁 Output Structure

```
output/
└── job-1713900000000/
    ├── main/
    │   ├── main.pdf            ← master page (A4, with background)
    │   └── main.pdf.links.txt  ← human-readable URL→PDF map
    └── pages/
        ├── page_01_author_my-article-title.pdf
        ├── page_02_author_another-article.pdf
        └── ...
```

The `links.txt` sidecar lists every URL that was rewritten so you can
manually open any PDF if you prefer not to use the cross-links.

---

## ⚠️ Notes & Limitations

- **Rate limiting**: Medium may throttle or block automated access.
  Using auth cookies helps significantly. Add a delay between runs if needed.
- **Paywalled content**: Member-only articles are unlocked via cookies;
  without them only the free preview renders.
- **Dynamic content**: The crawler waits for page load + lazy images,
  but very heavy pages may time out — increase `chromedp.Sleep` durations
  in `job.go` if needed.
- **PDF link patching**: Due to how Chrome's PDF generator works,
  in-PDF hyperlinks are recorded in a sidecar `.links.txt` file.
  A full binary PDF link rewrite can be added using `pdfcpu` (stub included).
- **One level of recursion**: Only URLs found on the master page are crawled.
  URLs found on those sub-pages are **not** crawled further.

---

## 📄 License

MIT
