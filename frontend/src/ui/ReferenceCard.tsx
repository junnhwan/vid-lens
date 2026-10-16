import { useState } from "react";
import type { Citation } from "../contract/types";
import { HttpApi } from "../api/http";

export function ReferenceCard({
  citation,
  requestId,
}: {
  citation: Citation;
  requestId: string;
}) {
  const [body, setBody] = useState<string | null>(null),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false);
  async function read() {
    if (body !== null || busy || !citation.reference_id) return;
    setBusy(true);
    try {
      const r = await new HttpApi().request<{ body: string }>(
        "GET",
        `/api/v1/requests/${encodeURIComponent(requestId)}/references/${encodeURIComponent(citation.reference_id)}`,
      );
      setBody(r.body.body);
    } catch {
      setError("原文快照暂不可读或已过期；引用保留读取时的摘录。");
    } finally {
      setBusy(false);
    }
  }
  return (
    <details
      className="reference-card"
      id={`reference-${citation.reference_id}`}
      onToggle={(e) => {
        if (e.currentTarget.open) void read();
      }}
    >
      <summary>网络补充 · {citation.title || citation.label}</summary>
      <div>
        <a
          href={
            /^https?:\/\//.test(citation.url || "") ? citation.url : undefined
          }
          target="_blank"
          rel="noopener noreferrer"
        >
          {citation.url} ↗
        </a>
        <p className="reference-date">
          读取于{" "}
          {citation.read_at
            ? new Date(citation.read_at).toLocaleString("zh-Hans")
            : "未知"}
          {citation.content_sha256
            ? ` · SHA256 ${citation.content_sha256.slice(0, 12)}…`
            : ""}
        </p>
        <blockquote>{citation.quote}</blockquote>
        {busy ? <p>正在读取已保存的网页快照…</p> : null}
        {body !== null ? <div className="reference-body">{body}</div> : null}
        {error ? <p role="status">{error}</p> : null}
      </div>
    </details>
  );
}
