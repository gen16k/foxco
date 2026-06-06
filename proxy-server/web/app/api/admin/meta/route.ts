import { NextResponse } from "next/server";
import { getJSON, errorResponse } from "@/lib/proxy-client";
import { MetaSchema } from "@/lib/schemas";
import { mockEnabled, mockMeta } from "@/lib/mock";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function GET() {
  if (mockEnabled()) return NextResponse.json(mockMeta());
  try {
    const data = await getJSON("/admin/meta", new URLSearchParams(), MetaSchema);
    return NextResponse.json(data);
  } catch (e) {
    return errorResponse(e);
  }
}
