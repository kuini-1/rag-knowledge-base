"use client";

import { useRouter } from "next/navigation";
import { useEffect, useSyncExternalStore } from "react";
import { loadSession, type Session } from "./api";

function subscribe(cb: () => void) {
  window.addEventListener("rag-session", cb);
  window.addEventListener("storage", cb);
  return () => {
    window.removeEventListener("rag-session", cb);
    window.removeEventListener("storage", cb);
  };
}

let cached: { raw: string | null; s: Session | null } = { raw: null, s: null };

function snapshot(): Session | null {
  let raw: string | null = null;
  try {
    raw = localStorage.getItem("rag.session");
  } catch {
    raw = null;
  }
  if (raw !== cached.raw) cached = { raw, s: loadSession() };
  return cached.s;
}

/** The signed-in session, or null. `undefined` while rendering on the server. */
export function useSession(): Session | null | undefined {
  return useSyncExternalStore(subscribe, snapshot, () => undefined);
}

/** Sends signed-out visitors to the login page. */
export function useRequireSession(): Session | null | undefined {
  const s = useSession();
  const router = useRouter();
  useEffect(() => {
    if (s === null) router.replace("/login");
  }, [s, router]);
  return s;
}
