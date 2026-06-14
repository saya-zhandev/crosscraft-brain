/**
 * Branding hub. A fork overrides product identity here (and the color tokens in
 * globals.css) — chrome reads from this object so the name/tagline/logo live in one place.
 */
export const brand = {
  name: 'crosscraft',
  product: 'workflow studio',
  tagline: 'Visual editor · integrations · AI · transparent monitoring.',
  /** lucide-react icon name used as the logo mark in chrome. */
  logoIcon: 'Workflow',
} as const;

export type Brand = typeof brand;
