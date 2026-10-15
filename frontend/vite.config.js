import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";
import {readFileSync, existsSync} from 'node:fs';
import {fileURLToPath} from 'node:url';
import {resolve, dirname} from 'node:path';

// Render the same source fragments as common/web/page.py; never fork device UI.
const root = fileURLToPath(new URL('../../', import.meta.url));
const products = {tc1:'TC1/http_server/web/index.html',a1:'A1/web/index.html',m1:'M1/web/index.html'};
function devicePages() {
 return {name:'device-pages', resolveId(id){if(id==='virtual:device-pages')return '\0device-pages';},
 load(id){if(id!=='\0device-pages')return;
 const pages=Object.fromEntries(Object.entries(products).map(([model,path])=>{
  const file=resolve(root,path);this.addWatchFile(file);
  let html=readFileSync(file,'utf8').replace(/\/\* @include ([\w.]+) \*\//g,(_,name)=>{
   const shared=resolve(root,'common/web',name);this.addWatchFile(existsSync(shared)?shared:resolve(dirname(file),name));
   const content=readFileSync(existsSync(shared)?shared:resolve(dirname(file),name),'utf8');
   if(name==='page.js'){const runtime=resolve(root,'platform/frontend/src/deviceHomeRuntime.js');this.addWatchFile(runtime);return readFileSync(runtime,'utf8')+'\n'+content;}
   return content;
  });
  this.addWatchFile(resolve(root,'platform/frontend/src/deviceBridge.js'));
  const bridge=readFileSync(resolve(root,'platform/frontend/src/deviceBridge.js'),'utf8');
  const appearance=readFileSync(resolve(root,'common/web/appearance.js'),'utf8');
  html=html.replace('</head>','<script>'+appearance+'</script></head>');
  html=html.replace('<script>async function command(', '<script>'+bridge+'</script><script>async function command(');
  return [model,html];
 }));
 return 'export default '+JSON.stringify(pages);
 }};
}

export default defineConfig({
  plugins: [vue(), devicePages()],
  server: {
    port: 3000,
    proxy: {
      "/mqtt": {
        target: process.env.MQTT_TARGET || "ws://localhost:9001",
        ws: true,
      },
      "/api": {
        target: process.env.API_TARGET || "http://localhost:8080",
        changeOrigin: false,
      },
    },
  },
});
