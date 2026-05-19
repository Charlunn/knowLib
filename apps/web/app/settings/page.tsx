import { SettingsTabs } from './tabs';

export const dynamic = 'force-dynamic';

export default function SettingsPage(): JSX.Element {
  return (
    <div className="container max-w-3xl py-6">
      <h1 className="mb-4 text-xl font-semibold">设置</h1>
      <SettingsTabs />
    </div>
  );
}
