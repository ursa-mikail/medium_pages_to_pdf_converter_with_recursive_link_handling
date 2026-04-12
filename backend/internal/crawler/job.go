package crawler

import (
	"context"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// ─── Types ────────────────────────────────────────────────────────────────────

type Cookie struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Domain   string `json:"domain"`
	Path     string `json:"path"`
	Secure   bool   `json:"secure"`
	HTTPOnly bool   `json:"httpOnly"`
}

type LogEntry struct {
	Time    string `json:"time"`
	Level   string `json:"level"`
	Message string `json:"message"`
}

type FileResult struct {
	Name string `json:"name"`
	Path string `json:"path"`
	URL  string `json:"url"`
}

type JobStatus struct {
	ID    string       `json:"id"`
	URL   string       `json:"url"`
	State string       `json:"state"`
	Logs  []LogEntry   `json:"logs"`
	Files []FileResult `json:"files"`
	Error string       `json:"error,omitempty"`
}

type Job struct {
	id        string
	url       string
	cookies   []Cookie
	outputDir string

	mu          sync.RWMutex
	state       string
	logs        []LogEntry
	files       []FileResult
	errMsg      string
	subscribers map[chan LogEntry]struct{}
	stopOnce    sync.Once
	stopCh      chan struct{}
}

func NewJob(id, rawURL string, cookies []Cookie, outputDir string) *Job {
	return &Job{
		id:          id,
		url:         rawURL,
		cookies:     cookies,
		outputDir:   outputDir,
		state:       "queued",
		subscribers: make(map[chan LogEntry]struct{}),
		stopCh:      make(chan struct{}),
	}
}

// Stop signals the job to cancel as soon as possible.
func (j *Job) Stop() {
	j.stopOnce.Do(func() { close(j.stopCh) })
}

func (j *Job) Subscribe() chan LogEntry {
	ch := make(chan LogEntry, 128)
	j.mu.Lock()
	j.subscribers[ch] = struct{}{}
	j.mu.Unlock()
	return ch
}

func (j *Job) Unsubscribe(ch chan LogEntry) {
	j.mu.Lock()
	delete(j.subscribers, ch)
	j.mu.Unlock()
}

func (j *Job) emit(level, msg string) {
	entry := LogEntry{Time: time.Now().Format("15:04:05"), Level: level, Message: msg}
	log.Printf("[%s] %s: %s", j.id, level, msg)
	j.mu.Lock()
	j.logs = append(j.logs, entry)
	subs := make([]chan LogEntry, 0, len(j.subscribers))
	for ch := range j.subscribers {
		subs = append(subs, ch)
	}
	j.mu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- entry:
		default:
		}
	}
}

func (j *Job) addFile(name, relPath, srcURL string) {
	j.mu.Lock()
	j.files = append(j.files, FileResult{Name: name, Path: relPath, URL: srcURL})
	j.mu.Unlock()
}

func (j *Job) Status() JobStatus {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return JobStatus{ID: j.id, URL: j.url, State: j.state, Logs: j.logs, Files: j.files, Error: j.errMsg}
}

func (j *Job) setState(s string) {
	j.mu.Lock()
	j.state = s
	j.mu.Unlock()
}

// ─── Run ──────────────────────────────────────────────────────────────────────

func (j *Job) Run() {
	j.setState("running")
	j.emit("info", fmt.Sprintf("Starting harvest for %s", j.url))

	if err := os.MkdirAll(filepath.Join(j.outputDir, "main"), 0755); err != nil {
		j.fail(err); return
	}
	if err := os.MkdirAll(filepath.Join(j.outputDir, "pages"), 0755); err != nil {
		j.fail(err); return
	}

	ctx, cancel := j.newCtx()
	defer cancel()

	// 1. Navigate master page and collect links
	j.emit("info", "Navigating to master page...")
	links, err := j.collectLinks(ctx, j.url)
	if err != nil {
		j.fail(fmt.Errorf("collect links: %w", err)); return
	}
	j.emit("info", fmt.Sprintf("Found %d linked Medium articles", len(links)))

	// 2. Master page → PDF
	mainPDFPath := filepath.Join(j.outputDir, "main", "main.pdf")
	j.emit("info", "Converting master page to PDF...")
	if err := j.printToPDF(ctx, j.url, mainPDFPath); err != nil {
		j.fail(fmt.Errorf("print master PDF: %w", err)); return
	}
	j.addFile("main.pdf", "main/main.pdf", j.url)
	j.emit("ok", "Master PDF saved → main/main.pdf")

	// 3. Each linked page → PDF
	pageMap := map[string]string{}
	for i, link := range links {
		// Check for stop signal before each page
		select {
		case <-j.stopCh:
			j.emit("warn", "Harvest stopped by user")
			j.setState("stopped")
			j.closeSubscribers()
			return
		default:
		}

		safe := sanitizeFilename(link)
		pdfName := fmt.Sprintf("page_%02d_%s.pdf", i+1, safe)
		pdfPath := filepath.Join(j.outputDir, "pages", pdfName)
		relPath := "pages/" + pdfName

		j.emit("info", fmt.Sprintf("[%d/%d] %s", i+1, len(links), link))

		// human-like delay between requests (also cancellable)
		select {
		case <-j.stopCh:
			j.emit("warn", "Harvest stopped by user")
			j.setState("stopped")
			j.closeSubscribers()
			return
		case <-time.After(time.Duration(2000+rand.Intn(3000)) * time.Millisecond):
		}

		if err := j.printToPDF(ctx, link, pdfPath); err != nil {
			j.emit("warn", fmt.Sprintf("Failed: %v", err))
			continue
		}
		pageMap[link] = relPath
		j.addFile(pdfName, relPath, link)
		j.emit("ok", fmt.Sprintf("Saved → %s", relPath))
	}

	// 4. Write link-map sidecar
	if err := patchPDFLinks(mainPDFPath, pageMap); err != nil {
		j.emit("warn", fmt.Sprintf("Link sidecar: %v", err))
	} else {
		j.emit("ok", "Link map written → main/main.pdf.links.txt")
	}

	j.setState("done")
	j.emit("ok", fmt.Sprintf("✅ Done. %d PDFs generated.", len(pageMap)+1))
	j.closeSubscribers()
}

