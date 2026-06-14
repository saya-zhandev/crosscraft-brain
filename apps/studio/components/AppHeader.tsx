'use client';
import Link from 'next/link';
import { usePathname } from 'next/navigation';
import * as Icons from 'lucide-react';
import { brand } from '@/lib/brand';
import { cn } from '@/lib/utils';

const NAV = [
  { href: '/', label: 'Workflows' },
  { href: '/credentials', label: 'Credentials' },
];

export function AppHeader() {
  const pathname = usePathname();
  const LogoIcon = (Icons as Record<string, unknown>)[brand.logoIcon] as
    | React.ComponentType<{ className?: string }>
    | undefined;

  return (
    <header className="sticky top-0 z-20 border-b border-border bg-bg/80 backdrop-blur">
      <div className="mx-auto flex max-w-6xl items-center gap-6 px-6 py-3">
        <Link href="/" className="flex items-center gap-2">
          <span className="grid size-7 place-items-center rounded-lg bg-accent/15 text-accent-2">
            {LogoIcon && <LogoIcon className="size-4" />}
          </span>
          <span className="text-[15px] font-semibold">{brand.name}</span>
          <span className="hidden text-sm text-muted sm:inline">{brand.product}</span>
        </Link>
        <nav className="flex items-center gap-1">
          {NAV.map((n) => {
            const active = n.href === '/' ? pathname === '/' : pathname.startsWith(n.href);
            return (
              <Link
                key={n.href}
                href={n.href}
                className={cn(
                  'rounded-md px-3 py-1.5 text-sm font-medium transition-colors',
                  active ? 'bg-panel-2 text-text' : 'text-muted hover:bg-panel-2 hover:text-text',
                )}
              >
                {n.label}
              </Link>
            );
          })}
        </nav>
      </div>
    </header>
  );
}
