import { NextRequest, NextResponse } from "next/server";
import { getJSON, errorResponse, forward } from "@/lib/proxy-client";
import { StatsSchema } from "@/lib/schemas";
import { mockEnabled, mockStats } from "@/lib/mock";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function GET(req: NextRequest) {
  if (mockEnabled()) return NextResponse.json(mockStats(req.nextUrl.searchParams));
  try {
    const params = forward(req.nextUrl.searchParams, ["from", "to"]);
    const data = await getJSON("/admin/stats", params, StatsSchema);
    return NextResponse.json(data);
  } catch (e) {
    return errorResponse(e);
  }
}
