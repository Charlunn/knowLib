'use client';

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';

export function TotpSection(): JSX.Element {
  return (
    <Card>
      <CardHeader>
        <CardTitle>TOTP 重新绑定</CardTitle>
        <CardDescription>
          换设备 / 重置 Authenticator 时使用。Phase 2 提供 UI;现阶段请用 bootstrap.sh 重新生成。
        </CardDescription>
      </CardHeader>
      <CardContent>
        <Button disabled>Phase 2</Button>
      </CardContent>
    </Card>
  );
}
