import type { SessionOptions } from "iron-session";

// Minimal single-user session. Credentials live in env (ADMIN_USERNAME /
// ADMIN_PASSWORD); a signed, httpOnly cookie carries only an "authenticated"
// flag. This guards a localhost dashboard that can display stored secrets — it
// is a basic ID/PW gate, not an identity system.
export interface SessionData {
  authenticated?: boolean;
  username?: string;
}

const secret =
  process.env.SESSION_SECRET ?? "dev-insecure-session-secret-change-me-min-32-characters";

export const sessionOptions: SessionOptions = {
  password: secret,
  cookieName: "promptgate_admin_session",
  cookieOptions: {
    httpOnly: true,
    // The proxy + UI run over http on localhost; secure cookies would be dropped.
    secure: false,
    sameSite: "lax",
    path: "/",
    maxAge: 60 * 60 * 8, // 8 hours
  },
};
