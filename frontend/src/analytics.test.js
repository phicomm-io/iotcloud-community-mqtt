import {test} from 'node:test';
import assert from 'node:assert/strict';
import {track} from './analytics.js';
test('analytics strips source URL details, reuses visitor and visit, and excludes unknown routes',()=>{
 const keys=['localStorage','document','location','innerWidth','fetch'];const old=new Map(keys.map(k=>[k,Object.getOwnPropertyDescriptor(globalThis,k)]));const saved=new Map(),sent=[];
 try{
  Object.assign(globalThis,{localStorage:{getItem:k=>saved.get(k)||null,setItem:(k,v)=>saved.set(k,v)},document:{referrer:'https://search.example/path?password=secret#private'},location:{pathname:'/login'},innerWidth:390,fetch:(url,options)=>{sent.push({url,data:JSON.parse(options.body)});return Promise.resolve();}});
  track('pageview');track('device_view','/','tc1');track('pageview','/auth/verify?token=secret');
  assert.equal(sent.length,2);assert.equal(sent[0].data.referrer,'https://search.example');assert.equal(sent[0].data.screen,'mobile');assert.equal(sent[0].data.visitor,sent[1].data.visitor);assert.equal(sent[0].data.session,sent[1].data.session);assert.equal(sent[1].data.model,'tc1');assert.equal(JSON.stringify(sent).includes('secret'),false);
  saved.set('phicomm.analytics-visit',JSON.stringify({id:sent[0].data.session,at:Date.now()-1800001}));track('pageview');assert.notEqual(sent[2].data.session,sent[0].data.session);assert.equal(sent[2].data.visitor,sent[0].data.visitor);
  globalThis.fetch=()=>Promise.reject(Error('offline'));assert.doesNotThrow(()=>track('pageview'));
 }finally{for(const k of keys){const descriptor=old.get(k);if(descriptor)Object.defineProperty(globalThis,k,descriptor);else delete globalThis[k];}}
});