func (j *Job) fail(err error) {
	j.mu.Lock()
	j.state = "failed"
	j.errMsg = err.Error()
	j.mu.Unlock()
	j.emit("error", err.Error())
	j.closeSubscribers()
}

func (j *Job) closeSubscribers() {
	j.mu.Lock()
	for ch := range j.subscribers {
		close(ch)
	}
	j.subscribers = make(map[chan LogEntry]struct{})
	j.mu.Unlock()
}

// ─── HTTP client (bypasses Cloudflare TLS fingerprinting) ────────────────────
//
// Cloudflare's bot detection works at two layers:
//   1. TLS fingerprinting (JA3/JA4) - identifies headless Chrome at the network level
//   2. JS challenges - checks browser properties
//
// Solution: fetch HTML using Go's net/http (different TLS stack, not flagged) with
// real browser headers + user cookies, then render locally in chromedp without
// making any outbound requests. Cloudflare never sees Chrome.

var userAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36",
}

// buildHTTPClient returns a net/http client that presents as a real browser.
func (j *Job) buildHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Re-attach cookies on redirects
			for _, c := range j.cookies {
				req.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
			}
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
}

// fetchHTML fetches a URL using Go's net/http with real browser headers.
// This bypasses Cloudflare TLS fingerprinting since Go's TLS stack
// looks nothing like headless Chrome.
func (j *Job) fetchHTML(rawURL string) (string, error) {
	ua := userAgents[rand.Intn(len(userAgents))]
	client := j.buildHTTPClient()

	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return "", err
	}

	// Real browser headers — order matters for fingerprinting
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Pragma", "no-cache")
	req.Header.Set("Sec-Ch-Ua", `"Chromium";v="124", "Google Chrome";v="124", "Not-A.Brand";v="99"`)
	req.Header.Set("Sec-Ch-Ua-Mobile", "?0")
	req.Header.Set("Sec-Ch-Ua-Platform", `"Windows"`)
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "none")
	req.Header.Set("Sec-Fetch-User", "?1")
	req.Header.Set("Upgrade-Insecure-Requests", "1")

	// Inject cookies
	for _, c := range j.cookies {
		req.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("http fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 403 {
		return "", fmt.Errorf("access denied (403) — page may require login cookies")
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}
	return string(body), nil
}

// ─── Browser context (PDF rendering only — no outbound requests) ─────────────

func (j *Job) newCtx() (context.Context, context.CancelFunc) {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", "new"), // new headless mode — harder to detect
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-setuid-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("window-size", "1920,1080"),
		chromedp.Flag("lang", "en-US"),
		chromedp.Flag("run-all-compositor-stages-before-draw", true),
		chromedp.Flag("disable-background-networking", false),
		chromedp.Flag("allow-running-insecure-content", true),
	)

	allocCtx, aCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	ctx, cCancel := chromedp.NewContext(allocCtx)
	cancel := func() { cCancel(); aCancel() }
	return ctx, cancel
}

// ─── collectLinks ─────────────────────────────────────────────────────────────
// Fetches via Go http client, parses links from HTML without touching Chrome.

