import { getIronSession } from "iron-session";
import { cookies } from "next/headers";
import { sessionOptions, type SessionData } from "@/lib/session";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function POST() {
  const session = await getIronSession<SessionData>(cookies(), sessionOptions);
  session.destroy();
  return Response.json({ ok: true });
}
