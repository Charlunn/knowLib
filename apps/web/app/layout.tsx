import type { Metadata, Viewport } from 'next';
import './globals.css';
import { Nav } from '@/components/nav';
import { Toaster } from '@/components/ui/toaster';

export const metadata: Metadata = {
  title: 'knowLib',
  description: 'Self-hosted AI-augmented Obsidian knowledge base',
  applicationName: 'knowLib',
  manifest: '/manifest.json',
  icons: {
    icon: '/favicon.svg',
    apple: '/apple-touch-icon.png',
  },
  appleWebApp: {
    capable: true,
    title: 'knowLib',
    statusBarStyle: 'default',
  },
};

export const viewport: Viewport = {
  themeColor: '#0f172a',
  width: 'device-width',
  initialScale: 1,
  maximumScale: 1,
  userScalable: false,
};

// Use the system font stack via next/font's local font isn't necessary —
// we declare the system stack directly in globals.css (font-feature-settings)
// so we don't ship Google Fonts at build time.
export default function RootLayout({ children }: { children: React.ReactNode }): JSX.Element {
  return (
    <html lang="zh-CN" suppressHydrationWarning>
      <body className="min-h-screen bg-background font-sans antialiased" style={{ fontFamily: 'system-ui, -apple-system, "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif' }}>
        <Nav />
        <main className="min-h-[calc(100vh-3rem)]">{children}</main>
        <Toaster />
      </body>
    </html>
  );
}
