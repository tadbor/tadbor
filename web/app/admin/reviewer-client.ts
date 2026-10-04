// Server-side client for the reviewer's half of the API.
//
// Everything here runs on the Next.js server: the browser never sees the API
// URL or the reviewer password. The dashboard is a set of server components and
// two route handlers, with no client-side JavaScript, so there is no code path
// that could call these endpoints from the browser in the first place.
//
// This is deliberately not the reader's app/api-client. That module is a
// NEXT_PUBLIC_ consumer and is bundled into the client; review tooling has no
// business in the reader's bundle. Nothing imports this module from a component
// that ships to the browser, so `process.env.REVIEWER_PASSWORD` stays server-side.

const PASSWORD_HEADER = "X-Reviewer-Password";

export type Evidence = {
  chunk_id: string;
  text: string;
  ayah_start: number;
  ayah_end: number;
  mapping_type: string;
  source_id: string;
  source_version: string;
  content_hash: string;
};

export type Citation = {
  source_id: string;
  title: string;
  author: string;
  edition?: string;
  version?: string;
};

export type QueueItem = {
  id: string;
  surah_id: number;
  ayah_start: number;
  ayah_end: number;
  style: string;
  level: string;
  text: string;
  source_refs: string[];
  review_status: string;
  source_version_snapshot: string;
  prompt_version?: string;
  model_version?: string;
  ayah_text: string;
  evidence: Evidence[];
  missing_refs: string[];
  citation: Citation;
  grounding_version_mismatch: boolean;
};

export type Decision = "approve" | "edit_approve" | "reject" | "escalate";

function apiUrl(): string {
  const url = process.env.NEXT_PUBLIC_API_URL;
  if (!url) {
    throw new Error("NEXT_PUBLIC_API_URL is not set");
  }
  return url.replace(/\/+$/, "");
}

async function call(path: string, init: RequestInit, password: string): Promise<Response> {
  return fetch(`${apiUrl()}${path}`, {
    ...init,
    cache: "no-store",
    headers: { ...(init.headers ?? {}), [PASSWORD_HEADER]: password },
  });
}

export async function fetchQueue(password: string): Promise<QueueItem[]> {
  const res = await call("/internal/review/queue", {}, password);
  if (res.status === 401) throw new AuthError();
  if (!res.ok) throw new Error(`review queue: ${res.status} ${await res.text()}`);

  const body: unknown = await res.json().catch(() => null);
  if (!Array.isArray(body)) throw new Error("review queue: response was not a list");
  return body.map(normalizeItem);
}

// Go serialises a nil slice as null, so an explanation with no refs arrives with
// `missing_refs: null` rather than `[]`. The page does `.length` and `.map` on
// both, and an explanation with nothing to cite is exactly the case that must not
// crash the tool a reviewer uses to notice it.
function normalizeItem(item: QueueItem): QueueItem {
  return {
    ...item,
    source_refs: asArray(item.source_refs),
    evidence: asArray(item.evidence),
    missing_refs: asArray(item.missing_refs),
  };
}

function asArray<T>(value: T[] | null | undefined): T[] {
  return Array.isArray(value) ? value : [];
}

export async function postDecision(
  explanationId: string,
  password: string,
  body: { reviewer_id: string; decision: Decision; comments?: string; text?: string }
): Promise<{ ok: true } | { ok: false; message: string }> {
  const res = await call(
    `/internal/review/${encodeURIComponent(explanationId)}/decision`,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    },
    password
  );

  if (res.status === 401) throw new AuthError();
  if (res.status === 409) {
    return { ok: false, message: "This item has already been decided. Reload the queue." };
  }
  if (!res.ok) {
    const detail: unknown = await res.json().catch(() => null);
    const message =
      detail && typeof detail === "object" && "error" in detail
        ? String((detail as { error: unknown }).error)
        : `${res.status}`;
    return { ok: false, message };
  }
  return { ok: true };
}

// Wrong or revoked password. Distinct from an API error because the only useful
// response is to send the reviewer back to the login form.
export class AuthError extends Error {}