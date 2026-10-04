import { NextResponse } from "next/server";

import { COOKIE_NAME } from "../session";

// Signs the reviewer out. POST only, so a stray link or an image tag cannot log
// anyone out.
export async function POST(request: Request) {
  const res = NextResponse.redirect(new URL("/admin", request.url), 303);
  res.cookies.delete(COOKIE_NAME);
  res.cookies.delete("tadbor_reviewer_name");
  return res;
}

export const dynamic = "force-dynamic";