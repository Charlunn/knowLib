'use client';

import * as React from 'react';
import { Toast, ToastClose, ToastDescription, ToastProvider, ToastTitle, ToastViewport, type ToastProps } from './toast';

interface ToastItem {
  id: number;
  title?: string;
  description?: string;
  variant?: ToastProps['variant'];
}

let _id = 0;
const listeners = new Set<(t: ToastItem) => void>();

export function toast(t: Omit<ToastItem, 'id'>): void {
  const item = { id: ++_id, ...t };
  listeners.forEach((l) => l(item));
}

export function Toaster(): JSX.Element {
  const [items, setItems] = React.useState<ToastItem[]>([]);
  React.useEffect(() => {
    const cb = (t: ToastItem) => {
      setItems((prev) => [...prev, t]);
      window.setTimeout(() => {
        setItems((prev) => prev.filter((p) => p.id !== t.id));
      }, 4000);
    };
    listeners.add(cb);
    return () => {
      listeners.delete(cb);
    };
  }, []);
  return (
    <ToastProvider>
      {items.map((t) => (
        <Toast key={t.id} variant={t.variant}>
          <div className="grid gap-1">
            {t.title ? <ToastTitle>{t.title}</ToastTitle> : null}
            {t.description ? <ToastDescription>{t.description}</ToastDescription> : null}
          </div>
          <ToastClose />
        </Toast>
      ))}
      <ToastViewport />
    </ToastProvider>
  );
}
