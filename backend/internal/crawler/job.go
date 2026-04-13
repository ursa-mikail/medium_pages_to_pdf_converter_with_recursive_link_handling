package crawler

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
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

	// Resolve custom subdomain → canonical medium.com URL upfront.
	// e.g. mikail-eliyah.medium.com → medium.com/@mikail-eliyah/...
	// This avoids the stricter Cloudflare rules on custom subdomains entirely.
	canonicalURL, err := j.resolveCanonicalURL(j.url)
	if err != nil {
		j.emit("warn", fmt.Sprintf("Could not resolve canonical URL (%v), using original", err))
		canonicalURL = j.url
	} else if canonicalURL != j.url {
		j.emit("info", fmt.Sprintf("Resolved to canonical: %s", canonicalURL))
	}

	// Start local proxy — Chrome navigates here, never contacts medium.com directly
	proxy, err := newLocalProxy(j)
	if err != nil {
		j.fail(fmt.Errorf("local proxy: %w", err)); return
	}
	defer proxy.close()

	ctx, cancel := j.newCtx()
	defer cancel()

	// 1. Collect links
	j.emit("info", "Fetching master page...")
	links, linkErr := j.collectLinks(canonicalURL)
	if linkErr != nil {
		j.fail(fmt.Errorf("collect links: %w", linkErr)); return
	}
	j.emit("info", fmt.Sprintf("Found %d linked Medium articles", len(links)))

	// 2. Master page → PDF
	mainPDFPath := filepath.Join(j.outputDir, "main", "main.pdf")
	j.emit("info", "Converting master page to PDF...")
	if err := j.printToPDF(ctx, proxy, canonicalURL, mainPDFPath); err != nil {
		j.fail(fmt.Errorf("print master PDF: %w", err)); return
	}
	j.addFile("main.pdf", "main/main.pdf", j.url)
	j.emit("ok", "Master PDF saved → main/main.pdf")

	// 3. Each linked page → PDF
	pageMap := map[string]string{}
	for i, link := range links {
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

		select {
		case <-j.stopCh:
			j.emit("warn", "Harvest stopped by user")
			j.setState("stopped")
			j.closeSubscribers()
			return
		case <-time.After(time.Duration(1500+rand.Intn(2000)) * time.Millisecond):
		}

		if err := j.printToPDF(ctx, proxy, link, pdfPath); err != nil {
			j.emit("warn", fmt.Sprintf("Failed: %v", err))
			continue
		}
		pageMap[link] = relPath
		j.addFile(pdfName, relPath, link)
		j.emit("ok", fmt.Sprintf("Saved → %s", relPath))
	}

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

// ─── Canonical URL resolution ─────────────────────────────────────────────────
// Custom subdomains (mikail-eliyah.medium.com) have stricter Cloudflare rules.
// Medium always sets a <link rel="canonical"> pointing to medium.com/@author/...
// We fetch just the head of the page to extract it, then use that URL instead.

func (j *Job) resolveCanonicalURL(rawURL string) (string, error) {
	html, err := j.fetchHTML(rawURL)
	if err != nil {
		return rawURL, err
	}

	// Look for <link rel="canonical" href="...">
	canonRe := regexp.MustCompile(`(?i)<link[^>]+rel=["']canonical["'][^>]+href=["']([^"']+)["']`)
	if m := canonRe.FindStringSubmatch(html); len(m) > 1 {
		canonical := strings.TrimSpace(m[1])
		if strings.Contains(canonical, "medium.com") && canonical != rawURL {
			return canonical, nil
		}
	}
	// Also try href first variant
	canonRe2 := regexp.MustCompile(`(?i)<link[^>]+href=["']([^"']+)["'][^>]+rel=["']canonical["']`)
	if m := canonRe2.FindStringSubmatch(html); len(m) > 1 {
		canonical := strings.TrimSpace(m[1])
		if strings.Contains(canonical, "medium.com") && canonical != rawURL {
			return canonical, nil
		}
	}

	return rawURL, nil
}

// ─── Local proxy ──────────────────────────────────────────────────────────────
// Chrome navigates to http://127.0.0.1:PORT/?src=<url>
// The proxy fetches via Go's http stack (different TLS fingerprint from Chrome —
// Cloudflare doesn't flag it) and streams the HTML back.
// Chrome never contacts medium.com directly.

type localProxy struct {
	server   *http.Server
	listener net.Listener
	job      *Job
	baseURL  string
}

func newLocalProxy(j *Job) (*localProxy, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	p := &localProxy{
		job:     j,
		baseURL: fmt.Sprintf("http://127.0.0.1:%d", ln.Addr().(*net.TCPAddr).Port),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", p.handle)
	p.server = &http.Server{Handler: mux}
	p.listener = ln
	go p.server.Serve(ln) //nolint:errcheck
	return p, nil
}

func (p *localProxy) close() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	p.server.Shutdown(ctx) //nolint:errcheck
}

func (p *localProxy) proxyURLFor(mediumURL string) string {
	return p.baseURL + "/?src=" + url.QueryEscape(mediumURL)
}

func (p *localProxy) handle(w http.ResponseWriter, r *http.Request) {
	src := r.URL.Query().Get("src")
	if src == "" {
		http.Error(w, "missing src", http.StatusBadRequest)
		return
	}
	html, err := p.job.fetchHTML(src)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	html = makeAbsolute(html, src)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, html)
}

// ─── HTTP fetch ───────────────────────────────────────────────────────────────

var userAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36",
}

// isCloudflarePage detects when we got a CF challenge page instead of real content.
func isCloudflarePage(html string) bool {
	return strings.Contains(html, "cf-browser-verification") ||
		strings.Contains(html, "cf_chl_") ||
		strings.Contains(html, "Performing security verification") ||
		strings.Contains(html, "Enable JavaScript and cookies") ||
		(strings.Contains(html, "cloudflare") && strings.Contains(html, "security"))
}

