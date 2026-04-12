package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/medium-harvester/backend/internal/crawler"
)

type Handler struct {
	outputDir string
	jobs      map[string]*crawler.Job
	mu        sync.RWMutex

	// cookie-bridge sessions: token → cookies (nil = pending)
	cookieSessions   map[string][]crawler.Cookie
	cookieSessionsMu sync.RWMutex
}

func NewHandler(outputDir string) *Handler {
	return &Handler{
		outputDir:      outputDir,
		jobs:           make(map[string]*crawler.Job),
		cookieSessions: make(map[string][]crawler.Cookie),
	}
}

type HarvestRequest struct {
	URL     string           `json:"url"`
	Cookies []crawler.Cookie `json:"cookies"`
}

func (h *Handler) Harvest(w http.ResponseWriter, r *http.Request) {
	var req HarvestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.URL == "" {
		http.Error(w, "url is required", http.StatusBadRequest)
		return
	}

	jobID := fmt.Sprintf("job-%d", time.Now().UnixMilli())
	jobDir := filepath.Join(h.outputDir, jobID)

	job := crawler.NewJob(jobID, req.URL, req.Cookies, jobDir)

	h.mu.Lock()
	h.jobs[jobID] = job
	h.mu.Unlock()

	go job.Run()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"jobID": jobID})
}

func (h *Handler) StopJob(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobID")
	h.mu.RLock()
	job, ok := h.jobs[jobID]
	h.mu.RUnlock()
	if !ok {
		http.Error(w, "job not found", http.StatusNotFound)
		return
	}
	job.Stop()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "stopping"})
}

func (h *Handler) GetJob(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobID")
	h.mu.RLock()
	job, ok := h.jobs[jobID]
	h.mu.RUnlock()
	if !ok {
		http.Error(w, "job not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(job.Status())
}

func (h *Handler) StreamJob(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobID")
	h.mu.RLock()
	job, ok := h.jobs[jobID]
	h.mu.RUnlock()
	if !ok {
		http.Error(w, "job not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, _ := w.(http.Flusher)

	logCh := job.Subscribe()
	defer job.Unsubscribe(logCh)

	for {
		select {
		case msg, open := <-logCh:
			if !open {
				fmt.Fprintf(w, "event: done\ndata: {}\n\n")
				if flusher != nil {
					flusher.Flush()
				}
				return
			}
			data, _ := json.Marshal(msg)
			fmt.Fprintf(w, "data: %s\n\n", data)
			if flusher != nil {
				flusher.Flush()
			}
		case <-r.Context().Done():
			return
		}
	}
}

func (h *Handler) ServeFile(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobID")
	rest := chi.URLParam(r, "*")
	h.mu.RLock()
	_, ok := h.jobs[jobID]
	h.mu.RUnlock()
	if !ok {
		http.Error(w, "job not found", http.StatusNotFound)
		return
	}

	filePath := filepath.Join(h.outputDir, jobID, filepath.Clean("/"+rest))
	if !strings.HasPrefix(filePath, h.outputDir) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filepath.Base(filePath)))
	http.ServeFile(w, r, filePath)
}

// ── Cookie bridge ─────────────────────────────────────────────────────────────
//
// Flow:
//  1. Frontend calls POST /api/cookie-session  → gets back { token, loginURL }
//  2. Frontend opens loginURL in a new tab
//     loginURL = https://medium.com/m/signin?source=...
//     After login Medium redirects the user back to medium.com (their normal flow).
//  3. User then clicks "Done – I've logged in" in the new tab, which navigates to
//     GET /api/cookie-capture?token=<token>
//     That page uses JS to read document.cookie (won't have httpOnly cookies, but
//     uid is readable) OR the user pastes the confirmation.
//
// Because medium.com sets httpOnly on sid we can't read it via JS.
// Better approach: serve a small bridge page at /api/cookie-bridge?token=<token>
// that itself is on the SAME origin as the backend, so it can't read medium cookies.
//
// The REAL correct approach for a containerised app with no display:
// Serve a page the user opens in their browser. That page:
//   1. Opens medium.com in a popup / redirects to medium.com/m/signin
//   2. After login, asks the user to click a "Send cookies" button
//   3. The button does fetch('/api/cookie-capture', {credentials:'include'}) — but
//      medium.com cookies won't be sent to our origin.
//
// The only reliable no-display approach is: ask the user to paste their cookie
// string from the browser, which is exactly what the raw-string mode does.
// So we replace the "auto fetch" with a guided helper page that auto-opens
// the right DevTools panel URL and pre-fills instructions, making it much faster.
//
// We serve a dedicated helper page at GET /api/cookie-helper that:
//   - Opens medium.com/m/signin in a new tab
//   - Shows step-by-step instructions with a copy button
//   - Has a textarea where the user pastes the cookie string
//   - POSTs it back to POST /api/cookie-session/{token}/submit
// The frontend polls GET /api/cookie-session/{token} until cookies arrive.

// CookieSessionStart creates a pending session and returns a token + helper URL.
func (h *Handler) CookieSessionStart(w http.ResponseWriter, r *http.Request) {
	token := fmt.Sprintf("cs-%d", time.Now().UnixNano())

	h.cookieSessionsMu.Lock()
	h.cookieSessions[token] = nil // nil = pending
	h.cookieSessionsMu.Unlock()

	// Clean up after 10 minutes
	go func() {
		time.Sleep(10 * time.Minute)
		h.cookieSessionsMu.Lock()
		delete(h.cookieSessions, token)
		h.cookieSessionsMu.Unlock()
	}()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"token":     token,
		"helperURL": "/api/cookie-helper?token=" + token,
	})
}

