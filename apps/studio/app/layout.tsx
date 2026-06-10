import type { Metadata } from 'next';
import './globals.css';

export const metadata: Metadata = {
  title: 'crosscraft-brain studio',
  description: 'Forkable workflow-automation platform: editor, integrations, AI, monitoring.',
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
