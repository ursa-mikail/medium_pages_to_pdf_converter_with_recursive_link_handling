export interface Cookie {
  name: string;
  value: string;
  domain: string;
  path: string;
  secure: boolean;
  httpOnly: boolean;
}

export interface LogEntry {
  time: string;
  level: "info" | "ok" | "warn" | "error";
  message: string;
}

export interface FileResult {
  name: string;
  path: string;
  url: string;
}

export interface JobStatus {
  id: string;
  url: string;
  state: "queued" | "running" | "done" | "failed" | "stopped";
  logs: LogEntry[];
  files: FileResult[];
  error?: string;
}
