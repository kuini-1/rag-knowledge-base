"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { api, fmtBytes, type Doc } from "@/lib/api";
import { useRequireSession } from "@/lib/session";

const ACCEPT = ".pdf,.docx,.md,.markdown,.txt";

export default function DocumentsPage() {
  const s = useRequireSession();
  const [docs, setDocs] = useState<Doc[] | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [drag, setDrag] = useState(false);
  const input = useRef<HTMLInputElement>(null);

  const load = useCallback(() => api.documents().then(setDocs).catch((e) => setError(e.message)), []);

  useEffect(() => {
    if (s) load();
  }, [s, load]);

  // Poll while anything is still being indexed.
  useEffect(() => {
    if (!docs?.some((d) => d.status === "processing")) return;
    const t = setInterval(load, 1500);
    return () => clearInterval(t);
  }, [docs, load]);

  if (!s) return null;

  const upload = async (files: File[]) => {
    if (files.length === 0) return;
    setBusy(true);
    setError("");
    try {
      for (let i = 0; i < files.length; i += 5) await api.upload(files.slice(i, i + 5));
      await load();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
      if (input.current) input.current.value = "";
    }
  };

  const remove = async (d: Doc) => {
    if (!confirm(`Delete ${d.name}? Its passages will no longer be used in answers.`)) return;
    try {
      await api.deleteDocument(d.id);
      await load();
    } catch (e) {
      setError((e as Error).message);
    }
  };

  return (
    <div className="space-y-5">
      <div>
        <h1 className="text-xl font-semibold">Documents</h1>
        <p className="text-sm text-muted">PDF, Word (.docx), Markdown and text files, up to 20 MB each. Only you can search your documents.</p>
      </div>

      <div
        onDragOver={(e) => { e.preventDefault(); setDrag(true); }}
        onDragLeave={() => setDrag(false)}
        onDrop={(e) => { e.preventDefault(); setDrag(false); upload(Array.from(e.dataTransfer.files)); }}
        className={`card grid place-items-center border-dashed px-4 py-10 text-center transition ${drag ? "border-accent bg-accent/5" : ""}`}
      >
        <p className="font-medium">{busy ? "Uploading…" : "Drop files here"}</p>
        <p className="mt-1 text-sm text-muted">or</p>
        <button className="btn mt-2" disabled={busy} onClick={() => input.current?.click()}>
          Choose files
        </button>
        <input ref={input} type="file" multiple accept={ACCEPT} className="hidden" onChange={(e) => upload(Array.from(e.target.files ?? []))} />
      </div>

      {error && <p className="rounded-lg bg-bad-bg px-3 py-2 text-sm text-bad">{error}</p>}

      {docs && docs.length === 0 && <p className="text-sm text-muted">No documents yet.</p>}
      {docs && docs.length > 0 && (
        <div className="card overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="text-left text-xs text-muted">
              <tr className="border-b border-line">
                <th className="px-4 py-2 font-medium">Name</th>
                <th className="px-4 py-2 font-medium">Status</th>
                <th className="px-4 py-2 text-right font-medium">Passages</th>
                <th className="hidden px-4 py-2 text-right font-medium sm:table-cell">Size</th>
                <th className="hidden px-4 py-2 font-medium sm:table-cell">Added</th>
                <th className="px-4 py-2" />
              </tr>
            </thead>
            <tbody>
              {docs.map((d) => (
                <tr key={d.id} className="border-b border-line last:border-0">
                  <td className="px-4 py-2.5 font-medium">
                    {d.name}
                    {d.error && <div className="text-xs font-normal text-bad">{d.error}</div>}
                  </td>
                  <td className="px-4 py-2.5"><Status s={d.status} /></td>
                  <td className="px-4 py-2.5 text-right tabular-nums">{d.status === "ready" ? d.chunks : "–"}</td>
                  <td className="hidden px-4 py-2.5 text-right tabular-nums text-muted sm:table-cell">{fmtBytes(d.size)}</td>
                  <td className="hidden px-4 py-2.5 text-muted sm:table-cell">{new Date(d.created_at).toLocaleDateString()}</td>
                  <td className="px-4 py-2.5 text-right">
                    <button className="text-xs text-bad hover:underline" onClick={() => remove(d)}>Delete</button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

function Status({ s }: { s: Doc["status"] }) {
  const style = { processing: "bg-warn-bg text-warn", ready: "bg-ok-bg text-ok", failed: "bg-bad-bg text-bad" }[s];
  const label = { processing: "Indexing…", ready: "Ready", failed: "Failed" }[s];
  return <span className={`rounded-full px-2 py-0.5 text-xs font-semibold ${style}`}>{label}</span>;
}
