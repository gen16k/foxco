import { NextRequest, NextResponse } from "next/server";
import { getJSON, errorResponse } from "@/lib/proxy-client";
import { EventRowSchema } from "@/lib/schemas";
import { mockEnabled, mockEvent } from "@/lib/mock";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function GET(_req: NextRequest, { params }: { params: { id: string } }) {
  if (mockEnabled()) {
    const row = mockEvent(params.id);
    return row
      ? NextResponse.json(row)
      : NextResponse.json({ error: "not_found", message: "event not found" }, { status: 404 });
  }
  try {
    const data = await getJSON(
      `/admin/events/${encodeURIComponent(params.id)}`,
      new URLSearchParams(),
      EventRowSchema,
    );
    return NextResponse.json(data);
  } catch (e) {
    return errorResponse(e);
  }
}
