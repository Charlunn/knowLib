// Server-side wrapper around the upstream knowlib-api.
// API_INTERNAL_URL is the in-cluster URL (e.g. http://api:8080 inside docker-compose).

export function backendUrl(path: string): string {
  const base = process.env.API_INTERNAL_URL ?? 'http://api:8080';
  if (path.startsWith('/')) return `${base}${path}`;
  return `${base}/${path}`;
}
