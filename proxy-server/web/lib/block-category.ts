// Maps a free-form block reason into a short, punchy "genre" label for the
// FF14-style floating popup over PromptGate. Covers the deterministic rule reasons
// ("secret detected (aws_access_key)" etc.), common LFM phrasings, and a few
// Japanese categories (the jp_confidential_extraction profile emits Japanese).
// Falls back to BLOCKED so an unknown reason still pops something readable.
export function blockCategory(reason?: string): string {
  const r = (reason || "").toLowerCase();
  if (!r) return "BLOCKED";

  // Japanese confidential categories
  if (r.includes("個人情報") || r.includes("個人")) return "個人情報";
  if (r.includes("社外秘") || r.includes("機密")) return "機密情報";
  if (r.includes("住所")) return "住所";
  if (r.includes("氏名") || r.includes("人名") || r.includes("名前")) return "氏名";
  if (r.includes("電話")) return "電話番号";
  if (r.includes("会社") || r.includes("企業")) return "企業情報";

  // Secret/credential rules
  if (r.includes("aws")) return "AWS KEY";
  if (r.includes("anthropic")) return "ANTHROPIC KEY";
  if (r.includes("openai")) return "OPENAI KEY";
  if (r.includes("github")) return "GITHUB TOKEN";
  if (r.includes("slack")) return "SLACK TOKEN";
  if (r.includes("gcp") || r.includes("google")) return "GCP KEY";
  if (r.includes("jwt")) return "JWT";
  if (r.includes("private key") || r.includes("private_key")) return "PRIVATE KEY";
  if (
    r.includes("database") ||
    r.includes("credential") ||
    r.includes("postgres") ||
    r.includes("password")
  )
    return "DB CREDENTIAL";

  // PII / network
  if (r.includes("email") || r.includes("mail")) return "EMAIL";
  if (r.includes("phone")) return "PHONE";
  if (r.includes("hostname") || r.includes("private ip") || r.includes("internal host"))
    return "INTERNAL HOST";

  // Operational
  if (r.includes("classifier")) return "FAIL-CLOSED";
  if (r.includes("secret")) return "SECRET";

  return "BLOCKED";
}
