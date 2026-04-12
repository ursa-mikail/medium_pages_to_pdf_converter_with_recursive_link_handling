import { useState, useRef, useEffect, useCallback } from "react";
import { motion, AnimatePresence } from "framer-motion";
import {
  Plus, Play, Download, ChevronRight,
  FileText, Globe, Cookie, Terminal, CheckCircle,
  AlertTriangle, XCircle, Loader2, X, Square, Sparkles
} from "lucide-react";
import type { Cookie as CookieType, LogEntry, FileResult, JobStatus } from "./types";
import styles from "./App.module.css";

const API = import.meta.env.VITE_API_URL ?? "";

// ─── Helpers ──────────────────────────────────────────────────────────────────

function parseRawCookies(raw: string): CookieType[] {
  return raw
    .split(";")
    .map((s) => s.trim())
    .filter(Boolean)
    .map((s) => {
      const idx = s.indexOf("=");
      if (idx === -1) return null;
      return {
        name: s.slice(0, idx).trim(),
        value: s.slice(idx + 1).trim(),
        domain: ".medium.com",
        path: "/",
        secure: true as boolean,
        httpOnly: false as boolean,
      } as CookieType;
    })
    .filter((c): c is CookieType => c !== null && c!.name !== "");
}

// ─── Cookie Row ───────────────────────────────────────────────────────────────

function CookieRow({
  cookie, index, onChange, onRemove,
}: {
  cookie: CookieType; index: number;
  onChange: (i: number, field: keyof CookieType, val: string) => void;
  onRemove: (i: number) => void;
}) {
  return (
    <motion.div
      className={styles.cookieRow}
      initial={{ opacity: 0, x: -12 }}
      animate={{ opacity: 1, x: 0 }}
      exit={{ opacity: 0, x: 12 }}
    >
      {(["name", "value", "domain", "path"] as (keyof CookieType)[]).map((field) => (
        <input
          key={field}
          className={styles.cookieInput}
          placeholder={field}
          value={String(cookie[field] ?? "")}
          onChange={(e) => onChange(index, field, e.target.value)}
          spellCheck={false}
        />
      ))}
      <button className={styles.removeBtn} onClick={() => onRemove(index)} title="Remove">
        <X size={14} />
      </button>
    </motion.div>
  );
}

// ─── Log Line ─────────────────────────────────────────────────────────────────

function LogLine({ entry }: { entry: LogEntry }) {
  const icons = {
    info: <ChevronRight size={12} />,
    ok: <CheckCircle size={12} />,
    warn: <AlertTriangle size={12} />,
    error: <XCircle size={12} />,
  };
  return (
    <motion.div
      className={`${styles.logLine} ${styles[`log_${entry.level}`]}`}
      initial={{ opacity: 0, y: 4 }} animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.12 }}
    >
      <span className={styles.logIcon}>{icons[entry.level]}</span>
      <span className={styles.logTime}>{entry.time}</span>
      <span className={styles.logMsg}>{entry.message}</span>
    </motion.div>
  );
}

// ─── File Card ────────────────────────────────────────────────────────────────

function FileCard({ file, jobId }: { file: FileResult; jobId: string }) {
  const isMain = file.path.startsWith("main/");
  const href = `${API}/api/files/${jobId}/${file.path}`;
  return (
    <motion.a
      href={href} download={file.name}
      className={`${styles.fileCard} ${isMain ? styles.fileCardMain : ""}`}
      initial={{ opacity: 0, scale: 0.95 }} animate={{ opacity: 1, scale: 1 }}
      whileHover={{ y: -2 }}
    >
      <FileText size={16} />
      <span className={styles.fileName}>{file.name}</span>
      <Download size={12} className={styles.dlIcon} />
      {isMain && <span className={`tag ${styles.mainTag}`}>master</span>}
    </motion.a>
  );
}

// ─── Main App ─────────────────────────────────────────────────────────────────

