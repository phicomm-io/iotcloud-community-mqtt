import {test} from 'node:test';
import assert from 'node:assert/strict';
import {JSDOM} from 'jsdom';
import fs from 'node:fs';
import vm from 'node:vm';
const flush=()=>new Promise(r=>setImmediate(r));
test('platform homepage retains usable last state on transient read failures and recovers',async()=>{
 const dom=new JSDOM('<div id="homePage"><button>control</button></div>',{runScripts:'outside-only',url:'http://host.local'}),w=dom.window;
 let poll,fail=false,now=1000,calls=0;
 w.ResizeObserver=class{observe(){}};w.UI={acceptStatus:s=>s};w.I18N={set(){},apply(){}};w.setTimeout=fn=>{poll=fn;};w.Date.now=()=>now;
 w.command=async()=>{calls++;if(fail)throw Error('timeout');return {socket_states:[1]};};
 vm.runInContext(fs.readFileSync(new URL('./deviceHomeRuntime.js',import.meta.url),'utf8'),dom.getInternalVMContext());
 w.UI.start({onStatus(){}});await flush();fail=true;
 for(let i=0;i<2;i++){now+=10000;await poll();await flush();assert.equal(w.document.querySelector('p').hidden,true);assert.equal(w.document.querySelector('button').disabled,false);}
 now+=15000;await poll();await flush();assert.equal(w.document.querySelector('p').hidden,false);assert.equal(w.document.querySelector('button').disabled,true);
 fail=false;await poll();await flush();assert.equal(w.document.querySelector('p').hidden,true);assert.equal(w.document.querySelector('button').disabled,false);assert.equal(calls,5);dom.window.close();
});
