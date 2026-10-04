import { cookies } from "next/headers";

// Session handling for the reviewer dashboard.
//
// The "session" is the reviewer password itself, in an HttpOnly cookie. That is
// not how sessions are normally built, and it is not what Part 2 §27 asks for
// (Supabase Auth, real identities). It is what a single-reviewer MVP can carry
// without a user table, a token-signing scheme, or a password reset flow — and
// the gate it provides is a real one: the API independently requires the same
// password, so a cookie is never the only thing standing between /internal and
// the internet.
//
// When this stops being enough: a second reviewer, or any deployment where
// "who approved this" has to be attributable to a person. docs/reviewer-dashboard.md
// records what to replace it with.

export const COOKIE_NAME = "tadbor_reviewer";

export function passwordIsSet(): boolean {
  return (process.env.REVIEWER_PASSWORD ?? "") !== "";
}

// Returns the reviewer password from the cookie, or null when the reviewer is
// not logged in. Deliberately returns the password rather than a boolean: the
// caller needs it to authenticate to the API either way.
export async function reviewerPassword(): Promise<string | null> {
  const value = (await cookies()).get(COOKIE_NAME)?.value;
  if (!value) return null;

  // A stale cookie from a previous password is worse than no cookie: it produces
  // a confusing 401 deep in the API instead of a login form.
  const expected = process.env.REVIEWER_PASSWORD ?? "";
  if (expected === "" || value !== expected) return null;
  return value;
}

// The name typed at login, used only to fill in reviewer_id on decisions.
export async function reviewerName(): Promise<string> {
  return (await cookies()).get("tadbor_reviewer_name")?.value || "reviewer";
}