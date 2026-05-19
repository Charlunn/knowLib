/* eslint-disable */
// Generates a minimal solid-color square PNG with a "K" glyph.
// Pure Node, no deps — uses zlib for the PNG IDAT chunk.
// Run: `node scripts/gen-icons.js`. Idempotent.

const fs = require('fs');
const path = require('path');
const zlib = require('zlib');

const OUT = path.join(__dirname, '..', 'public', 'icons');
fs.mkdirSync(OUT, { recursive: true });
const APPLE = path.join(__dirname, '..', 'public', 'apple-touch-icon.png');

// Background slate-900 (#0f172a), foreground slate-50 (#f8fafc)
const BG = [0x0f, 0x17, 0x2a, 0xff];
const FG = [0xf8, 0xfa, 0xfc, 0xff];

// Letter K rendered as a 7x7 bitmap (1 = foreground), scaled to fit.
const GLYPH = [
  '1.....1',
  '1....1.',
  '1...1..',
  '111....',
  '1...1..',
  '1....1.',
  '1.....1',
];

function makePng(size) {
  const w = size;
  const h = size;
  const buf = Buffer.alloc(w * h * 4);
  for (let i = 0; i < w * h; i++) {
    buf[i * 4 + 0] = BG[0];
    buf[i * 4 + 1] = BG[1];
    buf[i * 4 + 2] = BG[2];
    buf[i * 4 + 3] = BG[3];
  }
  // Render glyph centered, occupying ~50% of the canvas.
  const cell = Math.floor(size * 0.5 / 7);
  const gw = cell * 7;
  const gh = cell * 7;
  const ox = Math.floor((size - gw) / 2);
  const oy = Math.floor((size - gh) / 2);
  for (let gy = 0; gy < 7; gy++) {
    for (let gx = 0; gx < 7; gx++) {
      if (GLYPH[gy][gx] !== '1') continue;
      for (let py = 0; py < cell; py++) {
        for (let px = 0; px < cell; px++) {
          const x = ox + gx * cell + px;
          const y = oy + gy * cell + py;
          if (x < 0 || y < 0 || x >= w || y >= h) continue;
          const i = (y * w + x) * 4;
          buf[i + 0] = FG[0];
          buf[i + 1] = FG[1];
          buf[i + 2] = FG[2];
          buf[i + 3] = FG[3];
        }
      }
    }
  }

  // Build raw scanlines with filter byte 0.
  const stride = w * 4;
  const raw = Buffer.alloc((stride + 1) * h);
  for (let y = 0; y < h; y++) {
    raw[y * (stride + 1)] = 0;
    buf.copy(raw, y * (stride + 1) + 1, y * stride, y * stride + stride);
  }
  const idat = zlib.deflateSync(raw);

  const sig = Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]);

  function chunk(type, data) {
    const len = Buffer.alloc(4);
    len.writeUInt32BE(data.length, 0);
    const t = Buffer.from(type, 'ascii');
    const crc = Buffer.alloc(4);
    crc.writeInt32BE(crcOf(Buffer.concat([t, data])), 0);
    return Buffer.concat([len, t, data, crc]);
  }

  // CRC32
  const table = (() => {
    const t = new Uint32Array(256);
    for (let n = 0; n < 256; n++) {
      let c = n;
      for (let k = 0; k < 8; k++) c = (c & 1) ? (0xedb88320 ^ (c >>> 1)) : (c >>> 1);
      t[n] = c >>> 0;
    }
    return t;
  })();
  function crcOf(b) {
    let c = 0xffffffff;
    for (let i = 0; i < b.length; i++) c = table[(c ^ b[i]) & 0xff] ^ (c >>> 8);
    return (c ^ 0xffffffff) | 0;
  }

  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(w, 0);
  ihdr.writeUInt32BE(h, 4);
  ihdr[8] = 8;     // bit depth
  ihdr[9] = 6;     // color type RGBA
  ihdr[10] = 0;    // compression
  ihdr[11] = 0;    // filter
  ihdr[12] = 0;    // interlace

  return Buffer.concat([
    sig,
    chunk('IHDR', ihdr),
    chunk('IDAT', idat),
    chunk('IEND', Buffer.alloc(0)),
  ]);
}

function write(file, size) {
  fs.writeFileSync(file, makePng(size));
  console.log('wrote', file, size + 'x' + size);
}

write(path.join(OUT, 'icon-192.png'), 192);
write(path.join(OUT, 'icon-512.png'), 512);
write(APPLE, 180);
