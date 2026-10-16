/* Shared WebUI framework: header, routing, feedback, reusable controls and the
 * settings pages every product has. Loaded after service.js (protocol) and
 * i18n.js (strings); each product page calls UI.start() with its own layout.
 * All requests go through command() in service.js; nothing here talks to a URL
 * directly except the OTA upload. */
(()=>{
'use strict';
const {t}=I18N;
const $=id=>document.getElementById(id);
function h(tag,props,...kids){
 const el=document.createElement(tag);
 for(const [k,v] of Object.entries(props||{})){
  if(v==null||v===false)continue;
  if(k==='class')el.className=v;
  else if(k==='i18n'){el.dataset.i18n=v;el.textContent=t(v);}
  else if(k.startsWith('on')&&typeof v==='function')el.addEventListener(k.slice(2),v);
  else if(k in el&&!k.includes('-'))el[k]=v;
  else el.setAttribute(k,v===true?'':v);
 }
 for(const c of kids.flat(Infinity)){if(c==null||c===false)continue;el.append(c.nodeType?c:document.createTextNode(String(c)));}
 return el;
}
const ICONS={
 power:'<path d="M12 2v10"/><path d="M18.4 6.6a9 9 0 1 1-12.8 0"/>',
 fan:'<path d="M10.8 9.1C9 6.4 9.6 3 12.6 2.2c3-.8 4.5 2.4 2.6 6.4M14.9 10.8c2.7-1.8 6.1-1.2 6.9 1.8.8 3-2.4 4.5-6.4 2.6M13.2 14.9c1.8 2.7 1.2 6.1-1.8 6.9-3 .8-4.5-2.4-2.6-6.4M9.1 13.2C6.4 15 3 14.4 2.2 11.4c-.8-3 2.4-4.5 6.4-2.6"/><circle cx="12" cy="12" r="2"/>',
 plug:'<path d="M12 22v-5M9 8V2M15 8V2M18 8v5a6 6 0 0 1-12 0V8z"/>',
 air:'<path d="M17.7 7.7a2.5 2.5 0 1 1 1.8 4.3H2M9.6 4.6A2 2 0 1 1 11 8H2M12.6 19.4A2 2 0 1 0 14 16H2"/>',
 lock:'<rect x="4" y="11" width="16" height="10" rx="2"/><path d="M8 11V7a4 4 0 0 1 8 0v4"/>',
 wifi:'<path d="M5 12.6a10 10 0 0 1 14 0M8.5 16.1a5 5 0 0 1 7 0M2 8.8a15 15 0 0 1 20 0M12 20h.01"/>',
 home:'<path d="M3 10.5 12 3l9 7.5V20a1 1 0 0 1-1 1h-5v-6H9v6H4a1 1 0 0 1-1-1z"/>',
 sliders:'<path d="M4 21v-7M4 10V3M12 21v-9M12 8V3M20 21v-5M20 12V3M1 14h6M9 8h6M17 16h6"/>',
 edit:'<path d="M12 20h9M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4z"/>',
 trash:'<path d="M3 6h18M8 6V4h8v2M19 6l-1 14H6L5 6"/>',
 plus:'<path d="M12 5v14M5 12h14"/>',
 link:'<path d="M10 13a5 5 0 0 0 7.5.5l3-3a5 5 0 0 0-7-7l-1.7 1.7"/><path d="M14 11a5 5 0 0 0-7.5-.5l-3 3a5 5 0 0 0 7 7l1.7-1.7"/>',
 copy:'<rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/>',
 info:'<circle cx="12" cy="12" r="10"/><path d="M12 16v-4M12 8h.01"/>',
 alert:'<path d="M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0zM12 9v4M12 17h.01"/>',
 moon:'<path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z"/>',
 clock:'<circle cx="12" cy="12" r="10"/><path d="M12 6v6l4 2"/>',
 auto:'<path d="M21 12a9 9 0 0 1-15 6.7L3 16M3 12a9 9 0 0 1 15-6.7L21 8M21 3v5h-5M3 21v-5h5"/>'
};
function icon(name,cls='i'){const s=document.createElementNS('http://www.w3.org/2000/svg','svg');s.setAttribute('viewBox','0 0 24 24');s.setAttribute('class',cls);s.setAttribute('aria-hidden','true');s.innerHTML=ICONS[name]||'';return s;}

/* ---------- Feedback ---------- */
function toast(text,bad=false){
 let box=$('toasts');if(!box){box=h('div',{id:'toasts',class:'toasts',role:'status','aria-live':'polite'});document.body.append(box);}
 const el=h('div',{class:'toast'+(bad?' bad':'')},text);box.append(el);setTimeout(()=>el.remove(),bad?6000:2600);
}
async function run(button,fn,okKey='toast.accepted'){
 if(button){button.disabled=true;button.setAttribute('aria-busy','true');}
 try{const r=await fn();if(okKey)toast(t(okKey));return r;}
 catch(e){toast(e.message||String(e),true);}
 finally{if(button){button.disabled=false;button.removeAttribute('aria-busy');}}
}
function onSubmit(form,fn,okKey){form.addEventListener('submit',e=>{e.preventDefault();run(form.querySelector('button[type=submit],button:not([type])'),fn,okKey);});}
function confirmDialog({title,body,ok,danger}){
 if(typeof document.body.append!=='function'||!document.createElement('dialog').showModal)return Promise.resolve(window.confirm(title+'\n'+body));
 return new Promise(resolve=>{
  const d=h('dialog',{},h('h2',{},title),h('p',{},body),
   h('div',{class:'form-actions'},
    h('button',{class:'btn '+(danger?'danger':'primary'),type:'button',onclick:()=>{d.close();resolve(true);}},ok),
    h('button',{class:'btn ghost',type:'button',onclick:()=>d.close()},t('common.cancel'))));
  d.addEventListener('close',()=>{d.remove();resolve(false);});
  document.body.append(d);d.showModal();
 });
}

/* ---------- Controls ---------- */
function switchRow(id,titleKey,descKey){
 return h('label',{class:'switch'},h('span',{class:'text'},h('b',{i18n:titleKey}),descKey&&h('span',{i18n:descKey})),h('input',{type:'checkbox',role:'switch',id}));
}
function seg(options,onPick,{full=false,aria}={}){
 const label=k=>typeof k==='function'?k():I18N.has(k)?t(k):k;
 const box=h('div',{class:'seg'+(full?' full':''),role:'group','aria-label':aria||null});
 for(const [value,key] of options)box.append(h('button',{type:'button','data-value':String(value),'aria-pressed':'false',onclick:()=>{box.set(value);onPick(value);}},label(key)));
 box.set=v=>{for(const b of box.children)b.setAttribute('aria-pressed',String(b.dataset.value===String(v)));box.value=v;};
 box.relabel=()=>options.forEach(([,key],i)=>{box.children[i].textContent=label(key);});
 I18N.onChange(box.relabel);
 return box;
}
const DAY_BITS=[1,2,4,8,16,32,64];
function daysPicker(name,mask=127){
 const box=h('div',{class:'days',role:'group'});
 DAY_BITS.forEach((bit,i)=>box.append(h('label',{},h('input',{type:'checkbox',name,value:bit,checked:!!(mask&bit)}),h('span',{'data-i18n':'day.short.'+i},t('day.short.'+i)))));
 box.mask=()=>[...box.querySelectorAll('input')].reduce((m,i)=>m|(i.checked?Number(i.value):0),0);
 box.setMask=m=>box.querySelectorAll('input').forEach(i=>{i.checked=!!(m&Number(i.value));});
 return box;
}
function presetDays(picker){
 return h('div',{class:'row small'},[[127,'day.every'],[31,'day.weekdays'],[96,'day.weekends']].map(([m,k])=>h('button',{type:'button',class:'btn ghost',style:'min-height:28px;padding:2px 8px',onclick:()=>picker.setMask(m)},h('span',{i18n:k}))));
}
function daysText(mask){
 if(!mask)return t('day.once');if(mask===127)return t('day.every');if(mask===31)return t('day.weekdays');if(mask===96)return t('day.weekends');
 return DAY_BITS.map((b,i)=>mask&b?t('day.short.'+i):null).filter(Boolean).join(I18N.lang==='zh-CN'?'、':', ');
}
const hhmm=m=>String(Math.floor(m/60)%24).padStart(2,'0')+':'+String(m%60).padStart(2,'0');
const toMinutes=v=>{const [a,b]=v.split(':').map(Number);return a*60+b;};
function duration(sec){const d=Math.floor(sec/86400),hr=Math.floor(sec%86400/3600),m=Math.floor(sec%3600/60);return d?t('time.dh',{d,h:hr}):hr?t('time.hm',{h:hr,m}):t('time.m',{m});}
function summary(items){return h('dl',{class:'summary'},items.filter(Boolean).map(([k,v,dot])=>h('div',{},h('dt',{i18n:k}),h('dd',{},dot?h('span',{class:'dot '+dot}):null,v))));}
function card(titleKey,leadKey,...body){return h('section',{class:'card'},h('div',{class:'card-head'},h('div',{},h('h2',{i18n:titleKey}),leadKey&&h('p',{class:'help',i18n:leadKey}))),...body);}
function info(key){return h('details',{class:'info'},h('summary',{},icon('info'),h('span',{i18n:'common.more_info'})),h('p',{i18n:key}));}
function pageHead(titleKey,leadKey){return h('div',{},h('h2',{class:'page-title',i18n:titleKey}),h('p',{class:'page-lead',i18n:leadKey}));}
function saveBar(fn,label='common.save'){const b=h('button',{type:'button',class:'btn primary'},h('span',{i18n:label}));b.onclick=()=>run(b,fn);return h('div',{class:'form-actions'},b);}
function num(model,key,{min,max,step='any',unit,width}={}){
 const input=h('input',{type:'number',value:model[key],min,max,step,style:width?'width:'+width:null,oninput:e=>{if(e.target.value!==''&&Number.isFinite(+e.target.value))model[key]=+e.target.value;}});
 return unit?h('span',{class:'input-unit'},input,h('em',{},unit)):input;
}
function flag(model,key,titleKey,descKey){const r=switchRow(null,titleKey,descKey);const i=r.querySelector('input');i.checked=!!model[key];i.onchange=()=>{model[key]=typeof model[key]==='boolean'?i.checked:Number(i.checked);};return r;}
function slider(model,key,{min=0,max=100,fmt=v=>v+'%'}={}){
 const out=h('b',{class:'num'},fmt(model[key]));
 const r=h('input',{type:'range',min,max,value:model[key],oninput:e=>{model[key]=+e.target.value;out.textContent=fmt(model[key]);}});
 return [out,r];
}
function normalizeMac(v){const m=(v||'').replace(/[^0-9a-f]/gi,'').toLowerCase();return m.length===12?m:null;}
function prettyMac(m){return (m||'').toUpperCase().replace(/(..)(?!$)/g,'$1:');}
const clone=o=>JSON.parse(JSON.stringify(o));

/* Air quality bands: PM2.5 per GB 3095, formaldehyde limit per GB/T 18883-2022,
 * temperature and humidity comfort per the A1 light-segment defaults. */
const AIR={
 pm25:{unit:'µg/m³',max:250,bands:[[35,'excellent','ok'],[75,'good','ok'],[115,'light','warn'],[150,'moderate','warn'],[250,'heavy','bad'],[Infinity,'severe','bad']]},
 formaldehyde:{unit:'mg/m³',max:0.2,bands:[[0.08,'pass','ok'],[0.1,'slight','warn'],[Infinity,'exceed','bad']]},
 temperature:{unit:'℃',min:0,max:40,bands:[[18,'cool','warn'],[26,'comfort','ok'],[Infinity,'warm','warn']]},
 humidity:{unit:'%',max:100,bands:[[40,'dry','warn'],[60,'comfort','ok'],[Infinity,'humid','warn']]}
};
function grade(metric,v){const b=AIR[metric].bands.find(([lim])=>v<=lim);return {key:'air.'+metric+'.'+b[1],cls:b[2]};}
function airScale(metric,v){
 const m=AIR[metric],lo=m.min||0,span=m.max-lo;let prev=lo;
 const strip=h('div',{class:'scale'});
 for(const [lim,,cls] of m.bands){const end=Math.min(lim,m.max);if(end<=prev)continue;strip.append(h('i',{class:cls,style:'flex:'+((end-prev)/span*100).toFixed(2)}));prev=end;}
 strip.append(h('b',{style:'left:'+Math.max(0,Math.min(100,(v-lo)/span*100))+'%'}));
 return strip;
}
const TZ=[-720,-660,-600,-540,-480,-420,-360,-300,-240,-210,-180,-120,-60,0,60,120,180,210,240,270,300,330,345,360,390,420,480,540,570,600,630,660,720,780,840];
function tzLabel(m){const sg=m<0?'−':'+',a=Math.abs(m);return 'UTC'+sg+String(Math.floor(a/60)).padStart(2,'0')+':'+String(a%60).padStart(2,'0')+(m===480?' · '+t('tz.cn'):'');}

/* ---------- Header, navigation, banners ---------- */
const UI=window.UI={$,h,icon,toast,run,onSubmit,confirm:confirmDialog,switchRow,seg,daysPicker,presetDays,daysText,hhmm,toMinutes,duration,summary,card,info,pageHead,saveBar,num,flag,slider,normalizeMac,prettyMac,clone,AIR,grade,airScale,tzLabel,state:null,caps:null,pages:{},device:null,clockCard,maintenancePage,scanNetworks};
// Keep requested output visible until a fresh read confirms it. Polls may still
// contain the pre-write state while the device applies an accepted command.
let pendingControl=null;
UI.acceptStatus=s=>{UI.confirmed=s;UI.state=pendingControl?{...s,...pendingControl}:s;return UI.state;};
UI.control=async(action,params,patch)=>{
 if(pendingControl){UI.device.onStatus?.(UI.state);throw Error(t('control.pending'));}
 pendingControl=patch;UI.controlBusy=true;
 const render=()=>{if(UI.device.onStatus)UI.device.onStatus(UI.state);};
 UI.state={...UI.state,...patch};render();
 let sendError,last;
 const matches=s=>s&&Object.entries(patch).every(([k,v])=>Object.hasOwn(s,k)&&(Array.isArray(v)?Array.isArray(s[k])&&v.length===s[k].length&&v.every((x,i)=>!!x===!!s[k][i]):typeof v==='boolean'?!!s[k]===v:s[k]===v));
 try{
  try{await command(action,params);}catch(e){sendError=e;if(e.code&&!['timeout','offline'].includes(e.code))throw e;}
  const until=Date.now()+20000;
  do{
   last=await UI.reload();
   if(matches(last))return last;
   await new Promise(resolve=>setTimeout(resolve,750));
  }while(Date.now()<until);
  throw Error(t('control.unconfirmed')+(sendError?' · '+sendError.message:''));
 }finally{
  pendingControl=null;UI.controlBusy=false;UI.state=last||UI.confirmed||UI.state;render();
 }
};
function header(dev){
 const lang=h('select',{id:'langPick','aria-label':'Language'},
  h('option',{value:'auto','data-i18n':'pref.lang_auto'},t('pref.lang_auto')),h('option',{value:'en'},'English'),h('option',{value:'zh-CN'},'简体中文'));
 lang.value=I18N.preference;lang.onchange=()=>I18N.set(lang.value);
 const theme=h('select',{id:'themePick','aria-label':'Theme'},['system','light','dark'].map(v=>h('option',{value:v,'data-i18n':'pref.theme_'+v},t('pref.theme_'+v))));
 let saved='system';try{saved=localStorage.getItem('phicomm.appearance')||'system';}catch{}
 const apply=v=>{if(v==='system')document.documentElement.removeAttribute('data-theme');else document.documentElement.dataset.theme=v;};
 theme.value=saved;apply(saved);theme.onchange=()=>{apply(theme.value);try{localStorage.setItem('phicomm.appearance',theme.value);}catch{}};
 return h('header',{class:'topbar'},
  h('div',{class:'brand'},h('div',{class:'logo'},icon(dev.icon)),h('div',{class:'grow'},h('div',{class:'kind','data-i18n':dev.kindKey},t(dev.kindKey)),h('h1',{id:'title'},dev.model))),
  h('div',{class:'prefs'},lang,theme),
  h('div',{class:'chips',id:'chips'}));
}
function chip(dotCls,labelKey,value){return h('span',{class:'chip'},h('span',{class:'dot '+dotCls}),h('span',{i18n:labelKey}),value!=null&&h('b',{},value));}
function renderChips(s){
 const c=$('chips');if(!c)return;
 const wifi=s.wifi_connected?chip('ok','chip.wifi',s.ssid+' · '+s.ip):s.ap_active?chip('warn','chip.hotspot',s.ap_name):chip('bad','chip.wifi',t('state.connecting'));
 const mq=!s.mqtt_host?chip('','chip.mqtt',t('state.off')):s.mqtt_connected?chip('ok','chip.mqtt',t('state.ready')):chip('warn','chip.mqtt',t('s.'+(s.mqtt_status||'connecting')));
 c.replaceChildren(wifi,mq,h('span',{class:'chip'},h('span',{i18n:'chip.firmware'}),h('b',{class:'num'},s.version||'—')),...(UI.device.chips?UI.device.chips(s):[]));
}
function renderBanners(s,lost){
 const b=$('banners');if(!b)return;b.replaceChildren();
 const add=(cls,ic,text)=>b.append(h('div',{class:'banner '+cls,role:cls==='bad'?'alert':null},icon(ic),h('span',{},text)));
 if(lost){add('bad','alert',t('banner.lost'));return;}
 if(s.ota_active)add('info','auto',t('banner.ota',{p:s.ota_progress}));
 if(s.reboot_pending)add('info','auto',t('banner.rebooting'));
 if(s.mqtt_cleanup_warning)add('warn','alert',t('banner.cleanup'));
 if(s.save_error)add('bad','alert',t('banner.save_error',{e:s.save_error}));
 if(s.ap_active&&!s.wifi_connected)add('warn','wifi',t('banner.hotspot',{n:s.ap_name}));
}
function buildNav(dev){
 const side=$('side');side.replaceChildren();
 for(const [groupKey,keys] of dev.groups){
  const links=keys.filter(k=>UI.pages[k]).map(k=>h('a',{href:'#settings/'+k,'data-i18n':dev.nav[k]},t(dev.nav[k])));
  if(!links.length)continue;
  side.append(h('div',{class:'group','data-i18n':groupKey},t(groupKey)));
  for(const a of links)side.append(a);
 }
}
function showRoute(){
 const m=/^settings\/([a-z]+)$/.exec(location.hash.slice(1));
 const page=m&&document.querySelector('[data-settings-page="'+m[1]+'"]')?m[1]:m?(UI.device.groups.flatMap(g=>g[1]).find(k=>UI.pages[k])||null):null;
 $('homePage').hidden=!!page;$('settingsPage').hidden=!page;
 document.querySelectorAll('[data-settings-page]').forEach(p=>{p.hidden=p.dataset.settingsPage!==page;});
 document.querySelectorAll('.tabs a').forEach(a=>{const on=a.dataset.tab===(page?'settings':'home');if(on)a.setAttribute('aria-current','page');else a.removeAttribute('aria-current');});
 document.querySelectorAll('.side a').forEach(a=>{if(a.hash==='#settings/'+page)a.setAttribute('aria-current','page');else a.removeAttribute('aria-current');});
 if(page&&!UI.state)load().then(()=>afterShow(page));else afterShow(page);
}
function afterShow(page){
 if(!page)return;
 const active=document.querySelector('.side a[aria-current]');if(active&&active.scrollIntoView)active.scrollIntoView({block:'nearest',inline:'center'});
 const p=UI.pages[page];if(p&&p.show)p.show();
}
/* Wi-Fi scan is shared: start, poll, and hand the sorted list to a callback. */
async function scanNetworks(onList){
 await command('wifi.scan');
 for(let i=0;i<25;i++){const r=await command('wifi.scan.get');if(r.scanning){await new Promise(res=>setTimeout(res,700));continue;}onList(r.networks||[]);return;}
 throw Error(t('wifi.scan_timeout'));
}

/* ---------- Shared settings pages ---------- */
function wifiPage(){
 const sum=h('div'),list=h('div',{class:'list',hidden:true});
 const ssid=h('input',{id:'ssid',maxLength:32,required:true,autocomplete:'off'});
 const pass=h('input',{id:'wifiPassword',type:'password',maxLength:63,autocomplete:'new-password',placeholder:t('wifi.pass_ph')});
 const open=switchRow('openNetwork','wifi.open');
 const scanBtn=h('button',{type:'button',class:'btn',id:'scan'},icon('auto'),h('span',{i18n:'wifi.scan'}));
 const form=h('form',{id:'wifiForm',class:'stack'},
  h('label',{class:'field'},h('span',{i18n:'wifi.ssid'}),h('div',{class:'row'},h('div',{class:'grow'},ssid),scanBtn)),list,
  h('label',{class:'field'},h('span',{i18n:'wifi.password'}),pass),open,
  h('div',{class:'form-actions'},h('button',{class:'btn primary',type:'submit'},h('span',{i18n:'wifi.save'}))),info('wifi.hint'));
 open.querySelector('input').onchange=e=>{pass.disabled=e.target.checked;};
 scanBtn.onclick=()=>run(scanBtn,()=>scanNetworks(net=>{
  list.replaceChildren(...net.slice().sort((a,b)=>b.rssi-a.rssi).map(n=>h('button',{type:'button',class:'item',style:'cursor:pointer;text-align:left;font:inherit;color:inherit;width:100%',
   onclick:()=>{ssid.value=n.ssid;open.querySelector('input').checked=n.security===0;pass.disabled=n.security===0;list.hidden=true;pass.focus();}},
   icon('wifi'),h('span',{class:'grow'},h('b',{},n.ssid),h('span',{},t('wifi.signal.'+(n.rssi>-55?3:n.rssi>-70?2:1))+' · '+n.rssi+' dBm')),n.security===0?h('span',{class:'badge',i18n:'wifi.open_badge'}):icon('lock'))));
  list.hidden=false;
  if(!net.length)throw Error(t('wifi.none'));
 }),null);
 onSubmit(form,()=>api('/wifi/config',{ssid:ssid.value,password:pass.value,open_network:open.querySelector('input').checked}),'wifi.verifying');
 return {el:h('div',{class:'stack'},pageHead('nav.wifi','wifi.lead'),sum,card('wifi.change',null,form)),
  status(s){sum.replaceChildren(summary([['wifi.status',s.wifi_connected?t('state.connected'):s.ap_active?t('state.hotspot'):t('state.connecting'),s.wifi_connected?'ok':s.ap_active?'warn':'bad'],
   ['wifi.ssid',s.ssid||'—'],['wifi.ip',s.ip||'—'],s.ap_active?['wifi.ap',s.ap_name]:['wifi.saved_password',t(s.wifi_password_set?'common.yes':'common.no')]]));},
  init(s){ssid.value=s.ssid||'';}};
}
function mqttPage(dev){
 const sum=h('div');
 const host=h('input',{id:'mqttHost',maxLength:dev.mqttHostMax||127,placeholder:t('mqtt.host_ph')});
 const port=h('input',{id:'mqttPort',type:'number',min:1,max:65535,required:true,value:'1883'});
 const user=h('input',{id:'mqttUser',maxLength:63,autocomplete:'off'});
 const pass=h('input',{id:'mqttPassword',type:'password',maxLength:256,autocomplete:'new-password',placeholder:t('mqtt.pass_ph')});
 const protocol=dev.model==='TC1'?h('select',{id:'mqttMode'},h('option',{value:'tmcp'},'TMCP (v2)'),h('option',{value:'legacy'},'Legacy (v1)')):null;
 const clear=switchRow('clearMqttPassword','mqtt.clear','mqtt.clear_desc');
 const form=h('form',{id:'mqttForm',class:'stack'},
  h('div',{class:'fields',style:'grid-template-columns:minmax(0,3fr) minmax(90px,1fr)'},
   h('label',{class:'field'},h('span',{i18n:'mqtt.host'}),host),h('label',{class:'field'},h('span',{i18n:'mqtt.port'}),port)),
  h('div',{class:'fields'},h('label',{class:'field'},h('span',{i18n:'mqtt.user'}),user),h('label',{class:'field'},h('span',{i18n:'mqtt.password'}),pass)),
  h('details',{class:'more'},h('summary',{i18n:'common.advanced'}),...(protocol?[h('label',{class:'field'},h('span',{i18n:'mqtt.mode'}),protocol)]:[]),clear),info(dev.haHintKey),
  h('div',{class:'form-actions'},h('button',{class:'btn primary',type:'submit'},h('span',{i18n:'common.save'}))));
 onSubmit(form,()=>{
  const enc=new TextEncoder();if(enc.encode(user.value).length>63||enc.encode(pass.value).length>256)throw Error(t('mqtt.too_long'));
  return api('/mqtt/config',{host:host.value.trim(),port:Number(port.value),user:user.value,password:pass.value,clear_password:clear.querySelector('input').checked,...(protocol?{protocol:protocol.value}:{})});
 });
 return {el:h('div',{class:'stack'},pageHead('nav.mqtt','mqtt.lead'),sum,card('mqtt.broker',null,form)),
  status(s){sum.replaceChildren(summary([['mqtt.status',!s.mqtt_host?t('state.off'):s.mqtt_connected?t('state.ready'):t('s.'+(s.mqtt_status||'connecting')),!s.mqtt_host?'':s.mqtt_connected?'ok':'warn'],
   ['mqtt.host',s.mqtt_host?s.mqtt_host+':'+s.mqtt_port:'—'],['mqtt.mode',t(s.mqtt_topic_mode==='legacy'?'mqtt.mode_legacy':'mqtt.mode_tmcp')],['mqtt.user',s.mqtt_user||'—']]));},
  init(s){if(protocol)protocol.value=s.mqtt_protocol||s.mqtt_topic_mode||'tmcp';host.value=s.mqtt_host||'';port.value=s.mqtt_port||1883;user.value=s.mqtt_user||'';}};
}
function firmwarePage(dev){
 const online=h('section',{class:'card stack',hidden:!UI.caps?.actions?.includes('ota.sources.get')});
 if(!online.hidden){
  const source=h('select',{id:'otaSource'}),latest=h('p',{class:'help'}),notes=h('a',{target:'_blank',rel:'noopener',hidden:true},t('fw.notes'));
  const install=h('button',{class:'btn primary',type:'button',hidden:true},t('fw.install_online'));
  const check=h('button',{class:'btn',type:'button'},t('fw.check'));
  let timer=null,last=null;
  function translate(){check.textContent=t('fw.check');install.textContent=t('fw.install_online');notes.textContent=t('fw.notes');for(const option of source.options)option.textContent=t('fw.source_'+option.value);if(last)latest.textContent=t('fw.check_'+last.state)+(last.version?' · '+last.version:'');}
  I18N.onChange(translate);
  async function poll(){
   const data=await command('ota.check.get');last=data;
   latest.textContent=t('fw.check_'+data.state)+(data.version?' · '+data.version:'');
   install.hidden=!data.available;notes.hidden=!data.release_log;
   if(data.release_log)notes.href=data.release_log;
   if(data.state==='checking')timer=setTimeout(()=>poll().catch(e=>{latest.textContent=e.message;check.disabled=false;}),1000);
   else check.disabled=false;
  }
  check.onclick=()=>run(check,async()=>{if(timer)clearTimeout(timer);install.hidden=true;notes.hidden=true;await command('ota.check',{source:source.value});latest.textContent=t('fw.check_checking');check.disabled=true;timer=setTimeout(()=>poll().catch(e=>{latest.textContent=e.message;check.disabled=false;}),500);});
  source.onchange=()=>{if(timer)clearTimeout(timer);install.hidden=true;notes.hidden=true;latest.textContent='';check.disabled=false;};
  install.onclick=()=>run(install,async()=>{if(!window.confirm(t('fw.confirm_online')))return;await command('ota.install');toast(t('fw.url_started'));});
  command('ota.sources.get').then(data=>{for(const id of data.sources)source.append(h('option',{value:id},t('fw.source_'+id)));}).catch(()=>{online.hidden=true;});
  online.append(h('h2',{i18n:'fw.online'}),source,h('div',{class:'row'},check,install),latest,notes);
 }
 const sum=h('div'),prog=h('progress',{max:100,value:0}),state=h('p',{class:'help'});
 const file=h('input',{id:'firmware',type:'file',accept:'.bin,.raw,.signed',required:true});
 const unsigned=switchRow('unsignedRescue','fw.unsigned','fw.unsigned_desc');
 const url=h('input',{id:'otaUrl',type:'url',maxLength:499,required:true,placeholder:'http://…/'+dev.otaName});
 const fileForm=h('form',{id:'fileForm',class:'stack'},h('label',{class:'field'},h('span',{},t('fw.file',{m:dev.model})),file),
  h('details',{class:'more'},h('summary',{i18n:'common.advanced'}),unsigned),
  h('div',{class:'form-actions'},h('button',{class:'btn primary',type:'submit'},h('span',{i18n:'fw.upload'}))));
 const urlForm=h('form',{id:'urlForm',class:'stack',hidden:true},h('label',{class:'field'},h('span',{i18n:'fw.url'}),url),
  h('div',{class:'form-actions'},h('button',{class:'btn primary',type:'submit'},h('span',{i18n:'fw.download'}))));
 const picker=seg([['file','fw.tab_file'],['url','fw.tab_url']],v=>{fileForm.hidden=v!=='file';urlForm.hidden=v!=='url';});picker.set('file');
 // Upload streams raw bytes; the request is synchronous up to send(), like the old page.
 let uploading=false;
 fileForm.addEventListener('submit',e=>{
  e.preventDefault();const f=file.files&&file.files[0];if(!f||uploading)return;
  const rescue=unsigned.querySelector('input').checked;
  if(rescue&&!window.confirm(t('fw.unsigned_confirm')))return;
  uploading=true;prog.value=0;
  const xhr=new XMLHttpRequest();xhr.open('POST',rescue?'/api/v1/ota/upload-unsigned':'/api/v1/ota/upload');
  xhr.setRequestHeader('Content-Type','application/octet-stream');xhr.timeout=310000;
  xhr.upload.onprogress=ev=>{if(ev.lengthComputable){prog.value=ev.loaded/ev.total*100;state.textContent=t('fw.uploading',{p:Math.round(ev.loaded/ev.total*100)});}};
  xhr.onload=()=>{const ok=xhr.status===202;state.textContent=ok?t('fw.verify_ok'):t('fw.failed',{e:xhr.responseText});toast(state.textContent,!ok);};
  xhr.onerror=()=>toast(t('fw.upload_lost'),true);
  xhr.ontimeout=()=>toast(t('fw.upload_timeout'),true);
  xhr.onloadend=()=>{uploading=false;};
  xhr.send(f);
 });
 onSubmit(urlForm,()=>api('/ota',{url:url.value}),'fw.url_started');
 const fileLabel=fileForm.querySelector('.field>span');I18N.onChange(()=>{fileLabel.textContent=t('fw.file',{m:dev.model});});
 return {el:h('div',{class:'stack'},pageHead('nav.firmware','fw.lead'),sum,online,
  card('fw.install',null,picker,h('div',{style:'margin-top:16px'},fileForm,urlForm),h('div',{class:'stack',style:'margin-top:20px'},prog,state),info(dev.otaHintKey))),
  status(s){sum.replaceChildren(summary([['fw.version',h('span',{class:'num'},s.version||'—')],['fw.state',t('s.'+(s.ota_state||'idle'))],s.ota_error?['fw.error',h('span',{class:'badge bad'},s.ota_error)]:null]));
   if(s.ota_active){prog.value=s.ota_progress;state.textContent=t('fw.progress',{p:s.ota_progress});}}};
}
function diagValue(k,v){
 if(v===null||v===undefined||v==='')return '—';
 if(typeof v==='boolean')return t(v?'common.yes':'common.no');
 if(k==='uptime_seconds')return duration(v);
 if(k==='free_memory')return (v/1024).toFixed(1)+' KB';
 if(k==='age_ms')return (v/1000).toFixed(1)+' s';
 if(k==='ntp_last_success'||k==='utc_time')return new Date(v*1000).toLocaleString(I18N.lang);
 if(Array.isArray(v))return v.join(', ');
 if(typeof v==='string'&&I18N.has('s.'+v))return t('s.'+v);
 return String(v);
}
function diagnosticsPage(dev){
 const health=h('div',{class:'health'}),groups=h('div',{class:'diag'}),raw=h('pre'),updated=h('p',{class:'help'});
 const extra=dev.diagExtra?dev.diagExtra():null;
 return {el:h('div',{class:'stack'},pageHead('nav.diagnostics','diag.lead'),card('diag.health',null,health),
   h('section',{},h('div',{class:'section-label',i18n:'diag.details'}),groups),extra,
   h('details',{class:'more'},h('summary',{i18n:'diag.raw'}),raw),updated),
  status(s){
   if(!UI.pages.diagnostics||$('settingsPage').hidden)return;
   const checks=[['diag.c_wifi',s.wifi_connected?'ok':'bad',s.wifi_connected?s.ssid:t('state.disconnected')],
    ['diag.c_mqtt',!s.mqtt_host?'':s.mqtt_connected?'ok':'warn',!s.mqtt_host?t('state.off'):t('s.'+(s.mqtt_status||'connecting'))+(s.mqtt_errors?' · '+t('diag.errors',{n:s.mqtt_errors}):'')],
    ...(dev.healthChecks?dev.healthChecks(s):[]),['diag.c_ota',s.ota_error?'bad':'ok',s.ota_error||t('s.'+(s.ota_state||'idle'))]];
   health.replaceChildren(...checks.map(([k,cls,text])=>h('div',{class:'item'},h('span',{class:'dot '+cls}),h('span',{class:'grow'},h('b',{i18n:k})),h('span',{class:'help'},text))));
   const seen=new Set();
   const all=[['diag.g_device',['name','device_id','version','uptime_seconds','free_memory','web_password_set']],
    ['diag.g_network',['wifi_connected','ssid','ip','ap_active','ap_name','scan_busy','wifi_password_set','wifi_trial_status','wifi_trial_active']],
    ['diag.g_mqtt',['mqtt_connected','mqtt_status','mqtt_host','mqtt_port','mqtt_user','mqtt_topic_mode','mqtt_password_set','mqtt_errors','mqtt_last_error','mqtt_cleanup_warning']],
    ...(dev.diagGroups||[]),['diag.g_ota',['ota_active','ota_state','ota_progress','ota_error']]];
   const open=new Set([...groups.querySelectorAll('details')].filter(d=>d.open).map(d=>d.dataset.key));
   groups.replaceChildren(...all.map(([key,keys],i)=>{const rows=keys.filter(k=>Object.hasOwn(s,k)).map(k=>{seen.add(k);return [k,s[k]];});if(!rows.length)return null;
    return h('details',{'data-key':key,open:open.size?open.has(key):i<2},h('summary',{},h('span',{i18n:key}),h('span',{class:'badge'},rows.length)),
     h('dl',{},rows.map(([k,v])=>[h('dt',{},I18N.has('d.'+k)?t('d.'+k):k),h('dd',{class:'num'},diagValue(k,v))])));}).filter(Boolean));
   const rest=Object.keys(s).filter(k=>!seen.has(k)&&!(Array.isArray(s[k])&&typeof s[k][0]==='object'));
   if(rest.length)groups.append(h('details',{'data-key':'diag.g_other'},h('summary',{},h('span',{i18n:'diag.g_other'}),h('span',{class:'badge'},rest.length)),
    h('dl',{},rest.map(k=>[h('dt',{},I18N.has('d.'+k)?t('d.'+k):k),h('dd',{class:'num'},diagValue(k,s[k]))]))));
   raw.textContent=JSON.stringify(s,null,2);updated.textContent=t('diag.updated',{t:new Date().toLocaleTimeString(I18N.lang)});
  }};
}
function logsPage(){
 const out=h('pre',{},''),rows=[],state=h('p',{class:'help'});
 let cursor=0;
 async function read(){
  const d=await command('logs.get',{after:cursor,limit:8});
  if(d.overwritten)rows.push(t('logs.overwritten'));
  for(const r of d.records)rows.push(JSON.stringify(r));
  cursor=d.next;out.textContent=rows.join('\n')||t('logs.empty');return d;
 }
 const refresh=h('button',{type:'button',class:'btn'},icon('auto'),h('span',{i18n:'diag.log_refresh'}));
 refresh.onclick=()=>run(refresh,read,null);
 const all=h('button',{type:'button',class:'btn'},h('span',{i18n:'logs.load_all'}));
 all.onclick=()=>run(all,async()=>{for(let i=0;i<8;i++){const d=await read();if(cursor>=d.latest)break;}},null);
 const dl=h('button',{type:'button',class:'btn'},h('span',{i18n:'logs.download'}));
 dl.onclick=()=>run(dl,async()=>{
  for(let i=0;i<8;i++){const d=await read();if(cursor>=d.latest)break;}
  const url=URL.createObjectURL(new Blob([rows.join('\n')],{type:'text/plain;charset=utf-8'})),a=h('a',{href:url,download:'device-log.txt'});
  document.body.append(a);a.click();a.remove();URL.revokeObjectURL(url);
 },null);
 return {el:h('div',{class:'stack'},pageHead('nav.logs','logs.lead'),
  card('diag.logs',null,h('p',{class:'help',i18n:'logs.hint'}),h('div',{class:'form-actions'},refresh,all,dl),out,state))};
}
function maintenancePage(dev){
 const reboot=h('button',{type:'button',class:'btn'},h('span',{i18n:'sys.reboot'}));
 reboot.onclick=async()=>{if(await confirmDialog({title:t('sys.reboot_q',{m:dev.model}),body:t('sys.reboot_body'),ok:t('sys.reboot')}))run(reboot,()=>api('/reboot',{}),'toast.rebooting');};
 const items=[h('div',{class:'item'},h('div',{class:'grow'},h('b',{i18n:'sys.reboot'}),h('span',{i18n:'sys.reboot_desc'})),reboot)];
 if(dev.factoryReset){
  const reset=h('button',{type:'button',class:'btn danger'},h('span',{i18n:'sys.reset'}));
  reset.onclick=async()=>{if(await confirmDialog({title:t('sys.reset_q'),body:t('sys.reset_body'),ok:t('sys.reset'),danger:true}))run(reset,()=>api('/device/reset',{full:true}),'toast.resetting');};
  items.push(h('div',{class:'item'},h('div',{class:'grow'},h('b',{i18n:'sys.reset'}),h('span',{i18n:'sys.reset_desc'})),reset));
 }
 return h('div',{class:'stack'},pageHead('nav.maintenance','sys.lead'),
  h('section',{class:'card danger-zone'},h('div',{class:'list'},items),
   h('p',{class:'help',style:'margin-top:12px',i18n:dev.buttonHintKey},t(dev.buttonHintKey))));
}
/* Slots shared by every "General" page. */
function nameCard(){
 const input=h('input',{id:'deviceName',maxLength:63}),meta=h('div');
 const form=h('form',{id:'nameForm',class:'stack'},h('label',{class:'field'},h('span',{i18n:'gen.name'}),
  h('div',{class:'row'},h('div',{class:'grow'},input),h('button',{class:'btn primary',type:'submit'},h('span',{i18n:'common.save'}))),
  h('button',{class:'btn ghost',type:'button',onclick:e=>run(e.currentTarget,async()=>{await api('/deviceName',{name:''});await UI.reload();input.value=UI.state?.name||'';},'common.saved')},h('span',{i18n:'gen.default_name'}))),meta);
 onSubmit(form,()=>{if(new TextEncoder().encode(input.value).length>63)throw Error(t('gen.name_long'));return api('/deviceName',{name:input.value});});
 return {el:card('gen.identity',null,form),init(s){input.value=s.name||'';},status(s){meta.replaceChildren(summary([['gen.device_id',h('span',{class:'num'},prettyMac(s.device_id))],['gen.uptime',duration(s.uptime_seconds||0)]]));}};
}
function authCard(){
 const input=h('input',{id:'webPassword',type:'password',maxLength:32,autocomplete:'new-password',placeholder:t('auth.ph')});
 const state=h('span',{class:'badge'});
 const form=h('form',{id:'authForm',class:'stack'},h('label',{class:'field'},h('span',{i18n:'auth.password'}),input,h('p',{class:'help',i18n:'auth.help'})),
  h('div',{class:'form-actions'},h('button',{class:'btn primary',type:'submit'},h('span',{i18n:'auth.set'}))));
 onSubmit(form,()=>{if(input.value&&!/^[\x20-\x7e]{8,32}$/.test(input.value))throw Error(t('auth.invalid'));return api('/auth/password',{password:input.value});});
 const c=card('auth.title','auth.lead',form);c.querySelector('.card-head').append(state);
 return {el:c,status(s){state.className='badge '+(s.web_password_set?'ok':'');state.textContent=t(s.web_password_set?'auth.on':'auth.off');}};
}
function clockCard({load,save,status}){
 const sum=h('div'),tz=h('select'),ntp=h('input',{maxLength:63,required:true,placeholder:'ntp1.aliyun.com'});
 const fill=()=>{const v=tz.value;tz.replaceChildren(...TZ.map(m=>h('option',{value:m},tzLabel(m))));tz.value=v;};fill();I18N.onChange(fill);
 const form=h('form',{class:'stack'},h('div',{class:'fields'},
   h('label',{class:'field'},h('span',{i18n:'clock.tz'}),tz),h('label',{class:'field'},h('span',{i18n:'clock.ntp'}),ntp)),
  info('clock.hint'),h('div',{class:'form-actions'},h('button',{class:'btn primary',type:'submit'},h('span',{i18n:'common.save'}))));
 onSubmit(form,()=>save({timezone_minutes:Number(tz.value),ntp_host:ntp.value.trim()}));
 return {el:card('clock.title',null,sum,h('div',{style:'margin-top:16px'},form)),
  async show(){const c=await load();if(!c)return;tz.value=String(c.timezone_minutes||480);ntp.value=c.ntp_host||'';},
  status(s){sum.replaceChildren(summary(status(s)));}};
}

/* ---------- Boot ---------- */
let initialized=false,failures=0,lastSuccess=0,loading=null;
async function load(){
 try{
  const raw=await command('status.get');failures=0;lastSuccess=Date.now();const s=UI.acceptStatus(raw);
  $('title').textContent=UI.device.model+' '+(s.name||'');
  renderChips(s);renderBanners(s,false);
  if(!initialized){for(const p of Object.values(UI.pages))if(p.init)p.init(s);initialized=true;}
  for(const p of Object.values(UI.pages))if(p.status)p.status(s);
  if(UI.device.onStatus)UI.device.onStatus(s);
  return raw;
 }catch(e){if(++failures>=3&&(!lastSuccess||Date.now()-lastSuccess>=30000))renderBanners(null,true);return null;}
}
function pollLoad(){if(!loading)loading=load().finally(()=>{loading=null;});return loading;}
async function refresh(){await pollLoad();setTimeout(refresh,UI.device.interval||2000);}
UI.reload=async()=>{if(loading)await loading;return pollLoad();};
UI.start=async dev=>{
 UI.device=dev;
 const first=dev.groups.flatMap(g=>g[1])[0];
 document.body.prepend(header(dev),
  h('nav',{class:'tabs','aria-label':'Main'},
   h('a',{href:'#home','data-tab':'home'},icon('home'),h('span',{i18n:'nav.home'})),
   h('a',{href:'#settings/'+first,'data-tab':'settings'},icon('sliders'),h('span',{i18n:'nav.settings'}))),
  h('div',{id:'banners'}));
 UI.caps=await command('capabilities.get').catch(()=>null);
 const pages=$('pages');
 // Pages the framework builds for every product.
 const builders={network:wifiPage,mqtt:mqttPage,firmware:firmwarePage,diagnostics:diagnosticsPage,maintenance:maintenancePage,logs:logsPage};
 for(const [key,build] of Object.entries(builders)){const p=build(dev);UI.pages[key]=p;pages.append(h('div',{'data-settings-page':key,hidden:true},p.el));}
 // Automation pages, when the device advertises the capability.
 Object.assign(UI.pages,window.automationPages(dev));
 // Product pages: the product supplies show()/status(); the markup is either
 // already in the document or appended below.
 for(const [key,p] of Object.entries(dev.pages))UI.pages[key]=Object.assign(UI.pages[key]||{},p);
 for(const [key,p] of Object.entries(UI.pages)){
  let box=document.querySelector('[data-settings-page="'+key+'"]');
  if(!box){box=h('div',{'data-settings-page':key,hidden:true});pages.append(box);}
  if(p.el)box.append(p.el);
 }
 for(const slot of document.querySelectorAll('[data-slot]')){const p=slot.dataset.slot==='name'?nameCard():authCard();UI.pages['slot_'+slot.dataset.slot]=p;slot.replaceWith(p.el);}
 buildNav(dev);
 window.addEventListener('hashchange',showRoute);showRoute();
 I18N.apply();
 I18N.onChange(()=>{if(UI.state){renderChips(UI.state);renderBanners(UI.state,false);for(const p of Object.values(UI.pages))if(p.status)p.status(UI.state);}});
 refresh();
};
})();
