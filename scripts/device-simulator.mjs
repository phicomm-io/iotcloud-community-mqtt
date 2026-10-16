// Compatibility command; the simulator and device UI live in the frontend release.
import {execFileSync} from 'node:child_process';
import {fileURLToPath, pathToFileURL} from 'node:url';
import {resolve, dirname} from 'node:path';
const root=resolve(dirname(fileURLToPath(import.meta.url)),'..');
const bundle=execFileSync('python3',[resolve(root,'scripts/frontend_release.py')],{encoding:'utf8'}).trim();
await import(pathToFileURL(resolve(bundle,'source/scripts/device-simulator.mjs')).href);
