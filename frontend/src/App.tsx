import { useState, useRef, useEffect, useCallback, Component } from "react";
import type { ReactNode, ErrorInfo } from "react";

const API = "";

// ─── Types ────────────────────────────────────────────────────────────────────

interface CookieT {
  name: string; value: string; domain: string;
  path: string; secure: boolean; httpOnly: boolean;
}
interface LogEntry  { time: string; level: string; message: string; }
interface FileResult { name: string; path: string; url: string; }
interface JobStatus {
  id: string; url: string; state: string;
  logs: LogEntry[]; files: FileResult[]; error?: string;
}

// ─── Inline styles ────────────────────────────────────────────────────────────

const S = {
  shell: {
    minHeight: "100vh", maxWidth: 820, margin: "0 auto",
    padding: "0 1.25rem", fontFamily: "'Segoe UI',system-ui,sans-serif",
    background: "#0a0a0f", color: "#e8e4dc",
  } as React.CSSProperties,
  header: {
    padding: "2.5rem 0 1.5rem", borderBottom: "1px solid #1e1e28",
  } as React.CSSProperties,
  logoRow: { display: "flex", alignItems: "center", gap: 10, marginBottom: 6 } as React.CSSProperties,
  logoMark: {
    width: 32, height: 32, background: "#ff4500", color: "#fff",
    display: "inline-flex", alignItems: "center", justifyContent: "center",
    fontWeight: 800, fontSize: 18, borderRadius: 4,
  } as React.CSSProperties,
  logoText: { fontWeight: 800, fontSize: 22, letterSpacing: "-0.02em" } as React.CSSProperties,
  tagline: { color: "#6b6560", fontSize: 13 } as React.CSSProperties,
  main: { padding: "1.5rem 0", display: "flex", flexDirection: "column", gap: 16 } as React.CSSProperties,
  panel: {
    background: "#0f0f18", border: "1px solid #1e1e28", borderRadius: 8, overflow: "hidden",
  } as React.CSSProperties,
  tabBar: { display: "flex", borderBottom: "1px solid #1e1e28" } as React.CSSProperties,
  tabContent: { padding: "1rem 1.25rem" } as React.CSSProperties,
  hint: { color: "#6b6560", fontSize: 12, lineHeight: 1.6, fontFamily: "monospace" } as React.CSSProperties,
  urlWrap: {
    display: "flex", alignItems: "center", gap: 10,
    background: "#161622", border: "1px solid #2a2a3a", borderRadius: 6,
    padding: "0.65rem 0.9rem", marginBottom: 10,
  } as React.CSSProperties,
  urlInput: {
    flex: 1, background: "transparent", border: "none", outline: "none",
    color: "#e8e4dc", fontFamily: "monospace", fontSize: 14,
  } as React.CSSProperties,
  textarea: {
    width: "100%", background: "#161622", border: "1px solid #2a2a3a",
    borderRadius: 6, padding: "0.6rem 0.8rem", color: "#e8e4dc",
    fontFamily: "monospace", fontSize: 13, resize: "vertical" as const,
    outline: "none", boxSizing: "border-box" as const,
  } as React.CSSProperties,
  btnPrimary: {
    display: "flex", alignItems: "center", gap: 8, justifyContent: "center",
    background: "#ff4500", color: "#fff", border: "none", borderRadius: 0,
    padding: "0.85rem 1.5rem", fontWeight: 700, fontSize: 15,
    letterSpacing: "0.05em", cursor: "pointer", width: "100%",
  } as React.CSSProperties,
  btnStop: {
    display: "flex", alignItems: "center", gap: 6,
    background: "transparent", border: "1px solid #ff4d6d", color: "#ff4d6d",
    borderRadius: 6, padding: "0.5rem 1rem", fontWeight: 600,
    fontSize: 13, cursor: "pointer",
  } as React.CSSProperties,
  btnSecondary: {
    background: "#161622", border: "1px solid #2a2a3a", color: "#aab",
    borderRadius: 6, padding: "0.35rem 0.9rem", fontSize: 12,
    cursor: "pointer", fontFamily: "monospace",
  } as React.CSSProperties,
  btnActive: { borderColor: "#6070ff", color: "#e8e4dc", background: "#0d0d20" } as React.CSSProperties,
  btnLink: {
    background: "none", border: "none", color: "#6070ff", fontSize: 12,
    cursor: "pointer", textDecoration: "underline", padding: 0,
  } as React.CSSProperties,
  actionRow: { display: "flex", gap: 10, padding: "0 1.25rem 1rem" } as React.CSSProperties,
  statusBar: {
    display: "flex", alignItems: "center", gap: 8, padding: "0.5rem 1rem",
    background: "#0f0f18", borderBottom: "1px solid #1e1e28",
    fontFamily: "monospace", fontSize: 12, color: "#6b6560",
  } as React.CSSProperties,
  logsBox: {
    padding: "0.75rem 1rem", maxHeight: 320, overflowY: "auto" as const,
    fontFamily: "monospace", fontSize: 12, lineHeight: 1.8,
    display: "flex", flexDirection: "column" as const,
  } as React.CSSProperties,
  filesSection: { padding: "1rem", borderTop: "1px solid #1a1a28" } as React.CSSProperties,
  filesTitle: {
    fontSize: 12, fontWeight: 700, textTransform: "uppercase" as const,
    letterSpacing: "0.1em", color: "#6b6560", marginBottom: 10,
    display: "flex", alignItems: "center", gap: 6,
  } as React.CSSProperties,
  fileCard: {
    display: "flex", alignItems: "center", gap: 8, padding: "0.45rem 0.75rem",
    background: "#0f0f18", border: "1px solid #1e1e28", borderRadius: 5,
    color: "#e8e4dc", textDecoration: "none", fontSize: 13,
    fontFamily: "monospace", marginBottom: 6,
  } as React.CSSProperties,
  errorBox: {
    margin: "0 1rem 1rem", padding: "0.65rem 0.9rem",
    background: "#1a0a0a", border: "1px solid #ef4444",
    borderRadius: 5, color: "#ef4444", fontSize: 13,
  } as React.CSSProperties,
  cookieGrid: { display: "grid", gridTemplateColumns: "1fr 2fr 1.4fr 0.6fr auto", gap: 6, marginBottom: 6 } as React.CSSProperties,
  cookieInput: {
    background: "#161622", border: "1px solid #2a2a3a", borderRadius: 4,
    padding: "0.4rem 0.6rem", color: "#e8e4dc", fontFamily: "monospace",
    fontSize: 12, outline: "none",
  } as React.CSSProperties,
  banner: {
    background: "#0d0d20", border: "1px solid #2a2a4a", borderRadius: 6,
    padding: "0.85rem 1rem", marginBottom: 14,
  } as React.CSSProperties,
  footer: {
    borderTop: "1px solid #1e1e28", padding: "1rem 0",
    color: "#3a3a5a", fontSize: 11, fontFamily: "monospace",
    display: "flex", gap: 10,
  } as React.CSSProperties,
};

