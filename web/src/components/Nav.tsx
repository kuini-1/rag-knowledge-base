"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { api, fmtNum, saveSession, type MonthUsage } from "@/lib/api";
import { useSession } from "@/lib/session";

const links = [
  { href: "/ask", label: "Ask" },
  { href: "/documents", label: "Documents" },
  { href: "/history", label: "History" },
];

export function Nav() {
  const s = useSession();
  const path = usePathname();
  const router = useRouter();
  const [usage, setUsage] = useState<MonthUsage | null>(null);

  useEffect(() => {
    if (!s) return;
    const load = () => api.me().then((r) => setUsage({ ...r.usage, budget: r.user.token_budget })).catch(() => {});
    load();
    window.addEventListener("rag-usage", load);
    return () => window.removeEventListener("rag-usage", load);
  }, [s]);

  const pct = usage && usage.budget > 0 ? Math.min(100, (usage.tokens / usage.budget) * 100) : 0;

  return (
    <header className="border-b border-line bg-surface">
      <div className="mx-auto flex max-w-6xl flex-wrap items-center gap-x-6 gap-y-2 px-4 py-3 sm:px-6">
        <Link href="/" className="flex items-center gap-2 font-semibold">
          <span className="grid h-6 w-6 place-items-center rounded-md bg-accent text-xs text-accent-ink">KB</span>
          Knowledge Base
        </Link>
        {s && (
          <nav className="flex gap-1">
            {links.map((l) => (
              <Link
                key={l.href}
                href={l.href}
                className={`rounded-md px-3 py-1.5 text-sm ${path.startsWith(l.href) ? "bg-sunken font-medium" : "text-muted hover:text-ink"}`}
              >
                {l.label}
              </Link>
            ))}
          </nav>
        )}
        {s && (
          <div className="ml-auto flex items-center gap-4 text-sm">
            {usage && (
              <div className="hidden min-w-44 sm:block" title={`${fmtNum(usage.tokens)} of ${fmtNum(usage.budget)} tokens this month`}>
                <div className="flex justify-between text-xs text-muted">
                  <span>Tokens this month</span>
                  <span className="tabular-nums">{Math.round(pct)}%</span>
                </div>
                <div className="mt-1 h-1.5 overflow-hidden rounded-full bg-sunken">
                  <div className={`h-full ${pct > 90 ? "bg-bad" : "bg-accent"}`} style={{ width: `${pct}%` }} />
                </div>
              </div>
            )}
            <span className="hidden text-muted md:inline">{s.user.email}</span>
            <button
              className="btn"
              onClick={() => {
                saveSession(null);
                router.replace("/login");
              }}
            >
              Sign out
            </button>
          </div>
        )}
      </div>
    </header>
  );
}