export default function App() {
  const [url, setUrl] = useState("");
  const [cookies, setCookies] = useState<CookieType[]>([
    { name: "uid", value: "", domain: ".medium.com", path: "/", secure: true, httpOnly: false },
    { name: "sid", value: "", domain: ".medium.com", path: "/", secure: true, httpOnly: false },
  ]);
  const [rawCookieStr, setRawCookieStr] = useState("");
  const [cookieMode, setCookieMode] = useState<"table" | "raw">("raw");
  const [activeTab, setActiveTab] = useState<"url" | "cookies">("url");
  const [job, setJob] = useState<JobStatus | null>(null);
  const [loading, setLoading] = useState(false);
  const [stopping, setStopping] = useState(false);
  const [fetchingCookies, setFetchingCookies] = useState(false);
  const [fetchCookieStatus, setFetchCookieStatus] = useState<string>("");
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const logsEndRef = useRef<HTMLDivElement>(null);
  const esRef = useRef<EventSource | null>(null);

  useEffect(() => {
    logsEndRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [job?.logs.length]);

  const addCookie = () =>
    setCookies((c) => [...c, { name: "", value: "", domain: ".medium.com", path: "/", secure: true, httpOnly: false }]);

  const removeCookie = (i: number) =>
    setCookies((c) => c.filter((_, idx) => idx !== i));

  const updateCookie = (i: number, field: keyof CookieType, val: string) =>
    setCookies((c) => c.map((row, idx) => idx === i ? { ...row, [field]: val } : row));

  const applyRawCookies = () => {
    const parsed = parseRawCookies(rawCookieStr);
    if (parsed.length) {
      setCookies(parsed);
      setCookieMode("table");
    }
  };

  // Cookie bridge: open helper page in user's real browser, poll for result
  const autoFetchCookies = async () => {
    setFetchingCookies(true);
    setFetchCookieStatus("Starting session…");
    try {
      const res = await fetch(`${API}/api/cookie-session`, { method: "POST" });
      if (!res.ok) { setFetchCookieStatus("Failed to start session"); setFetchingCookies(false); return; }
      const { token, helperURL } = await res.json();

      // Open the helper page in a new tab
      window.open(helperURL, "_blank");
      setFetchCookieStatus("Helper page opened — follow the steps there, then come back here.");

      // Poll until cookies arrive (max 10 min)
      if (pollRef.current) clearInterval(pollRef.current);
      const deadline = Date.now() + 10 * 60 * 1000;
      pollRef.current = setInterval(async () => {
        if (Date.now() > deadline) {
          clearInterval(pollRef.current!);
          setFetchingCookies(false);
          setFetchCookieStatus("Timed out — try again.");
          return;
        }
        try {
          const pr = await fetch(`${API}/api/cookie-session/${token}`);
          if (pr.status === 202) return; // still pending
          if (!pr.ok) { clearInterval(pollRef.current!); setFetchingCookies(false); return; }
          const data = await pr.json();
          clearInterval(pollRef.current!);
          const fetched: CookieType[] = data.cookies ?? [];
          if (fetched.length > 0) {
            setCookies(fetched);
            setCookieMode("table");
            setFetchCookieStatus(`✅ Got ${fetched.length} cookies — ready to harvest!`);
          } else {
            setFetchCookieStatus("No cookies received — try again.");
          }
          setFetchingCookies(false);
        } catch { /* keep polling */ }
      }, 2000);
    } catch (e) {
      setFetchCookieStatus(`Error: ${e}`);
      setFetchingCookies(false);
    }
  };

  const startStream = useCallback((jobId: string) => {
    esRef.current?.close();
    const es = new EventSource(`${API}/api/jobs/${jobId}/stream`);
    esRef.current = es;
    es.onmessage = (e) => {
      const entry: LogEntry = JSON.parse(e.data);
      setJob((prev) => prev ? { ...prev, logs: [...prev.logs, entry] } : prev);
    };
    es.addEventListener("done", async () => {
      es.close();
      const res = await fetch(`${API}/api/jobs/${jobId}`);
      const status: JobStatus = await res.json();
      setJob(status);
      setLoading(false);
      setStopping(false);
    });
    es.onerror = () => { es.close(); setLoading(false); setStopping(false); };
  }, []);

  const harvest = async () => {
    if (!url.trim()) return;
    setLoading(true);
    setStopping(false);
    setJob(null);

    const activeCookies = cookieMode === "raw"
      ? parseRawCookies(rawCookieStr)
      : cookies.filter((c) => c.name && c.value);

    const res = await fetch(`${API}/api/harvest`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ url: url.trim(), cookies: activeCookies }),
    });
    if (!res.ok) { setLoading(false); return; }
    const { jobID } = await res.json();
    setJob({ id: jobID, url: url.trim(), state: "running", logs: [], files: [] });
    startStream(jobID);
  };

  const stopHarvest = async () => {
    if (!job?.id) return;
    setStopping(true);
    await fetch(`${API}/api/jobs/${job.id}/stop`, { method: "POST" });
  };

  const isRunning = loading && job?.state === "running";

  const stateColor =
    job?.state === "done" ? "var(--ok)" :
    job?.state === "failed" ? "var(--err)" :
    job?.state === "stopped" ? "var(--warn)" : "var(--accent)";

  const activeCookieCount = cookieMode === "raw"
    ? parseRawCookies(rawCookieStr).length
    : cookies.filter((c) => c.name && c.value).length;

  return (
    <div className={styles.shell}>
      {/* ── Header ── */}
      <header className={styles.header}>
        <div className={styles.headerInner}>
          <div className={styles.logo}>
            <span className={styles.logoMark}>M</span>
            <span className={styles.logoText}>edium<em>Harvester</em></span>
          </div>
          <span className={`tag ${styles.versionTag}`}>v1.0</span>
        </div>
        <p className={styles.tagline}>
          Convert any Medium page + its linked articles to offline, cross-linked PDFs
        </p>
      </header>

      <main className={styles.main}>
        {/* ── Input Panel ── */}
        <section className={styles.inputPanel}>
          <div className={styles.tabBar}>
            <button
              className={`${styles.tab} ${activeTab === "url" ? styles.tabActive : ""}`}
              onClick={() => setActiveTab("url")}
            >
              <Globe size={14} /> URL
            </button>
            <button
              className={`${styles.tab} ${activeTab === "cookies" ? styles.tabActive : ""}`}
              onClick={() => setActiveTab("cookies")}
            >
              <Cookie size={14} /> Auth Cookies
              {activeCookieCount > 0 && (
                <span className={styles.cookieBadge}>{activeCookieCount}</span>
              )}
            </button>
          </div>

          <AnimatePresence mode="wait">
            {activeTab === "url" ? (
              <motion.div key="url" className={styles.tabContent}
                initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0, y: -8 }}>
                <div className={styles.urlInputWrap}>
                  <Globe size={18} className={styles.urlIcon} />
                  <input
                    className={styles.urlInput}
                    placeholder="https://medium.com/@author/article-slug"
                    value={url}
                    onChange={(e) => setUrl(e.target.value)}
                    onKeyDown={(e) => e.key === "Enter" && harvest()}
                    spellCheck={false}
                  />
                </div>
                <p className={styles.hint}>
                  Paste the master Medium URL. All linked articles (1 level deep) will be
                  harvested, converted to PDF, and cross-linked. Add cookies to unlock
                  member-only content.
                </p>
              </motion.div>
            ) : (
              <motion.div key="cookies" className={styles.tabContent}
                initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0, y: -8 }}>

                {/* Auto-fetch banner */}
                <div className={styles.autoFetchBanner}>
                  <div className={styles.autoFetchInfo}>
                    <Sparkles size={15} />
                    <span>Opens a guided helper page in your browser — no DevTools needed</span>
                  </div>
                  <button
                    className={styles.autoFetchBtn}
                    onClick={autoFetchCookies}
                    disabled={fetchingCookies}
                  >
                    {fetchingCookies
                      ? <><Loader2 size={14} className={styles.spin} /> Waiting for cookies…</>
                      : <><Sparkles size={14} /> Open cookie helper</>}
                  </button>
                  {fetchCookieStatus && (
                    <p className={styles.fetchStatus}>{fetchCookieStatus}</p>
                  )}
                </div>

                <div className={styles.orDivider}><span>or enter manually</span></div>

                {/* Mode switcher */}
                <div className={styles.modeSwitcher}>
                  <button className={`${styles.modeBtn} ${cookieMode === "raw" ? styles.modeBtnActive : ""}`}
                    onClick={() => setCookieMode("raw")}>
                    Raw string
                  </button>
                  <button className={`${styles.modeBtn} ${cookieMode === "table" ? styles.modeBtnActive : ""}`}
                    onClick={() => setCookieMode("table")}>
                    Table
                  </button>
                </div>

                {cookieMode === "raw" ? (
                  <div className={styles.rawBlock}>
                    <textarea
                      className={styles.rawInput}
                      rows={4}
                      placeholder={"uid=abc123; sid=xyz789; __cfruid=..."}
                      value={rawCookieStr}
                      onChange={(e) => setRawCookieStr(e.target.value)}
                      spellCheck={false}
                    />
                    <div className={styles.rawActions}>
                      {rawCookieStr && (
                        <span className={styles.hint}>
                          {parseRawCookies(rawCookieStr).length} cookies parsed
                        </span>
                      )}
                      <button className={styles.parseBtn} onClick={applyRawCookies}
                        disabled={!rawCookieStr.trim()}>
                        Parse → Table
                      </button>
                    </div>
                  </div>
                ) : (
                  <>
                    <div className={styles.cookieHeader}>
                      {["name", "value", "domain", "path"].map((h) => (
                        <span key={h} className={styles.cookieHeaderCell}>{h}</span>
                      ))}
                    </div>
                    <AnimatePresence>
                      {cookies.map((c, i) => (
                        <CookieRow key={i} cookie={c} index={i}
                          onChange={updateCookie} onRemove={removeCookie} />
                      ))}
                    </AnimatePresence>
                    <button className={styles.addCookieBtn} onClick={addCookie}>
                      <Plus size={14} /> Add cookie
                    </button>
                  </>
                )}
              </motion.div>
            )}
          </AnimatePresence>

          <div className={styles.actionRow}>
            <button className={styles.harvestBtn} onClick={harvest}
              disabled={loading || !url.trim()}>
              {loading
                ? <><Loader2 size={18} className={styles.spin} /> Harvesting…</>
                : <><Play size={18} /> Harvest</>}
            </button>

            <AnimatePresence>
              {isRunning && (
                <motion.button
                  className={styles.stopBtn}
                  onClick={stopHarvest}
                  disabled={stopping}
                  initial={{ opacity: 0, scale: 0.9 }}
                  animate={{ opacity: 1, scale: 1 }}
                  exit={{ opacity: 0, scale: 0.9 }}
                >
                  {stopping
                    ? <><Loader2 size={16} className={styles.spin} /> Stopping…</>
                    : <><Square size={16} /> Stop</>}
                </motion.button>
              )}
            </AnimatePresence>
          </div>
        </section>

        {/* ── Output Panel ── */}
        <AnimatePresence>
          {job && (
            <motion.section className={styles.outputPanel}
              initial={{ opacity: 0, y: 24 }} animate={{ opacity: 1, y: 0 }}>
              <div className={styles.statusBar}>
                <Terminal size={14} />
                <span className={styles.statusLabel}>Job</span>
                <code className={styles.jobId}>{job.id}</code>
                <span className={styles.statePill}
                  style={{ borderColor: stateColor, color: stateColor }}>
                  {job.state}
                </span>
              </div>

              <div className={styles.logsBox}>
                {job.logs.map((e, i) => <LogLine key={i} entry={e} />)}
                <div ref={logsEndRef} />
              </div>

              {job.files.length > 0 && (
                <div className={styles.filesSection}>
                  <h3 className={styles.filesSectionTitle}>
                    <FileText size={14} /> Output files
                    <span className={styles.fileCount}>{job.files.length}</span>
                  </h3>
                  <div className={styles.filesGrid}>
                    {job.files.map((f, i) => (
                      <FileCard key={i} file={f} jobId={job.id} />
                    ))}
                  </div>
                </div>
              )}

              {job.error && (
                <div className={styles.errorBox}>
                  <XCircle size={16} /> {job.error}
                </div>
              )}
            </motion.section>
          )}
        </AnimatePresence>
      </main>

      <footer className={styles.footer}>
        <span>Medium Harvester</span>
        <span>·</span>
        <span>TypeScript + Go + Chromium</span>
      </footer>
    </div>
  );
}
