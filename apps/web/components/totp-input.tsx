'use client';

import * as React from 'react';
import { cn } from '@/lib/utils';

interface TotpInputProps {
  value: string;
  onChange: (next: string) => void;
  onComplete?: (code: string) => void;
  disabled?: boolean;
  autoFocus?: boolean;
}

const LEN = 6;

// 6 cells, one digit each. Auto-advances on input, backspaces step back,
// pasting a 6-digit code distributes across cells.
export function TotpInput({ value, onChange, onComplete, disabled, autoFocus = true }: TotpInputProps): JSX.Element {
  const refs = React.useRef<(HTMLInputElement | null)[]>([]);
  const digits = React.useMemo(() => {
    const arr = value.split('').slice(0, LEN);
    while (arr.length < LEN) arr.push('');
    return arr;
  }, [value]);

  React.useEffect(() => {
    if (autoFocus) refs.current[0]?.focus();
  }, [autoFocus]);

  const setAt = (idx: number, ch: string) => {
    const next = digits.slice();
    next[idx] = ch;
    const out = next.join('').replace(/[^0-9]/g, '');
    onChange(out);
    if (out.length === LEN && onComplete) onComplete(out);
  };

  const handleChange = (idx: number) => (e: React.ChangeEvent<HTMLInputElement>) => {
    const raw = e.target.value.replace(/[^0-9]/g, '');
    if (!raw) {
      setAt(idx, '');
      return;
    }
    if (raw.length === 1) {
      setAt(idx, raw);
      if (idx < LEN - 1) refs.current[idx + 1]?.focus();
    } else {
      // Pasting multiple digits into one cell — distribute.
      const next = digits.slice();
      for (let i = 0; i < raw.length && idx + i < LEN; i++) {
        next[idx + i] = raw.charAt(i);
      }
      const out = next.join('');
      onChange(out);
      const targetIdx = Math.min(idx + raw.length, LEN - 1);
      refs.current[targetIdx]?.focus();
      if (out.length === LEN && onComplete) onComplete(out);
    }
  };

  const handleKeyDown = (idx: number) => (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Backspace' && !digits[idx] && idx > 0) {
      refs.current[idx - 1]?.focus();
    } else if (e.key === 'ArrowLeft' && idx > 0) {
      refs.current[idx - 1]?.focus();
    } else if (e.key === 'ArrowRight' && idx < LEN - 1) {
      refs.current[idx + 1]?.focus();
    }
  };

  const handlePaste = (e: React.ClipboardEvent<HTMLInputElement>) => {
    const text = e.clipboardData.getData('text').replace(/[^0-9]/g, '').slice(0, LEN);
    if (!text) return;
    e.preventDefault();
    onChange(text);
    if (text.length === LEN && onComplete) onComplete(text);
    refs.current[Math.min(text.length, LEN - 1)]?.focus();
  };

  return (
    <div className="flex justify-center gap-2" onPaste={handlePaste}>
      {digits.map((d, i) => (
        <input
          key={i}
          ref={(el) => {
            refs.current[i] = el;
          }}
          type="text"
          inputMode="numeric"
          autoComplete="one-time-code"
          maxLength={1}
          disabled={disabled}
          value={d}
          onChange={handleChange(i)}
          onKeyDown={handleKeyDown(i)}
          aria-label={`TOTP digit ${i + 1}`}
          className={cn(
            'h-14 w-12 rounded-md border border-input bg-background text-center text-2xl font-mono shadow-sm',
            'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
          )}
        />
      ))}
    </div>
  );
}
