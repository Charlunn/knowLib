// Static fallback used by the service worker when navigation requests fail offline.
export const dynamic = 'force-static';

export default function OfflinePage(): JSX.Element {
  return (
    <div className="container max-w-lg py-16 text-center">
      <h1 className="text-2xl font-semibold">离线</h1>
      <p className="mt-3 text-muted-foreground">
        当前没有网络。打开 /capture 仍可写入,等连上后会自动同步。
      </p>
    </div>
  );
}
