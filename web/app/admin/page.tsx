import Link from "next/link";

import "./admin.css";

import { passwordIsSet, reviewerName, reviewerPassword } from "./session";
import { AuthError, fetchQueue, type QueueItem } from "./reviewer-client";

// The review queue is re-read on every request: a reviewer's next decision
// depends on what other decisions have already landed.
export const dynamic = "force-dynamic";

// Part 1 §20. Every item must be checked against all four before it can be
// approved — they are the difference between a reviewer reading the explanation
// and a reviewer rubber-stamping it.
const CHECKLIST = [
  {
    key: "accuracy",
    label: "Every claim in the text is supported by the evidence above.",
  },
  {
    key: "no_added_meaning",
    label: "Nothing was added that the source does not say, and nothing was dropped that changes the meaning.",
  },
  {
    key: "correct_source",
    label: "The citation names the source the evidence actually came from.",
  },
  {
    key: "clear_separation",
    label: "The Quran's wording and the explanation stay clearly separate — no commentary is presented as scripture.",
  },
] as const;

export default async function AdminPage({
  searchParams,
}: {
  searchParams: { error?: string; done?: string; explanation?: string };
}) {
  if (!passwordIsSet()) {
    return (
      <main className="wrap">
        <h1>Reviewer dashboard</h1>
        <div className="banner banner-block">
          <strong>REVIEWER_PASSWORD is not set on the web app.</strong>
          <p>
            Nothing here is reachable until it is. Set it in <code>.env</code> and restart both{" "}
            <code>make dev</code> processes. This is deliberate: a missing password closes the
            dashboard rather than opening it.
          </p>
        </div>
      </main>
    );
  }

  const password = await reviewerPassword();
  if (!password) {
    return <LoginForm next="/admin" />;
  }

  let queue: QueueItem[];
  try {
    queue = await fetchQueue(password);
  } catch (err) {
    if (err instanceof AuthError) {
      return <LoginForm next="/admin" message="That password is no longer valid. Sign in again." />;
    }
    return (
      <main className="wrap">
        <h1>Reviewer dashboard</h1>
        <div className="banner banner-error">
          <strong>Could not load the queue.</strong>
          <p>{String(err)}</p>
          <p>Is the API running (<code>make dev</code>)?</p>
        </div>
      </main>
    );
  }

  const reviewer = await reviewerName();

  return (
    <main className="wrap">
      <header className="top">
        <div>
          <h1>Reviewer dashboard</h1>
          <p className="sub">
            {queue.length === 0
              ? "Nothing is waiting for review."
              : `${queue.length} explanation${queue.length === 1 ? "" : "s"} waiting for review.`}
          </p>
        </div>
        <form action="/admin/logout" method="post" className="top-actions">
          <span className="who">{reviewer}</span>
          <button type="submit" className="btn btn-quiet">
            Sign out
          </button>
        </form>
      </header>

      {searchParams.done && (
        <div className="banner banner-ok">
          Recorded: <strong>{humanDecision(searchParams.done)}</strong>. The item has left the queue.
        </div>
      )}
      {searchParams.error && (
        <div className="banner banner-error">
          {searchParams.error === "checklist" ? (
            <>
              Not saved. Working through the checklist is required before approving — that is the
              point of the gate.
            </>
          ) : searchParams.error === "session-expired" ? (
            <>Your password changed or expired. Sign in again.</>
          ) : (
            <>Not saved: {searchParams.error}</>
          )}
        </div>
      )}

      {queue.length === 0 ? (
        <p className="empty">
          Nothing pending. Generation writes explanations here with{" "}
          <code>review_status: &quot;pending&quot;</code> — nothing does yet, so this queue stays
          empty until the batch job in issue #10 runs.
        </p>
      ) : (
        queue.map((item) => <ReviewCard key={item.id} item={item} reviewer={reviewer} />)
      )}
    </main>
  );
}

