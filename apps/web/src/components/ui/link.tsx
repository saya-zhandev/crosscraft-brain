import { forwardRef } from 'react';
import { Link as RouterLink, type LinkProps } from 'react-router-dom';

/**
 * Compat Link: accepts `href` (matching the previous next/link usage across the
 * canvas) and maps it to react-router's `to`. Forwards the ref so it works inside
 * Radix `asChild` slots (e.g. <Button asChild><Link .../></Button>).
 */
export const Link = forwardRef<HTMLAnchorElement, Omit<LinkProps, 'to'> & { href: string }>(
  function Link({ href, ...rest }, ref) {
    return <RouterLink ref={ref} to={href} {...rest} />;
  },
);
