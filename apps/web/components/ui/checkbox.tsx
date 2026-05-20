'use client';

import * as React from 'react';
import { cn } from '@/lib/utils';

export interface CheckboxProps {
  checked?: boolean;
  onCheckedChange?: (checked: boolean) => void;
  disabled?: boolean;
  className?: string;
}

export const Checkbox = React.forwardRef<HTMLInputElement, CheckboxProps>(
  ({ checked, onCheckedChange, disabled, className }, ref) => {
    return (
      <input
        ref={ref}
        type="checkbox"
        checked={!!checked}
        disabled={disabled}
        onChange={(e) => onCheckedChange?.(e.target.checked)}
        className={cn(
          'h-4 w-4 cursor-pointer rounded border border-input bg-background',
          'accent-primary',
          'disabled:cursor-not-allowed disabled:opacity-50',
          className,
        )}
      />
    );
  },
);
Checkbox.displayName = 'Checkbox';
