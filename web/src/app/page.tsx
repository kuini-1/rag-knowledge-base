"use client";

import { useRouter } from "next/navigation";
import { useEffect } from "react";
import { useSession } from "@/lib/session";

export default function Home() {
  const s = useSession();
  const router = useRouter();
  useEffect(() => {
    if (s !== undefined) router.replace(s ? "/ask" : "/login");
  }, [s, router]);
  return null;
}
