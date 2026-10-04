import { redirect } from "next/navigation";
import { NextResponse } from "next/server";

import { COOKIE_NAME, passwordIsSet } from "../session";

// The login form posts here. On success the password is stored in an HttpOnly
// cookie so that page loads can forward it to the API without any client-side
// JavaScript holding it.
export async function POST(request: Request) {
  if (!passwordIsSet()) {
    return NextResponse.json(
      { error: "REVIEWER_PASSWORD is not set on the web app, so no one can log in. /admin stays closed." },
      { status: 503 }
    );
  }

  const form = await request.formData();
  const reviewer = String(form.get("reviewer") ?? "").trim();
  const password = String(form.get("password") ?? "");
  const expected = process.env.REVIEWER_PASSWORD ?? "";

  if (password === "" || password !== expected) {
    return NextResponse.json({ error: "Wrong password." }, { status: 401 });
  }

  const res = NextResponse.redirect(
    new URL(sanitizeReturnTo(String(form.get("next") ?? "/admin")), request.url),
    303
  );
  const options = {
    httpOnly: true,
    sameSite: "lax" as const,
    path: "/",
    // Deliberately not `secure`: `make dev` runs on http://localhost, where a
    // Secure cookie would silently not be stored. Turn this on with the app
    // behind TLS (issue #15).
    secure: false,
    maxAge: 60 * 60 * 12,
  };
  res.cookies.set(COOKIE_NAME, password, options);
  // Only used to attribute the review record; the queue itself needs no identity.
  res.cookies.set("tadbor_reviewer_name", reviewer || "reviewer", options);
  return res;
}

// Keeps the login form's `next` parameter from being used as an open redirect.
function sanitizeReturnTo(next: string): string {
  if (!next.startsWith("/") || next.startsWith("//")) return "/admin";
  return next;
}

export const dynamic = "force-dynamic";

export async function GET() {
  redirect("/admin");
}