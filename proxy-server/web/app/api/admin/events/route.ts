import { NextRequest, NextResponse } from "next/server";
import { getJSON, errorResponse, forward } from "@/lib/proxy-client";
import { EventPageSchema } from "@/lib/schemas";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function GET(req: NextRequest) {
  try {
    const params = forward(req.nextUrl.searchParams, [
      "from",
      "to",
      "decision",
      "source",
      "q",
      "limit",
      "offset",
    ]);
    const data = await getJSON("/admin/events", params, EventPageSchema);
    return NextResponse.json(data);
  } catch (e) {
    return errorResponse(e);
  }
}