function ReviewCard({ item, reviewer }: { item: QueueItem; reviewer: string }) {
  const ayahLabel =
    item.ayah_start === item.ayah_end
      ? `${item.surah_id}:${item.ayah_start}`
      : `${item.surah_id}:${item.ayah_start}-${item.ayah_end}`;

  // An explanation whose evidence cannot be read is not reviewable, and one
  // grounded in a source version that has since moved is not what the reviewer
  // thinks they are approving. Both block the approve buttons.
  const unresolvable = item.missing_refs.length > 0;
  const staleGrounding = item.grounding_version_mismatch;
  const blocked = unresolvable || staleGrounding;

  return (
    <section className="card">
      <div className="card-head">
        <h2>
          {ayahLabel} <span className="tag">{item.style}</span> <span className="tag">{item.level}</span>
        </h2>
        <code className="id">{item.id}</code>
      </div>

      {staleGrounding && (
        <div className="banner banner-block">
          <strong>Grounding version mismatch.</strong> This explanation was generated against{" "}
          <code>{item.source_version_snapshot || "an unrecorded source version"}</code>, but its
          evidence chunks were built from a different version. Approving it publishes text whose
          grounding no longer matches the registry (Part 1 §20). Reject it and regenerate.
        </div>
      )}

      <div className="panel">
        <h3>The verse</h3>
        {item.ayah_text ? (
          <p className="arabic" dir="rtl" lang="ar">
            {item.ayah_text}
          </p>
        ) : (
          <p className="missing">
            No verse text found in the corpus for {ayahLabel}. The explanation cannot be checked
            against a verse it does not have.
          </p>
        )}
      </div>

      <div className="panel">
        <h3>The explanation under review</h3>
        <p className="proposed" dir="rtl" lang="ar">
          {item.text || <em className="missing">empty — nothing to approve</em>}
        </p>
        <p className="meta">
          style <code>{item.style}</code> · level <code>{item.level}</code>
          {item.model_version ? (
            <>
              {" "}
              · model <code>{item.model_version}</code>
            </>
          ) : null}
          {item.prompt_version ? (
            <>
              {" "}
              · prompt <code>{item.prompt_version}</code>
            </>
          ) : null}
        </p>
      </div>

      <div className="panel">
        <h3>
          Evidence <span className="count">{item.evidence.length}</span>
        </h3>
        {item.citation.title ? (
          <p className="citation">
            {item.citation.title} — {item.citation.author}
            {item.citation.edition ? ` (${item.citation.edition})` : null}
            {item.citation.version ? ` · ${item.citation.version}` : null}
          </p>
        ) : (
          <p className="missing">
            No citation. Nothing here is attributable to a registered source, so there is nothing to
            check the text against.
          </p>
        )}

        {item.evidence.map((chunk) => (
          <details key={chunk.chunk_id} className="chunk">
            <summary>
              {chunk.mapping_type} · {chunk.ayah_start === chunk.ayah_end
                ? `ayah ${chunk.ayah_start}`
                : `ayahs ${chunk.ayah_start}-${chunk.ayah_end}`}{" "}
              <code>{chunk.chunk_id.slice(0, 12)}</code>
            </summary>
            <p dir="rtl" lang="ar">
              {chunk.text}
            </p>
            <p className="meta">
              <code>{chunk.content_hash}</code>
            </p>
          </details>
        ))}

        {unresolvable && (
          <p className="missing">
            {item.missing_refs.length} cited chunk
            {item.missing_refs.length === 1 ? "" : "s"} could not be found:{" "}
            {item.missing_refs.map((r) => (
              <code key={r}>{r.slice(0, 12)} </code>
            ))}
          </p>
        )}
      </div>

      <form action="/admin/decision" method="post" className="decide">
        <input type="hidden" name="explanation_id" value={item.id} />
        <input type="hidden" name="reviewer" value={reviewer} />

        <fieldset>
          <legend>Checklist — all four required to approve</legend>
          {CHECKLIST.map((c) => (
            <label key={c.key} className="check">
              <input type="checkbox" name={`check_${c.key}`} />
              <span>{c.label}</span>
            </label>
          ))}
        </fieldset>

        <label className="field">
          <span>Comments (recorded on the review either way)</span>
          <textarea name="comments" rows={2} placeholder="What you checked, or why you rejected it" />
        </label>

        <label className="field">
          <span>
            Corrected text — required for <em>Edit &amp; Approve</em>, ignored by the others
          </span>
          <textarea
            name="text"
            rows={4}
            dir="rtl"
            lang="ar"
            defaultValue={item.text}
            placeholder="Edit the explanation, then choose Edit & Approve"
          />
        </label>

        <div className="actions">
          <button type="submit" name="decision" value="approve" className="btn btn-approve">
            Approve as generated
          </button>
          <button type="submit" name="decision" value="edit_approve" className="btn btn-edit">
            Edit &amp; Approve
          </button>
          <button
            type="submit"
            name="decision"
            value="reject"
            className="btn btn-reject"
            formNoValidate
          >
            Reject
          </button>
          <button type="submit" name="decision" value="escalate" className="btn btn-quiet">
            Escalate
          </button>
        </div>

        {blocked && (
          <p className="missing">
            Approving is not blocked by the form, but it should not be done:{" "}
            {unresolvable ? "the cited evidence is unreadable. " : null}
            {staleGrounding ? "the grounding version does not match. " : null}
            Reject with a reason so the item is regenerated rather than published.
          </p>
        )}
      </form>
    </section>
  );
}

function LoginForm({ next, message }: { next: string; message?: string }) {
  return (
    <main className="wrap wrap-narrow">
      <h1>Reviewer dashboard</h1>
      <p className="sub">
        This is the publish gate for every explanation in the app. It is not a reader page.
      </p>

      {message && <div className="banner banner-error">{message}</div>}

      <form action="/admin/login" method="post" className="login">
        <input type="hidden" name="next" value={next} />
        <label className="field">
          <span>Your name (recorded on every decision)</span>
          <input type="text" name="reviewer" autoComplete="username" defaultValue="reviewer" />
        </label>
        <label className="field">
          <span>Reviewer password</span>
          <input type="password" name="password" autoComplete="current-password" required />
        </label>
        <button type="submit" className="btn btn-approve">
          Sign in
        </button>
      </form>

      <p className="empty">
        The password is <code>REVIEWER_PASSWORD</code>. It is the same value the API requires, so
        this dashboard is a convenience — not the thing keeping the review endpoints private.
      </p>
    </main>
  );
}

function humanDecision(decision: string): string {
  switch (decision) {
    case "approve":
      return "approved";
    case "edit_approve":
      return "edited and approved";
    case "reject":
      return "rejected";
    case "escalate":
      return "escalated — still in the queue";
    default:
      return decision;
  }
}