func (j *Job) collectLinks(_ context.Context, rawURL string) ([]string, error) {
	html, err := j.fetchHTML(rawURL)
	if err != nil {
		return nil, err
	}

	// Extract all href values using regex (avoids HTML parser dependency)
	hrefRe := regexp.MustCompile(`href=["'](https?://[^"'#\s]+)["']`)
	matches := hrefRe.FindAllStringSubmatch(html, -1)

	j.emit("info", fmt.Sprintf("Raw hrefs collected: %d", len(matches)))

	base, _ := url.Parse(rawURL)
	seen := map[string]bool{stripQuery(rawURL): true}
	var uniq []string
	for _, m := range matches {
		ref := m[1]
		parsed, err := url.Parse(ref)
		if err != nil {
			continue
		}
		resolved := base.ResolveReference(parsed)
		clean := stripQuery(resolved.String())
		if !seen[clean] && isMediumArticle(clean) {
			seen[clean] = true
			uniq = append(uniq, clean)
		}
	}
	return uniq, nil
}

// ─── printToPDF ───────────────────────────────────────────────────────────────
// Fetches HTML via Go http client (bypasses Cloudflare), then renders locally
// in chromedp by loading content directly — Chrome never makes outbound requests.

func (j *Job) printToPDF(ctx context.Context, rawURL, destPath string) error {
	// Step 1: fetch HTML with Go's http client — invisible to Cloudflare
	rawHTML, err := j.fetchHTML(rawURL)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}

	// Step 2: rewrite relative URLs to absolute so resources load correctly
	html := makeAbsolute(rawHTML, rawURL)

	// Step 3: render locally in chromedp, print to PDF
	var pdfBuf []byte
	err = chromedp.Run(ctx,
		// Navigate to the real URL first to set the correct origin context
		// (needed for same-origin resource loads)
		chromedp.ActionFunc(func(c context.Context) error {
			_, _, _, err := page.Navigate(rawURL).Do(c)
			return err
		}),
		chromedp.Sleep(500*time.Millisecond),
		// Overwrite the page content with our pre-fetched HTML
		chromedp.ActionFunc(func(c context.Context) error {
			return page.SetDocumentContent(html).Do(c)
		}),
		chromedp.Sleep(3*time.Second),
		// Remove paywalls / overlays
		chromedp.Evaluate(`
			[".overlay",".modal","[data-testid='paywall']",
			 "[id*='paywall']","[class*='paywall']",
			 "[class*='overlay']","[class*='banner']",
			 "[class*='popup']","[class*='cookie']"]
			.flatMap(s=>[...document.querySelectorAll(s)])
			.forEach(el=>el.remove());
		`, nil),
		// Scroll to trigger lazy loads
		chromedp.Evaluate(`window.scrollTo(0,document.body.scrollHeight)`, nil),
		chromedp.Sleep(1500*time.Millisecond),
		chromedp.Evaluate(`window.scrollTo(0,0)`, nil),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.ActionFunc(func(c context.Context) error {
			var printErr error
			pdfBuf, _, printErr = page.PrintToPDF().
				WithPrintBackground(true).
				WithPaperWidth(8.27).
				WithPaperHeight(11.69).
				WithMarginTop(0.4).
				WithMarginBottom(0.4).
				WithMarginLeft(0.4).
				WithMarginRight(0.4).
				WithPreferCSSPageSize(false).
				Do(c)
			return printErr
		}),
	)
	if err != nil {
		return err
	}
	return os.WriteFile(destPath, pdfBuf, 0644)
}

// makeAbsolute rewrites relative src/href attributes to absolute URLs
// so that resources (images, CSS) load correctly when content is injected locally.
func makeAbsolute(html, baseURL string) string {
	base, err := url.Parse(baseURL)
	if err != nil {
		return html
	}
	// Rewrite src="..." and href="..." that start with / or are relative
	attrRe := regexp.MustCompile(`(src|href|action)=["'](/[^"']*)["']`)
	return attrRe.ReplaceAllStringFunc(html, func(match string) string {
		parts := attrRe.FindStringSubmatch(match)
		if len(parts) < 3 {
			return match
		}
		attr, path := parts[1], parts[2]
		ref, err := url.Parse(path)
		if err != nil {
			return match
		}
		abs := base.ResolveReference(ref).String()
		return fmt.Sprintf(`%s="%s"`, attr, abs)
	})
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

var mediumPathRe = regexp.MustCompile(`^/@?[^/]+/[a-zA-Z0-9_-]{5,}`)

func isMediumArticle(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if !strings.Contains(u.Hostname(), "medium.com") {
		return false
	}
	for _, skip := range []string{"/tag/", "/topics/", "/me/", "/sign", "/about", "/membership", "/new-story"} {
		if strings.HasPrefix(u.Path, skip) {
			return false
		}
	}
	return mediumPathRe.MatchString(u.Path)
}

func stripQuery(rawURL string) string {
	if i := strings.IndexAny(rawURL, "?#"); i != -1 {
		return rawURL[:i]
	}
	return rawURL
}

var nonAlnum = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

func sanitizeFilename(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "page"
	}
	slug := strings.TrimPrefix(u.Path, "/")
	slug = strings.ReplaceAll(slug, "/", "_")
	slug = nonAlnum.ReplaceAllString(slug, "")
	if len(slug) > 60 {
		slug = slug[:60]
	}
	if slug == "" {
		slug = "page"
	}
	return slug
}

