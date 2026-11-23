"use client";

import { useEffect, useState } from "react";
import { api, type QueryLog } from "@/lib/api";
import { useRequireSession } from "@/lib/session";

const riskStyle: Record<string, string> = {
  low: "bg-ok-bg text-ok",
  medium: "bg-warn-bg text-warn",
  high: "bg-bad-bg text-bad",
};

export default function HistoryPage() {
  const s = useRequireSession();
  const [flagged, setFlagged] = useState(false);
  const [rows, setRows] = useState<QueryLog[] | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!s) return;
    api.queries(flagged).then(setRows).catch((e) => setError(e.message));
  }, [s, flagged]);

  if (!s) return null;

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold">History</h1>
          <p className="text-sm text-muted">
            &quot;Needs review&quot; shows answers that were flagged, had low retrieval confidence, or that you marked unhelpful.
          </p>
        </div>
        <div className="flex rounded-lg border border-line bg-surface p-0.5 text-sm">
          {[false, true].map((f) => (
            <button
              key={String(f)}
              onClick={() => setFlagged(f)}
              className={`rounded-md px-3 py-1 ${flagged === f ? "bg-sunken font-medium" : "text-muted"}`}
            >
              {f ? "Needs review" : "All"}
            </button>
          ))}
        </div>
      </div>

      {error && <p className="rounded-lg bg-bad-bg px-3 py-2 text-sm text-bad">{error}</p>}
      {rows && rows.length === 0 && <p className="text-sm text-muted">Nothing here yet.</p>}

      <ul className="space-y-2">
        {rows?.map((q) => (
          <li key={q.id} className="card p-4">
            <div className="flex flex-wrap items-center gap-2 text-xs">
              <span className={`rounded-full px-2 py-0.5 font-semibold ${riskStyle[q.risk] ?? ""}`}>{q.risk} risk</span>
              {q.feedback < 0 && <span className="rounded-full bg-bad-bg px-2 py-0.5 font-semibold text-bad">marked unhelpful</span>}
              {q.confidence >= 0 && q.confidence < 0.2 && <span className="rounded-full bg-warn-bg px-2 py-0.5 font-semibold text-warn">low retrieval confidence</span>}
              <span className="ml-auto text-muted">{new Date(q.created_at).toLocaleString()}</span>
            </div>
            <p className="mt-2 font-medium">{q.question}</p>
            <p className="mt-1 line-clamp-3 text-sm text-muted">{q.answer}</p>
            <p className="mt-2 text-xs text-muted">
              Sources: {q.top_sources || "none"} · {Math.round(q.grounded * 100)}% cited · {q.tokens.toLocaleString()} tokens · {(q.latency_ms / 1000).toFixed(1)} s
            </p>
          </li>
        ))}
      </ul>
    </div>
  );
}
