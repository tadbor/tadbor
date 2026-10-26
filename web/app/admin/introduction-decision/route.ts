import { NextResponse } from "next/server";

export async function POST() {
  return NextResponse.json(
    {
      error: "Introduction decisions are disabled until trusted reviewer identity and authorization are configured.",
    },
    { status: 503 }
  );
}