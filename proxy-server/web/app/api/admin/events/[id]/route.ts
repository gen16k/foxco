import { NextRequest, NextResponse } from "next/server";
import { getJSON, errorResponse } from "@/lib/proxy-client";
import { EventRowSchema } from "@/lib/schemas";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function GET(_req: NextRequest, { params }: { params: { id: string } }) {
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
