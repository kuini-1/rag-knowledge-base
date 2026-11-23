"use client";

import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { api, saveSession } from "@/lib/api";
import { useSession } from "@/lib/session";

export default function LoginPage() {
  const router = useRouter();
  const session = useSession();
  useEffect(() => {
    if (session) router.replace("/ask");
  }, [session, router]);
  const [mode, setMode] = useState<"login" | "register">("login");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const s = mode === "login" ? await api.login(email, password) : await api.register(email, password);
      saveSession(s);
      router.replace(mode === "register" ? "/documents" : "/ask");
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="mx-auto mt-10 max-w-sm">
      <h1 className="text-xl font-semibold">{mode === "login" ? "Sign in" : "Create an account"}</h1>
      <p className="mt-1 text-sm text-muted">
        Upload your documents, then ask questions. Every answer cites the passages it came from.
      </p>
      <form onSubmit={submit} className="card mt-6 space-y-4 p-5">
        <label className="block text-sm">
          <span className="font-medium">Email</span>
          <input className="input mt-1" type="email" autoComplete="email" required value={email} onChange={(e) => setEmail(e.target.value)} />
        </label>
        <label className="block text-sm">
          <span className="font-medium">Password</span>
          <input
            className="input mt-1"
            type="password"
            autoComplete={mode === "login" ? "current-password" : "new-password"}
            minLength={mode === "register" ? 10 : undefined}
            required
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
          {mode === "register" && <span className="mt-1 block text-xs text-muted">At least 10 characters.</span>}
        </label>
        {error && <p className="rounded-lg bg-bad-bg px-3 py-2 text-sm text-bad">{error}</p>}
        <button className="btn btn-primary w-full" disabled={busy}>
          {busy ? "Please wait…" : mode === "login" ? "Sign in" : "Create account"}
        </button>
      </form>
      <p className="mt-4 text-center text-sm text-muted">
        {mode === "login" ? "No account yet? " : "Already have an account? "}
        <button className="font-medium text-accent hover:underline" onClick={() => { setMode(mode === "login" ? "register" : "login"); setError(""); }}>
          {mode === "login" ? "Create one" : "Sign in"}
        </button>
      </p>
    </div>
  );
}
