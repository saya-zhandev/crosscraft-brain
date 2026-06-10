/** AES-256-GCM credential encryption. Key from CREDENTIALS_SECRET (32-byte hex). */
import crypto from 'node:crypto';

function key(): Buffer {
  const hex = process.env.CREDENTIALS_SECRET;
  if (!hex || hex.length !== 64) {
    throw new Error('CREDENTIALS_SECRET must be a 64-char hex string (32 bytes)');
  }
  return Buffer.from(hex, 'hex');
}

export function encrypt(plain: string): string {
  const iv = crypto.randomBytes(12);
  const cipher = crypto.createCipheriv('aes-256-gcm', key(), iv);
  const enc = Buffer.concat([cipher.update(plain, 'utf8'), cipher.final()]);
  const tag = cipher.getAuthTag();
  return [iv.toString('hex'), tag.toString('hex'), enc.toString('hex')].join(':');
}

export function decrypt(blob: string): string {
  const [ivH, tagH, encH] = blob.split(':');
  if (!ivH || !tagH || !encH) throw new Error('Malformed encrypted blob');
  const decipher = crypto.createDecipheriv('aes-256-gcm', key(), Buffer.from(ivH, 'hex'));
  decipher.setAuthTag(Buffer.from(tagH, 'hex'));
  return Buffer.concat([decipher.update(Buffer.from(encH, 'hex')), decipher.final()]).toString('utf8');
}
