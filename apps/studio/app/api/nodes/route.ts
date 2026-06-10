import { NextResponse } from 'next/server';
import { registry } from '@/lib/registry';

// Serializable node descriptors (no execute()) for the canvas palette + config forms.
export async function GET() {
  return NextResponse.json(registry().descriptors());
}
