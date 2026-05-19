'use client';

import * as React from 'react';
import { useRouter, useSearchParams } from 'next/navigation';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { TotpInput } from '@/components/totp-input';
import { Button } from '@/components/ui/button';
import { api, ApiError } from '@/lib/api';

function LoginForm(): JSX.Element {
  const [code, setCode] = React.useState('');
  const [busy, setBusy] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);
  const router = useRouter();
  const params = useSearchParams();
  const next = params.get('next') ?? '/capture';

  const submit = React.useCallback(
    async (totp: string) => {
      setBusy(true);
      setError(null);
      try {
        await api.login(totp);
        router.replace(next);
        router.refresh();
      } catch (e) {
        if (e instanceof ApiError) {
          if (e.status === 401) setError('验证码错误,请重试');
          else if (e.status === 429) setError('请求过于频繁,稍后再试');
          else setError('登录失败');
        } else {
          setError('网络错误');
        }
        setCode('');
        setBusy(false);
      }
    },
    [router, next],
  );

  return (
    <Card className="w-full max-w-md">
      <CardHeader className="text-center">
        <CardTitle>knowLib</CardTitle>
        <CardDescription>请输入 Authenticator 中的 6 位动态码</CardDescription>
      </CardHeader>
      <CardContent className="space-y-6">
        <TotpInput value={code} onChange={setCode} onComplete={submit} disabled={busy} />
        {error ? (
          <p role="alert" className="text-center text-sm text-destructive">
            {error}
          </p>
        ) : null}
        <Button
          className="w-full"
          disabled={busy || code.length !== 6}
          onClick={() => submit(code)}
        >
          {busy ? '验证中…' : '登录'}
        </Button>
      </CardContent>
    </Card>
  );
}

export default function LoginPage(): JSX.Element {
  return (
    <div className="container flex min-h-[calc(100vh-3rem)] items-center justify-center">
      {/* Suspense boundary required because LoginForm calls useSearchParams() */}
      <React.Suspense fallback={<div className="text-sm text-muted-foreground">加载中…</div>}>
        <LoginForm />
      </React.Suspense>
    </div>
  );
}
