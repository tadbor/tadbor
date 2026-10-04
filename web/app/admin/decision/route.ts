import { cookies } from "next/headers";
import { NextResponse } from "next/server";

import { COOKIE_NAME } from "../session";
import { AuthError, postDecision, type Decision } from "../reviewer-client";

const DECISIONS: Decision[] = ["approve", "edit_approve", "reject", "escalate"];

// The review form posts here. There is no client-side JavaScript in this app, so
// the decisions are plain HTML form submissions and the response is a redirect
// back to the queue with the outcome in the query string.
export async function POST(request: Request) {
  const password = (await cookies()).get(COOKIE_NAME)?.value;
  if (!password) {
    return NextResponse.redirect(new URL("/admin?error=not-logged-in", request.url), 303);
  }

  const form = await request.formData();
  const explanationId = String(form.get("explanation_id") ?? "");
  const decision = String(form.get("decision") ?? "") as Decision;
  const comments = String(form.get("comments") ?? "").trim();
  const text = String(form.get("text") ?? "");
  const reviewer = String(form.get("reviewer") ?? "").trim() || "reviewer";

  if (!explanationId) {
    return NextResponse.redirect(new URL("/admin?error=missing-explanation", request.url), 303);
  }
  if (!DECISIONS.includes(decision)) {
    return NextResponse.redirect(new URL("/admin?error=unknown-decision", request.url), 303);
  }

  // The checklist from Part 1 §20 is not decoration: approving without working
  // through it is the failure this project exists to prevent, so the form
  // requires it and the server refuses a decision that skipped it.
  if ((decision === "approve" || decision === "edit_approve") && !checklistComplete(form)) {
    return NextResponse.redirect(
      new URL(`/admin?error=checklist&explanation=${encodeURIComponent(explanationId)}`, request.url),
      303
    );
  }

  try {
    const result = await postDecision(explanationId, password, {
      reviewer_id: reviewer,
      decision,
      comments,
      text: decision === "edit_approve" ? text : undefined,
    });

    if (result.ok) {
      return NextResponse.redirect(new URL(`/admin?done=${encodeURIComponent(decision)}`, request.url), 303);
    }
    return NextResponse.redirect(
      new URL(
        `/admin?error=${encodeURIComponent(result.message)}&explanation=${encodeURIComponent(explanationId)}`,
        request.url
      ),
      303
    );
  } catch (err) {
    if (err instanceof AuthError) {
      const res = NextResponse.redirect(new URL("/admin?error=session-expired", request.url), 303);
      res.cookies.delete(COOKIE_NAME);
      return res;
    }
    return NextResponse.redirect(
      new URL(`/admin?error=${encodeURIComponent(String(err))}`, request.url),
      303
    );
  }
}

// Four checks from Part 1 §20, all required before approval.
const CHECKLIST = ["accuracy", "no_added_meaning", "correct_source", "clear_separation"];

function checklistComplete(form: FormData): boolean {
  return CHECKLIST.every((key) => form.get(`check_${key}`) === "on");
}