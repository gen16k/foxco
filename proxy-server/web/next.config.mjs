/** @type {import('next').NextConfig} */
const nextConfig = {
  reactStrictMode: true,
  // Allow a separate build/run output dir (default ".next"). The demo mock server
  // (start-mock.ps1) sets NEXT_DIST_DIR=".next-mock" so it can run alongside the
  // real instance on another port without clobbering its ".next".
  distDir: process.env.NEXT_DIST_DIR || ".next",
};

export default nextConfig;
