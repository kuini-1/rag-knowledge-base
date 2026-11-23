"use client";

import Link from "next/link";
import { useState } from "react";
import { api, type AskResponse } from "@/lib/api";
import { useRequireSession } from "@/lib/session";
import { AnswerView } from "@/components/AnswerView";

interface Turn {
  question: string;
  answer?: AskResponse;
  error?: string;
}

const examples = [
  "What is the request-per-minute limit on the Team plan?",
  "How long are snapshots kept on the Business plan?",
  "What does LS-3012 mean?",
];

export default function AskPage() {
  const s = useRequireSession();
  const [question, setQuestion] = useState("");
  const [turns, setTurns] = useState<Turn[]>([]);
  const [busy, setBusy] = useState(false);

  if (!s) return null;

  const ask = async (q: string) => {
    q = q.trim();
    if (!q || busy) return;
    setBusy(true);
    setQuestion("");
    setTurns((t) => [{ question: q }, ...t]);
    try {
      const a = await api.ask(q);
      setTurns((t) => [{ question: q, answer: a }, ...t.slice(1)]);
      window.dispatchEvent(new Event("rag-usage"));
    } catch (e) {
      setTurns((t) => [{ question: q, error: (e as Error).message }, ...t.slice(1)]);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="space-y-6">
      <form
        onSubmit={(e) => {
          e.preventDefault();
          ask(question);
        }}
        className="card flex gap-2 p-2"
      >
        <input
          className="input border-0 focus:ring-0"
          placeholder="Ask a question about your documents…"
          value={question}
          onChange={(e) => setQuestion(e.target.value)}
          maxLength={2000}
          autoFocus
        />
        <button className="btn btn-primary shrink-0" disabled={busy || !question.trim()}>
          {busy ? "Thinking…" : "Ask"}
        </button>
      </form>

      {turns.length === 0 && (
        <div className="text-sm text-muted">
          <p>
            Answers come only from <Link href="/documents" className="text-accent hover:underline">your documents</Link>.
            Each claim links to the passage it came from, and answers that go beyond the sources are flagged.
          </p>
          <div className="mt-3 flex flex-wrap gap-2">
            {examples.map((e) => (
              <button key={e} className="btn text-muted" onClick={() => ask(e)}>
                {e}
              </button>
            ))}
          </div>
        </div>
      )}

      {turns.map((t, i) => (
        <section key={turns.length - i} className="space-y-3">
          <h2 className="text-lg font-semibold">{t.question}</h2>
          {t.error && <p className="rounded-lg bg-bad-bg px-3 py-2 text-sm text-bad">{t.error}</p>}
          {!t.answer && !t.error && <p className="animate-pulse text-sm text-muted">Searching your documents and writing an answer…</p>}
          {t.answer && <AnswerView answer={t.answer} />}
        </section>
      ))}
    </div>
  );
}
