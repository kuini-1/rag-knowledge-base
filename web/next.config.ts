import type { NextConfig } from "next";

// The browser only talks to this app; /api is forwarded to the Go server, so
// there is one origin and no CORS to configure.
const apiURL = process.env.RAG_API_URL ?? "http://127.0.0.1:8090";

const nextConfig: NextConfig = {
  output: "standalone",
  cacheComponents: true,
  partialPrefetching: true,
  turbopack: {
    rules: {
      "*.css": {
        loaders: ["@tailwindcss/turbopack"],
        as: "*.css",
      },
    },
  },
  async rewrites() {
    return [{ source: "/api/:path*", destination: `${apiURL}/api/:path*` }];
  },
};

export default nextConfig;
