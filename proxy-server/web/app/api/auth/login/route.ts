import { getIronSession } from "iron-session";
import { cookies } from "next/headers";
import { sessionOptions, type SessionData } from "@/lib/session";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function POST(req: Request) {
  let body: { username?: string; password?: string };
  try {
    body = await req.json();
  } catch {
    return Response.json({ error: "bad_request", message: "invalid body" }, { status: 400 });
  }

  const expectedUser = process.env.ADMIN_USERNAME ?? "admin";
  const expectedPass = process.env.ADMIN_PASSWORD ?? "admin";
  if (body.username !== expectedUser || body.password !== expectedPass) {
    return Response.json(
      { error: "invalid_credentials", message: "ユーザー名またはパスワードが違います。" },
      { status: 401 },
    );
  }

  const session = await getIronSession<SessionData>(cookies(), sessionOptions);
  session.authenticated = true;
  session.username = body.username;
  await session.save();
  return Response.json({ ok: true });
}
