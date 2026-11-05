import type { NextConfig } from "next";

const config: NextConfig = {
  output: "standalone",
  poweredByHeader: false,
  cacheMaxMemorySize: 8 * 1024 * 1024,
  // The Windows service can read its release, but only data directories are writable.
  experimental: { isrFlushToDisk: false },
};
export default config;
