'use client';

import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';

export function SkillSection(): JSX.Element {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Skill 包</CardTitle>
        <CardDescription>
          下载注入了你 server URL 和 token 的个人版 skill 包,挂到 Claude Desktop / Cursor / Cherry Studio 等。
        </CardDescription>
      </CardHeader>
      <CardContent>
        <Button asChild>
          <a href="/api/skill-bundle.zip" download>
            下载我的 skill 包(zip)
          </a>
        </Button>
      </CardContent>
    </Card>
  );
}