// ─── Error Boundary ───────────────────────────────────────────────────────────

class ErrorBoundary extends Component<{ children: ReactNode }, { err: string }> {
  state = { err: "" };
  static getDerivedStateFromError(e: Error) { return { err: e.message }; }
  componentDidCatch(e: Error, i: ErrorInfo) { console.error(e, i); }
  render() {
    if (this.state.err) return (
      <div style={{ padding: 32, fontFamily: "monospace", color: "#ef4444" }}>
        <b>Render error:</b> {this.state.err}
        <br /><button onClick={() => this.setState({ err: "" })} style={{ marginTop: 12 }}>Retry</button>
      </div>
    );
    return this.props.children;
  }
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

function parseCookies(raw: string): CookieT[] {
  if (!raw.trim()) return [];
  const out: CookieT[] = [];
  for (const part of raw.split(";")) {
    const idx = part.indexOf("=");
    if (idx < 1) continue;
    const name = part.slice(0, idx).trim();
    const value = part.slice(idx + 1).trim();
    if (name) out.push({ name, value, domain: ".medium.com", path: "/", secure: true, httpOnly: false });
  }
  return out;
}

function logColor(level: string) {
  if (level === "ok")    return "#22c55e";
  if (level === "warn")  return "#f59e0b";
  if (level === "error") return "#ef4444";
  return "#555577";
}

// ─── Tab button ───────────────────────────────────────────────────────────────

function TabBtn({ label, active, onClick }: { label: string; active: boolean; onClick: () => void }) {
  return (
    <button onClick={onClick} style={{
      padding: "0.65rem 1.1rem", background: "none", border: "none",
      borderBottom: active ? "2px solid #ff4500" : "2px solid transparent",
      color: active ? "#e8e4dc" : "#6b6560", fontWeight: 600,
      fontSize: 13, cursor: "pointer", letterSpacing: "0.04em",
    }}>
      {label}
    </button>
  );
}

// ─── Main ─────────────────────────────────────────────────────────────────────

function AppInner() {
  const [tab, setTab]         = useState<"url" | "cookies">("url");
  const [cookieTab, setCookieTab] = useState<"raw" | "table">("raw");
  const [url, setUrl]         = useState("");
  const [raw, setRaw]         = useState("");
  const [cookies, setCookies] = useState<CookieT[]>([
    { name: "uid", value: "", domain: ".medium.com", path: "/", secure: true, httpOnly: false },
    { name: "sid", value: "", domain: ".medium.com", path: "/", secure: true, httpOnly: false },
  ]);
  const [job, setJob]         = useState<JobStatus | null>(null);
  const [loading, setLoading] = useState(false);
  const [stopping, setStopping] = useState(false);
  const [fetchingCookies, setFetchingCookies] = useState(false);
  const [cookieStatus, setCookieStatus] = useState("");
  const [netErr, setNetErr]   = useState("");

  const logsEnd = useRef<HTMLDivElement>(null);
  const es      = useRef<EventSource | null>(null);
  const poll    = useRef<ReturnType<typeof setInterval> | null>(null);

  useEffect(() => { logsEnd.current?.scrollIntoView({ behavior: "smooth" }); }, [job?.logs?.length]);
  useEffect(() => () => { es.current?.close(); }, []);

  // ── cookie helpers ──
  const activeCookies = () =>
    cookieTab === "raw" ? parseCookies(raw) : cookies.filter(c => c.name && c.value);

  const addCookie = () => setCookies(c => [...c, { name: "", value: "", domain: ".medium.com", path: "/", secure: true, httpOnly: false }]);
  const rmCookie  = (i: number) => setCookies(c => c.filter((_, j) => j !== i));
  const setCookieField = (i: number, f: keyof CookieT, v: string) =>
    setCookies(c => c.map((r, j) => j === i ? { ...r, [f]: v } : r));

  // ── streaming ──
  const startStream = useCallback((jobId: string) => {
    es.current?.close();
    const src = new EventSource(`${API}/api/jobs/${jobId}/stream`);
    es.current = src;

    src.onmessage = (e) => {
      try {
        const entry: LogEntry = JSON.parse(e.data);
        setJob(prev => prev ? { ...prev, logs: [...(prev.logs || []), entry] } : prev);
      } catch (err) { console.warn("SSE parse error", err); }
    };

    src.addEventListener("done", async () => {
      src.close();
      try {
        const r = await fetch(`${API}/api/jobs/${jobId}`);
        if (r.ok) setJob(await r.json());
      } catch (err) { console.error("final poll", err); }
      setLoading(false);
      setStopping(false);
    });

    src.onerror = () => { src.close(); setLoading(false); setStopping(false); };
  }, []);

  // ── harvest ──
  const harvest = async () => {
    if (!url.trim() || loading) return;
    setNetErr("");
    setLoading(true);
    setStopping(false);
    setJob(null);

    try {
      const r = await fetch(`${API}/api/harvest`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ url: url.trim(), cookies: activeCookies() }),
      });
      if (!r.ok) { setNetErr(`Server error ${r.status}`); setLoading(false); return; }
      const { jobID } = await r.json();
      setJob({ id: jobID, url: url.trim(), state: "running", logs: [], files: [] });
      startStream(jobID);
    } catch (err) {
      setNetErr(`Network error: ${err instanceof Error ? err.message : String(err)}`);
      setLoading(false);
    }
  };

  const stop = async () => {
    if (!job?.id) return;
    setStopping(true);
    try { await fetch(`${API}/api/jobs/${job.id}/stop`, { method: "POST" }); } catch { setStopping(false); }
  };

  // ── cookie bridge ──
  const openHelper = async () => {
    setFetchingCookies(true);
    setCookieStatus("Starting session…");
    try {
      const r = await fetch(`${API}/api/cookie-session`, { method: "POST" });
      if (!r.ok) { setCookieStatus("Failed"); setFetchingCookies(false); return; }
      const { token, helperURL } = await r.json();
      window.open(helperURL, "_blank");
      setCookieStatus("Helper opened — follow steps there, then return here.");

      if (poll.current) clearInterval(poll.current);
      const deadline = Date.now() + 10 * 60 * 1000;
      poll.current = setInterval(async () => {
        if (Date.now() > deadline) { clearInterval(poll.current!); setFetchingCookies(false); setCookieStatus("Timed out."); return; }
        try {
          const p = await fetch(`${API}/api/cookie-session/${token}`);
          if (p.status === 202) return;
          clearInterval(poll.current!);
          if (!p.ok) { setFetchingCookies(false); return; }
          const data = await p.json();
          const got: CookieT[] = data.cookies || [];
          if (got.length) { setCookies(got); setCookieTab("table"); setCookieStatus(`✅ ${got.length} cookies loaded!`); }
          else { setCookieStatus("No cookies received."); }
          setFetchingCookies(false);
        } catch { /* keep polling */ }
      }, 2000);
    } catch (err) { setCookieStatus(`Error: ${err}`); setFetchingCookies(false); }
  };

  const stateColor =
    job?.state === "done"    ? "#22c55e" :
    job?.state === "failed"  ? "#ef4444" :
    job?.state === "stopped" ? "#f59e0b" : "#ff4500";

  return (
    <div style={S.shell}>
      {/* Header */}
      <header style={S.header}>
        <div style={S.logoRow}>
          <span style={S.logoMark}>M</span>
          <span style={S.logoText}>
            edium<span style={{ color: "#ff4500" }}>Harvester</span>
          </span>
          <span style={{ fontSize: 10, border: "1px solid #333", borderRadius: 2, padding: "1px 6px", color: "#555", fontFamily: "monospace" }}>v1.0</span>
        </div>
        <p style={S.tagline}>Convert any Medium page + linked articles to offline cross-linked PDFs</p>
      </header>

      <main style={S.main}>
        {/* Input panel */}
        <div style={S.panel}>
          {/* Tabs */}
          <div style={S.tabBar}>
            <TabBtn label="URL" active={tab === "url"} onClick={() => setTab("url")} />
            <TabBtn
              label={`Auth Cookies${activeCookies().length ? ` (${activeCookies().length})` : ""}`}
              active={tab === "cookies"}
              onClick={() => setTab("cookies")}
            />
          </div>

          {/* URL tab */}
          {tab === "url" && (
            <div style={S.tabContent}>
              <div style={S.urlWrap}>
                <span style={{ color: "#555" }}>🌐</span>
                <input
                  style={S.urlInput}
                  placeholder="https://medium.com/@author/article-slug"
                  value={url}
                  onChange={e => setUrl(e.target.value)}
                  onKeyDown={e => e.key === "Enter" && harvest()}
                  spellCheck={false}
                  autoComplete="off"
                />
              </div>
              <p style={S.hint}>
                Paste the master Medium URL. Linked articles (1 level deep) will be converted and cross-linked.
                Add auth cookies to unlock member-only content.
              </p>
            </div>
          )}

          {/* Cookies tab */}
          {tab === "cookies" && (
            <div style={S.tabContent}>
              {/* Banner */}
              <div style={S.banner}>
                <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", flexWrap: "wrap", gap: 8 }}>
                  <span style={{ fontSize: 13, color: "#aac" }}>✨ Guided helper — no DevTools needed</span>
                  <button
                    style={{ ...S.btnSecondary, background: "#1a1a35", borderColor: "#4444aa", color: "#aabbff", padding: "0.45rem 1rem" }}
                    onClick={openHelper}
                    disabled={fetchingCookies}
                  >
                    {fetchingCookies ? "Waiting for cookies…" : "✨ Open cookie helper"}
                  </button>
                </div>
                {cookieStatus && <p style={{ marginTop: 8, fontSize: 12, color: cookieStatus.startsWith("✅") ? "#22c55e" : "#aac" }}>{cookieStatus}</p>}
              </div>

              {/* Divider */}
              <div style={{ display: "flex", alignItems: "center", gap: 12, margin: "0 0 12px", color: "#3a3a5a", fontSize: 12 }}>
                <div style={{ flex: 1, height: 1, background: "#1e1e28" }} />
                <span>or enter manually</span>
                <div style={{ flex: 1, height: 1, background: "#1e1e28" }} />
              </div>

              {/* Mode toggle */}
              <div style={{ display: "flex", gap: 6, marginBottom: 12 }}>
                {(["raw", "table"] as const).map(m => (
                  <button key={m} onClick={() => setCookieTab(m)}
                    style={{ ...S.btnSecondary, ...(cookieTab === m ? S.btnActive : {}) }}>
                    {m === "raw" ? "Raw string" : "Table"}
                  </button>
                ))}
              </div>

              {cookieTab === "raw" ? (
                <div>
                  <textarea
                    style={{ ...S.textarea, minHeight: 80 }}
                    rows={4}
                    placeholder="uid=abc123; sid=xyz789; __cfruid=..."
                    value={raw}
                    onChange={e => setRaw(e.target.value)}
                    spellCheck={false}
                  />
                  <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginTop: 6 }}>
                    <span style={S.hint}>{parseCookies(raw).length > 0 ? `${parseCookies(raw).length} cookies parsed` : ""}</span>
                    <button style={S.btnSecondary} disabled={!raw.trim()} onClick={() => {
                      const p = parseCookies(raw);
                      if (p.length) { setCookies(p); setCookieTab("table"); }
                    }}>Parse → Table</button>
                  </div>
                </div>
              ) : (
                <div>
                  <div style={{ display: "grid", gridTemplateColumns: "1fr 2fr 1.4fr 0.6fr 24px", gap: 6, marginBottom: 4 }}>
                    {["name","value","domain","path",""].map(h => (
                      <span key={h} style={{ fontSize: 10, color: "#555", fontFamily: "monospace", textTransform: "uppercase", letterSpacing: "0.08em" }}>{h}</span>
                    ))}
                  </div>
                  {cookies.map((c, i) => (
                    <div key={i} style={{ display: "grid", gridTemplateColumns: "1fr 2fr 1.4fr 0.6fr 24px", gap: 6, marginBottom: 6 }}>
                      {(["name","value","domain","path"] as (keyof CookieT)[]).map(f => (
                        <input key={f} style={S.cookieInput} value={String(c[f] ?? "")}
                          placeholder={f} spellCheck={false}
                          onChange={e => setCookieField(i, f, e.target.value)} />
                      ))}
                      <button onClick={() => rmCookie(i)}
                        style={{ background: "none", border: "none", color: "#555", cursor: "pointer", fontSize: 16, lineHeight: 1 }}>×</button>
                    </div>
                  ))}
                  <button style={{ ...S.btnSecondary, marginTop: 4 }} onClick={addCookie}>+ Add cookie</button>
                </div>
              )}
            </div>
          )}

          {/* Action row */}
          <div style={S.actionRow}>
            <button
              style={{ ...S.btnPrimary, opacity: loading || !url.trim() ? 0.5 : 1, cursor: loading || !url.trim() ? "not-allowed" : "pointer", flex: 1, borderRadius: 6 }}
              onClick={harvest}
              disabled={loading || !url.trim()}
            >
              {loading ? "⟳ Harvesting…" : "▶ Harvest"}
            </button>

            {loading && job?.state === "running" && (
              <button style={S.btnStop} onClick={stop} disabled={stopping}>
                {stopping ? "Stopping…" : "⏹ Stop"}
              </button>
            )}
          </div>

          {netErr && (
            <div style={{ ...S.errorBox, margin: "0 1rem 1rem" }}>⚠ {netErr}</div>
          )}
        </div>

        {/* Output panel */}
        {job && (
          <div style={S.panel}>
            {/* Status bar */}
            <div style={S.statusBar}>
              <span>⬡</span>
              <span style={{ textTransform: "uppercase", letterSpacing: "0.08em" }}>Job</span>
              <code style={{ color: "#6070ff", fontSize: 11 }}>{job.id}</code>
              <span style={{ marginLeft: "auto", border: `1px solid ${stateColor}`, color: stateColor, padding: "1px 8px", borderRadius: 2, fontSize: 11, letterSpacing: "0.08em", textTransform: "uppercase" }}>
                {job.state}
              </span>
            </div>

            {/* Logs */}
            <div style={S.logsBox}>
              {(job.logs || []).map((e, i) => (
                <div key={i} style={{ display: "flex", gap: 8, alignItems: "flex-start", padding: "1px 0" }}>
                  <span style={{ color: logColor(e.level), flexShrink: 0, marginTop: 1 }}>
                    {e.level === "ok" ? "✓" : e.level === "warn" ? "!" : e.level === "error" ? "✗" : "›"}
                  </span>
                  <span style={{ color: "#3a3a5a", flexShrink: 0 }}>{e.time}</span>
                  <span style={{ color: e.level === "ok" ? "#22c55e" : e.level === "error" ? "#ef4444" : e.level === "warn" ? "#f59e0b" : "#a0a0c0", wordBreak: "break-all" }}>
                    {e.message}
                  </span>
                </div>
              ))}
              <div ref={logsEnd} />
            </div>

            {/* Files */}
            {(job.files || []).length > 0 && (
              <div style={S.filesSection}>
                <div style={S.filesTitle}>
                  📄 Output files <span style={{ background: "#2a2a3a", padding: "1px 8px", borderRadius: 10, fontSize: 11 }}>{job.files.length}</span>
                </div>
                {job.files.map((f, i) => (
                  <a key={i} href={`${API}/api/files/${job.id}/${f.path}`} download={f.name}
                    style={{ ...S.fileCard, borderColor: f.path.startsWith("main/") ? "#3a2020" : "#1e1e28" }}>
                    📄
                    <span style={{ flex: 1 }}>{f.name}</span>
                    <span style={{ color: "#555", fontSize: 11 }}>↓</span>
                    {f.path.startsWith("main/") && (
                      <span style={{ fontSize: 10, border: "1px solid #ff4500", color: "#ff4500", padding: "0 5px", borderRadius: 2 }}>master</span>
                    )}
                  </a>
                ))}
              </div>
            )}

            {job.error && <div style={S.errorBox}>✗ {job.error}</div>}
          </div>
        )}
      </main>

      <footer style={S.footer}>
        <span>Medium Harvester</span><span>·</span><span>TypeScript + Go + Chromium</span>
      </footer>
    </div>
  );
}

export default function App() {
  return <ErrorBoundary><AppInner /></ErrorBoundary>;
}
