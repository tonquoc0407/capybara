import { copyFile, mkdir } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { dirname, join, resolve } from 'node:path';

const site = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const source = resolve(site, '..', 'demo');
const destination = join(site, 'public', 'assets');
const files = ['logo.png', 'pupu-workspace.png', 'web-overview.png', 'web-trace-mobile.png', 'pupu-tour.mp4'];

await mkdir(destination, { recursive: true });
await Promise.all(files.map(file => copyFile(join(source, file), join(destination, file))));
await copyFile(join(site, 'REACT_BITS_LICENSE.md'), join(destination, 'react-bits-license.txt'));
