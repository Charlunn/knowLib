/** @type {import('next').NextConfig} */
const withPWA = require('next-pwa')({
  dest: 'public',
  register: true,
  skipWaiting: true,
  // Service worker should not cache /api/* — those calls are dynamic and auth-bound.
  // We let next-pwa cache static assets and pages by default; explicitly bypass /api.
  buildExcludes: [/middleware-manifest\.json$/],
  runtimeCaching: [
    {
      // Never cache API responses; always go to network.
      urlPattern: /\/api\/.*$/,
      handler: 'NetworkOnly',
    },
    {
      urlPattern: /\.(?:png|jpg|jpeg|svg|gif|webp|ico)$/i,
      handler: 'CacheFirst',
      options: {
        cacheName: 'static-images',
        expiration: { maxEntries: 64, maxAgeSeconds: 60 * 60 * 24 * 30 },
      },
    },
    {
      urlPattern: /\.(?:js|css)$/i,
      handler: 'StaleWhileRevalidate',
      options: { cacheName: 'static-assets' },
    },
    {
      // Pages — fall back to whatever is in the cache when network fails.
      // (next-pwa 5.6's `fallbacks` option breaks on Next 14.2's webpack entry,
      // so we rely on NetworkFirst + a manual /offline route instead.)
      urlPattern: ({ request }) => request.mode === 'navigate',
      handler: 'NetworkFirst',
      options: {
        cacheName: 'pages',
        networkTimeoutSeconds: 4,
      },
    },
  ],
  disable: process.env.NODE_ENV === 'development',
});

const nextConfig = {
  output: 'standalone',
  reactStrictMode: true,
  poweredByHeader: false,
  experimental: {
    // Server actions are off; we use route handlers only.
  },
};

module.exports = withPWA(nextConfig);