// CookieSessionPoll returns cookies if they've been submitted, or 202 if pending.
func (h *Handler) CookieSessionPoll(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")

	h.cookieSessionsMu.RLock()
	cookies, exists := h.cookieSessions[token]
	h.cookieSessionsMu.RUnlock()

	if !exists {
		http.Error(w, "session not found or expired", http.StatusNotFound)
		return
	}
	if cookies == nil {
		w.WriteHeader(http.StatusAccepted) // 202 = still pending
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"cookies": cookies})
}

// CookieSessionSubmit receives the pasted cookie string from the helper page.
func (h *Handler) CookieSessionSubmit(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")

	h.cookieSessionsMu.RLock()
	_, exists := h.cookieSessions[token]
	h.cookieSessionsMu.RUnlock()
	if !exists {
		http.Error(w, "session not found or expired", http.StatusNotFound)
		return
	}

	var body struct {
		Raw string `json:"raw"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Raw == "" {
		http.Error(w, "raw cookie string required", http.StatusBadRequest)
		return
	}

	cookies := parseCookieString(body.Raw)

	h.cookieSessionsMu.Lock()
	h.cookieSessions[token] = cookies
	h.cookieSessionsMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"cookies": cookies})
}

// CookieHelper serves the guided in-browser helper page.
func (h *Handler) CookieHelper(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, cookieHelperHTML, token)
}

// parseCookieString parses "name=value; name2=value2" into Cookie structs.
func parseCookieString(raw string) []crawler.Cookie {
	var out []crawler.Cookie
	for _, part := range strings.Split(raw, ";") {
		part = strings.TrimSpace(part)
		idx := strings.IndexByte(part, '=')
		if idx < 1 {
			continue
		}
		name := strings.TrimSpace(part[:idx])
		value := strings.TrimSpace(part[idx+1:])
		if name == "" {
			continue
		}
		out = append(out, crawler.Cookie{
			Name:   name,
			Value:  value,
			Domain: ".medium.com",
			Path:   "/",
			Secure: true,
		})
	}
	return out
}

// cookieHelperHTML is the guided helper page served to the user's real browser.
// %s is replaced with the session token.
const cookieHelperHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Medium Harvester – Cookie Helper</title>
<style>
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body { font-family: system-ui, sans-serif; background: #0d0d1f; color: #d0d0f0; min-height: 100vh; display: flex; align-items: center; justify-content: center; padding: 2rem; }
  .card { background: #13132a; border: 1px solid #2a2a55; border-radius: 14px; padding: 2rem 2.5rem; max-width: 520px; width: 100%%; }
  h1 { font-size: 1.25rem; color: #fff; margin-bottom: 0.4rem; }
  .sub { color: #8888bb; font-size: 0.88rem; margin-bottom: 1.8rem; }
  ol { padding-left: 1.3rem; display: flex; flex-direction: column; gap: 0.9rem; margin-bottom: 1.8rem; }
  li { font-size: 0.92rem; line-height: 1.5; color: #c0c0e8; }
  code { background: #1e1e40; border: 1px solid #3a3a66; border-radius: 4px; padding: 0.1em 0.45em; font-size: 0.85em; color: #99aaff; }
  .btn-row { display: flex; gap: 0.75rem; margin-bottom: 1.5rem; flex-wrap: wrap; }
  button { cursor: pointer; border: none; border-radius: 7px; font-size: 0.88rem; font-weight: 600; padding: 0.6rem 1.2rem; transition: opacity 0.15s; }
  .btn-primary { background: #4444cc; color: #fff; }
  .btn-primary:hover { opacity: 0.85; }
  .btn-copy { background: #1e1e40; border: 1px solid #4444aa; color: #aabbff; }
  .btn-copy:hover { background: #28285a; }
  textarea { width: 100%%; background: #0a0a1e; border: 1.5px solid #2a2a55; border-radius: 8px; padding: 0.75rem; color: #d0d0f0; font-family: monospace; font-size: 0.82rem; resize: vertical; min-height: 90px; outline: none; transition: border-color 0.15s; }
  textarea:focus { border-color: #5555dd; }
  .submit-btn { width: 100%%; padding: 0.8rem; background: linear-gradient(135deg, #3b3baa, #5555cc); color: #fff; font-size: 1rem; font-weight: 700; border-radius: 8px; margin-top: 0.75rem; letter-spacing: 0.04em; }
  .submit-btn:hover:not(:disabled) { opacity: 0.88; }
  .submit-btn:disabled { opacity: 0.4; cursor: not-allowed; }
  .status { margin-top: 1rem; font-size: 0.88rem; min-height: 1.4em; }
  .ok { color: #44dd88; }
  .err { color: #ff6677; }
</style>
</head>
<body>
<div class="card">
  <h1>🍪 Get your Medium cookies</h1>
  <p class="sub">Follow these steps — takes about 30 seconds</p>
  <ol>
    <li>Click <strong>Open Medium login</strong> below — log in if you haven't already</li>
    <li>Once logged in, open <strong>DevTools</strong> with <code>F12</code> (Windows/Linux) or <code>Cmd+Option+I</code> (Mac)</li>
    <li>Click the <strong>Console</strong> tab and paste this command, then press Enter:</li>
  </ol>
  <div class="btn-row">
    <button class="btn-primary" onclick="window.open('https://medium.com','_blank')">Open Medium login ↗</button>
    <button class="btn-copy" id="copyBtn" onclick="copyCmd()">Copy console command</button>
  </div>
  <textarea id="cookiePaste" placeholder="Paste your cookie string here…&#10;e.g.  uid=abc123; sid=xyz789; __cfruid=..."></textarea>
  <button class="submit-btn" id="submitBtn" onclick="submit()" disabled>Send cookies to harvester</button>
  <p class="status" id="status"></p>
</div>
<script>
const TOKEN = '%s';
const CMD = 'copy(document.cookie)';

function copyCmd() {
  navigator.clipboard.writeText(CMD).then(() => {
    document.getElementById('copyBtn').textContent = '✓ Copied!';
    setTimeout(() => document.getElementById('copyBtn').textContent = 'Copy console command', 2000);
  });
}

document.getElementById('cookiePaste').addEventListener('input', function() {
  document.getElementById('submitBtn').disabled = this.value.trim() === '';
});

async function submit() {
  const raw = document.getElementById('cookiePaste').value.trim();
  if (!raw) return;
  const btn = document.getElementById('submitBtn');
  btn.disabled = true;
  btn.textContent = 'Sending…';
  try {
    const res = await fetch('/api/cookie-session/' + TOKEN + '/submit', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ raw })
    });
    if (res.ok) {
      document.getElementById('status').className = 'status ok';
      document.getElementById('status').textContent = '✅ Cookies sent! You can close this tab.';
      btn.textContent = '✓ Done';
    } else {
      throw new Error(await res.text());
    }
  } catch(e) {
    document.getElementById('status').className = 'status err';
    document.getElementById('status').textContent = 'Error: ' + e.message;
    btn.disabled = false;
    btn.textContent = 'Send cookies to harvester';
  }
}
</script>
</body>
</html>`