func (j *Job) fetchHTML(rawURL string) (string, error) {
	ua := userAgents[rand.Intn(len(userAgents))]
	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Re-attach cookies on every redirect hop
			for _, c := range j.cookies {
				req.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
			}
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Accept-Encoding", "gzip")
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

	// Attach all user cookies — these are what gets us through the Medium paywall
	// and, on medium.com (not custom subdomains), past Cloudflare too
	for _, c := range j.cookies {
		req.AddCookie(&http.Cookie{Name: c.Name, Value: c.Value})
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("http fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 403 || resp.StatusCode == 503 {
		return "", fmt.Errorf("blocked (HTTP %d) by Cloudflare — ensure uid+sid cookies are set", resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d for %s", resp.StatusCode, rawURL)
	}

	var reader io.Reader = resp.Body
	if resp.Header.Get("Content-Encoding") == "gzip" {
		gz, err := gzip.NewReader(resp.Body)
		if err != nil {
			return "", fmt.Errorf("gzip: %w", err)
		}
		defer gz.Close()
		reader = gz
	}

	body, err := io.ReadAll(reader)
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}

	html := string(body)

	// Detect Cloudflare challenge page served with 200 status
	if isCloudflarePage(html) {
		return "", fmt.Errorf("Cloudflare challenge page received — uid+sid cookies required for this page")
	}

	return html, nil
}

// ─── Browser context ──────────────────────────────────────────────────────────

const stealthScript = `
(function(){
  Object.defineProperty(navigator,'webdriver',{get:()=>undefined});
  Object.defineProperty(navigator,'plugins',{get:()=>[1,2,3,4,5]});
  Object.defineProperty(navigator,'languages',{get:()=>['en-US','en']});
  window.chrome={runtime:{},loadTimes:function(){},csi:function(){}};
})();
`

func (j *Job) newCtx() (context.Context, context.CancelFunc) {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", "new"),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-setuid-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("window-size", "1920,1080"),
		chromedp.Flag("lang", "en-US"),
		chromedp.Flag("allow-insecure-localhost", true),
		chromedp.UserAgent(userAgents[rand.Intn(len(userAgents))]),
	)

	allocCtx, aCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	ctx, cCancel := chromedp.NewContext(allocCtx)
	cancel := func() { cCancel(); aCancel() }

	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(c context.Context) error {
		_, err := page.AddScriptToEvaluateOnNewDocument(stealthScript).Do(c)
		return err
	})); err != nil {
		j.emit("warn", fmt.Sprintf("stealth inject: %v", err))
	}

	return ctx, cancel
}

// ─── collectLinks ─────────────────────────────────────────────────────────────

func (j *Job) collectLinks(rawURL string) ([]string, error) {
	html, err := j.fetchHTML(rawURL)
	if err != nil {
		return nil, err
	}

	hrefRe := regexp.MustCompile(`href=["'](https?://[^"'#\s]+)["']`)
	matches := hrefRe.FindAllStringSubmatch(html, -1)
	j.emit("info", fmt.Sprintf("Raw hrefs found: %d", len(matches)))

	base, _ := url.Parse(rawURL)
	seen := map[string]bool{stripQuery(rawURL): true}
	var uniq []string
	for _, m := range matches {
		parsed, err := url.Parse(m[1])
		if err != nil {
			continue
		}
		clean := stripQuery(base.ResolveReference(parsed).String())
		if !seen[clean] && isMediumArticle(clean) {
			seen[clean] = true
			uniq = append(uniq, clean)
		}
	}
	return uniq, nil
}

// ─── printToPDF ───────────────────────────────────────────────────────────────
// Go http fetches the HTML (bypassing Cloudflare TLS fingerprinting),
// local proxy serves it to Chrome, Chrome renders and prints.
// Chrome never contacts medium.com — Cloudflare never sees it.

func (j *Job) printToPDF(ctx context.Context, proxy *localProxy, rawURL, destPath string) error {
	localURL := proxy.proxyURLFor(rawURL)

	var pdfBuf []byte
	err := chromedp.Run(ctx,
		chromedp.Navigate(localURL),
		chromedp.Sleep(3*time.Second),

		// Remove paywalls / overlays / chrome UI clutter
		chromedp.Evaluate(`
			[".overlay",".modal","[data-testid='paywall']",
			 "[id*='paywall']","[class*='paywall']",
			 "[class*='overlay']","[class*='banner']",
			 "[class*='popup']","[class*='cookie']",
			 "[class*='metabar']","[class*='sidebar']",
			 "nav","footer"]
			.flatMap(s=>[...document.querySelectorAll(s)])
			.forEach(el=>el.remove());
		`, nil),

		chromedp.Evaluate(`window.scrollTo(0,document.body.scrollHeight)`, nil),
		chromedp.Sleep(800*time.Millisecond),
		chromedp.Evaluate(`window.scrollTo(0,0)`, nil),
		chromedp.Sleep(300*time.Millisecond),

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

// makeAbsolute rewrites root-relative src/href/action/srcset to absolute URLs.
func makeAbsolute(html, baseURL string) string {
	base, err := url.Parse(baseURL)
	if err != nil {
		return html
	}
	attrRe := regexp.MustCompile(`((?:src|href|action|srcset)=["'])(/[^"']*)`)
	return attrRe.ReplaceAllStringFunc(html, func(match string) string {
		parts := attrRe.FindStringSubmatch(match)
		if len(parts) < 3 {
			return match
		}
		ref, err := url.Parse(parts[2])
		if err != nil {
			return match
		}
		return parts[1] + base.ResolveReference(ref).String()
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
