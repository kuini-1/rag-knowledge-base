"use client";

import { useState } from "react";
import { api, fmtNum, type AskResponse, type Citation, type Source } from "@/lib/api";

const riskStyle = {
  low: "bg-ok-bg text-ok",
  medium: "bg-warn-bg text-warn",
  high: "bg-bad-bg text-bad",
};

const riskLabel = {
  low: "Supported by sources",
  medium: "Partly supported",
  high: "Check before trusting",
};

export function AnswerView({ answer }: { answer: AskResponse }) {
  const [active, setActive] = useState<number | null>(null);
  const [vote, setVote] = useState<boolean | null>(null);
  const c = answer.check;

  return (
    <div className="grid items-start gap-4 lg:grid-cols-[1fr_380px]">
      <div className="card p-4">
        <div className="mb-3 flex flex-wrap items-center gap-2 text-xs">
          <span className={`rounded-full px-2 py-0.5 font-semibold ${riskStyle[c.hallucination_risk]}`}>
            {c.abstained ? "Not in your documents" : riskLabel[c.hallucination_risk]}
          </span>
          {!c.abstained && <span className="text-muted">{Math.round(c.grounded * 100)}% of the answer is cited</span>}
          {c.retrieval_confidence >= 0 && (
            <span className="text-muted">· retrieval confidence {c.retrieval_confidence.toFixed(2)}</span>
          )}
        </div>

        <p className="leading-relaxed">
          {answer.segments.map((seg, i) => (
            <span key={i}>
              <span className={seg.citations?.length ? "" : "text-ink"}>{seg.text}</span>
              {uniqueSources(seg.citations).map((cit) => (
                <button
                  key={cit.index}
                  onMouseEnter={() => setActive(cit.index)}
                  onFocus={() => setActive(cit.index)}
                  onClick={() => setActive(cit.index)}
                  className={`mx-0.5 inline-grid h-4 min-w-4 place-items-center rounded px-1 align-super text-[10px] font-semibold ${
                    cit.verified ? "bg-accent/15 text-accent" : "bg-bad-bg text-bad"
                  }`}
                  title={cit.verified ? cit.cited_text : "This quote was not found in the source"}
                  aria-label={`Source ${cit.index + 1}`}
                >
                  {cit.index + 1}
                </button>
              ))}
            </span>
          ))}
        </p>

        {c.warnings && c.warnings.length > 0 && (
          <ul className="mt-4 space-y-1 rounded-lg bg-warn-bg px-3 py-2 text-sm text-warn">
            {c.warnings.map((w) => (
              <li key={w}>⚠ {w}</li>
            ))}
          </ul>
        )}

        <div className="mt-4 flex flex-wrap items-center gap-3 border-t border-line pt-3 text-xs text-muted">
          <span>
            {answer.usage.model ? `${answer.usage.model} · ` : ""}
            {fmtNum(answer.usage.input_tokens + answer.usage.output_tokens)} tokens · {(answer.latency_ms / 1000).toFixed(1)} s
          </span>
          <span className="ml-auto flex items-center gap-1">
            {vote === null ? (
              <>
                Helpful?
                {[true, false].map((v) => (
                  <button
                    key={String(v)}
                    className="btn px-2 py-0.5 text-xs"
                    onClick={() => api.feedback(answer.query_id, v).then(() => setVote(v)).catch(() => {})}
                  >
                    {v ? "Yes" : "No"}
                  </button>
                ))}
              </>
            ) : (
              <span>Thanks{vote ? "" : ", this answer is now in your review list"}.</span>
            )}
          </span>
        </div>
      </div>

      <ol className="space-y-2">
        {answer.sources.map((s, i) => (
          <SourceCard key={s.id} n={i} source={s} active={active === i} quotes={quotesFor(answer, i)} onHover={() => setActive(i)} />
        ))}
        {answer.sources.length === 0 && <li className="text-sm text-muted">No sources found.</li>}
      </ol>
    </div>
  );
}

function uniqueSources(cits: Citation[] | null): Citation[] {
  const seen = new Set<number>();
  return (cits ?? []).filter((c) => !seen.has(c.index) && (seen.add(c.index), true));
}

function quotesFor(a: AskResponse, i: number): string[] {
  return a.segments.flatMap((s) => (s.citations ?? []).filter((c) => c.index === i && c.verified).map((c) => c.cited_text));
}

function SourceCard({ n, source, active, quotes, onHover }: {
  n: number; source: Source; active: boolean; quotes: string[]; onHover: () => void;
}) {
  const p = source.payload;
  // Drop the "Title > Section" context line that was added for embedding.
  const body = p.text.includes("\n") ? p.text.slice(p.text.indexOf("\n") + 1) : p.text;
  const cited = quotes.length > 0;
  return (
    <li
      onMouseEnter={onHover}
      className={`card p-3 text-sm transition ${active ? "border-accent ring-2 ring-accent/20" : ""} ${cited ? "" : "opacity-70"}`}
    >
      <div className="flex items-start gap-2">
        <span className="grid h-5 min-w-5 place-items-center rounded bg-sunken text-[11px] font-semibold">{n + 1}</span>
        <div className="min-w-0 flex-1">
          <div className="truncate font-medium" title={p.doc_name}>{p.doc_name}</div>
          <div className="truncate text-xs text-muted">{p.section.slice(1).join(" › ") || p.title}</div>
        </div>
        {source.rerank_score !== undefined && (
          <span className="text-xs tabular-nums text-muted" title="Reranker score">
            {source.rerank_score.toFixed(1)}
          </span>
        )}
      </div>
      <p className="mt-2 line-clamp-6 whitespace-pre-line text-muted">
        <Highlight text={body} quotes={quotes} />
      </p>
    </li>
  );
}

function Highlight({ text, quotes }: { text: string; quotes: string[] }) {
  const q = quotes.map((x) => x.trim()).find((x) => x && text.includes(x));
  if (!q) return <>{text}</>;
  const i = text.indexOf(q);
  return (
    <>
      {text.slice(0, i)}
      <mark className="rounded bg-mark px-0.5 text-ink">{q}</mark>
      {text.slice(i + q.length)}
    </>
  );
}
