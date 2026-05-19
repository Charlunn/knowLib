import { redirect } from 'next/navigation';
import { isAuthed } from '@/lib/auth';

export default function HomePage(): never {
  redirect(isAuthed() ? '/capture' : '/login');
}
