import { NextRequest, NextResponse } from "next/server";
import { getJSON, errorResponse, forward } from "@/lib/proxy-client";
import { EventPageSchema } from "@/lib/schemas";
import { mockEnabled, mockEvents } from "@/lib/mock";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function GET(req: NextRequest) {
  if (mockEnabled()) return NextResponse.json(mockEvents(req.nextUrl.searchParams));
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
