/* M1 air quality monitor page. The monitor only reports readings and answers
 * link requests from an A1; linkage rules live on the A1. */
(()=>{
'use strict';
const {t}=I18N;
const {$,h,icon,toast,run,confirm,summary,card,info,pageHead,seg,prettyMac,normalizeMac,clone}=UI;
const METRICS=['pm25','formaldehyde','temperature','humidity'];
const STALE_MS=15000;
let binding={response_ids:[]},bindingStatus={peers:[]};

/* ---------- Home: readings ---------- */
const tiles={};
$('metrics').append(...METRICS.map(k=>{
 const value=h('span',{class:'num'}),badge=h('span',{class:'badge'}),scale=h('div');
 tiles[k]={value,badge,scale};
 return h('section',{class:'card metric'},h('div',{class:'row between'},h('span',{class:'label','data-i18n':'m.'+k},t('m.'+k)),badge),
  h('div',{class:'stat'},value,h('small',{},UI.AIR[k].unit)),scale);
}));
const screen=seg([[0,'state.off'],[1,'1'],[2,'2'],[3,'3'],[4,'4']],v=>run(null,()=>UI.control('display.set',{brightness:v*25},{brightness_requested:v}),'toast.accepted'),{full:true,aria:t('m1.screen')});
screen.dataset.control='brightness';
$('screenSeg').replaceWith(screen);
const RANK={ok:0,warn:1,bad:2};
function renderHome(s){
 const stale=!s.sensor_valid||s.age_ms>STALE_MS;
 const fmt={pm25:v=>String(Math.round(v)),formaldehyde:v=>v.toFixed(3),temperature:v=>v.toFixed(1),humidity:v=>v.toFixed(0)};
 const grades={};
 for(const k of METRICS){const g=UI.grade(k,s[k]);grades[k]=g;const el=tiles[k];
  el.value.textContent=s.sensor_received?fmt[k](s[k]):'—';
  el.badge.className='badge '+(stale?'':g.cls);el.badge.textContent=t(g.key);
  el.scale.replaceChildren(UI.airScale(k,s[k]));}
 // Overall verdict comes from the pollutants; comfort only adds a remark.
 const worst=['pm25','formaldehyde'].reduce((a,k)=>RANK[grades[k].cls]>RANK[grades[a].cls]?k:a,'pm25');
 const v=$('verdict');v.dataset.level=stale?'':grades[worst].cls;
 const both=g=>grades[g].cls==='ok';
 $('overall').textContent=stale?t('m1.no_data'):t(both('pm25')&&both('formaldehyde')?(grades.pm25.key.endsWith('excellent')?'m1.v_excellent':'m1.v_good'):grades[worst].cls==='warn'?'m1.v_fair':'m1.v_poor');
 const why=METRICS.filter(k=>grades[k].cls!=='ok').map(k=>t('m.'+k)+' '+t(grades[k].key));
 $('overallWhy').textContent=stale?t('m1.stale_hint'):why.length?why.join(' · '):t('m1.all_ok');
 $('freshDot').className='dot '+(stale?'bad':'ok live');
 $('freshText').textContent=stale?t('m1.stale'):t('m1.updated',{n:Math.max(0,Math.round(s.age_ms/1000))});
 if(document.activeElement&&document.activeElement.parentNode!==screen)screen.set(s.brightness_requested);
}
function renderPeers(){
 const ids=binding.response_ids||[],peers=bindingStatus.peers||[];
 const ready=peers.filter(p=>p.session_ready).length;
 const head=h('div',{class:'card-head'},
  h('div',{},h('h2',{i18n:'m1.linked'}),h('p',{class:'help',i18n:'m1.linked_desc'})),
  h('span',{class:'badge '+(ready?'ok':'')},ready+' / '+ids.length));
 if(!ids.length){
  $('peerCard').replaceChildren(head,h('div',{class:'empty'},h('p',{},t('m1.linked_empty')),
   h('a',{class:'btn',href:'#settings/binding',style:'margin-top:12px'},icon('link'),h('span',{},t('m1.allow_btn')))));
  return;
 }
 const rows=ids.map(id=>{
  const a1=id.split(':')[0],p=peers.find(x=>x.mac===a1)||{};
  return h('div',{class:'item'},icon('fan'),
   h('div',{class:'grow'},h('b',{class:'num'},'A1 · '+prettyMac(a1)),h('span',{},p.ip||'—')),
   h('span',{class:'badge '+(p.session_ready?'ok':'warn')},t(p.session_ready?'m1.session_ok':'m1.session_wait')));
 });
 $('peerCard').replaceChildren(head,h('div',{class:'list'},rows));
}


/* ---------- Settings: linkage permission ---------- */
async function showBinding(){
 const page=document.querySelector('[data-settings-page="binding"]');
 const [cfg,status]=await Promise.all([api('/binding/config'),api('/binding/status').catch(()=>({peers:[]}))]);
 binding=cfg;bindingStatus=status;
 const save=()=>api('/binding/config',{response_ids:binding.response_ids}).then(showBinding);
 const own=String((UI.state&&UI.state.device_id)||'').toLowerCase();
 const input=h('input',{placeholder:'b0f893303134:'+(own||'b0f8932864b6'),maxLength:25,autocomplete:'off',spellcheck:false});
 const ids=(binding.response_ids||[]).slice();
 const add=h('form',{class:'subcard stack'},h('h3',{i18n:'m1.allow_add'}),
  h('label',{class:'field'},h('span',{i18n:'m1.pair_id'}),
   h('div',{class:'row'},h('div',{class:'grow'},input),h('button',{class:'btn primary',type:'submit',disabled:ids.length>=8},icon('plus'),h('span',{},t('common.add'))))),
  h('p',{class:'help',i18n:'m1.pair_help'}));
 UI.onSubmit(add,()=>{
  const parts=input.value.trim().split(':'),a1=normalizeMac(parts[0]||''),m1=normalizeMac(parts[1]||'');
  if(!a1||!m1)throw Error(t('m1.pair_bad'));
  if(own&&m1!==own)throw Error(t('m1.pair_other'));
  const id=a1+':'+m1;if(ids.includes(id))throw Error(t('bind.dup'));
  if(ids.length>=8)throw Error(t('common.max',{n:8}));
  binding.response_ids=ids.concat([id]);return save();},'bind.added');
 page.replaceChildren(pageHead('nav.permission','m1.perm_lead'),
  h('section',{class:'card'},h('div',{class:'card-head'},h('h2',{i18n:'m1.allowed'}),h('span',{class:'badge'},ids.length+' / 8')),
   ids.length?h('div',{class:'list'},ids.map((id,i)=>{const a1=id.split(':')[0],p=(bindingStatus.peers||[]).find(x=>x.mac===a1)||{};
    return h('div',{class:'item'},icon('fan'),h('div',{class:'grow'},h('b',{class:'num'},'A1 · '+prettyMac(a1)),h('span',{class:'num'},id)),
     h('span',{class:'badge '+(p.session_ready?'ok':'warn')},t(p.session_ready?'m1.session_ok':'m1.session_wait')),
     h('button',{type:'button',class:'btn ghost icon','aria-label':t('common.delete'),onclick:ev=>{
      const rest=ids.slice();rest.splice(i,1);
      run(ev.currentTarget,async()=>{await api('/binding/config',{response_ids:rest});await showBinding();},'common.deleted');}},icon('trash')));}))
   :h('div',{class:'empty'},t('m1.allowed_empty')),add,info('m1.perm_hint')));
}

/* ---------- Boot ---------- */
const clock=UI.clockCard({
 load:async()=>UI.state||await api('/status'),
 save:async v=>{
  const s=UI.state;
  if(v.timezone_minutes!==s.timezone_minutes)await api('/time/config',{timezone_minutes:v.timezone_minutes});
  if(v.ntp_host!==s.ntp_host)await api('/ntp/config',{ntp_host:v.ntp_host});
 },
 status:s=>[['clock.synced',t('s.'+(s.ntp_status||'syncing')),s.ntp_synced?'ok':'warn'],
  ['clock.tz',UI.tzLabel(s.timezone_minutes||0)],['clock.ntp',s.ntp_host||'—'],
  ['clock.last',s.ntp_last_success?new Date(s.ntp_last_success*1000).toLocaleString(I18N.lang):'—']]});
$('clockCard').replaceWith(clock.el);

UI.start({
 model:'M1',kindKey:'kind.m1',icon:'air',mqttHostMax:127,factoryReset:true,
 otaName:'phicomm_m1.ota.bin.signed',haHintKey:'mqtt.ha_m1',otaHintKey:'fw.hint_m1',buttonHintKey:'sys.hint_m1',
 scheduleLead:'sch.lead_a1',interval:1000,
 groups:[['grp.network',['network','mqtt']],['grp.device',['device']],['grp.auto',['binding','schedule','night']],['grp.system',['firmware','diagnostics','logs','maintenance']]],
 nav:{network:'nav.wifi',mqtt:'nav.mqtt',device:'nav.general',binding:'nav.permission',schedule:'nav.schedule',night:'nav.sleep',firmware:'nav.firmware',diagnostics:'nav.diagnostics',logs:'nav.logs',maintenance:'nav.maintenance'},
 pages:{device:{show:clock.show,status:clock.status},binding:{show:showBinding}},
 diagGroups:[['diag.g_sensor',['uart_ready','sensor_valid','sensor_received','age_ms','temperature','humidity','formaldehyde','pm25','rx_frames','unknown_frames','uart_errors','discarded_bytes','last_frame']],
  ['diag.g_clock',['timezone_minutes','ntp_host','ntp_status','ntp_server','ntp_synced','ntp_screen_queued','ntp_last_success','ntp_next_seconds']],
  ['diag.g_screen',['brightness_requested']],
  ['diag.g_ota_img',['ota_signature_verified','ota_image_verified','ota_key_id','ota_image_length','ota_release_counter','ota_version']]],
 healthChecks:s=>[['diag.c_sensor',s.uart_ready&&s.sensor_valid?'ok':'bad',s.uart_errors?t('diag.errors',{n:s.uart_errors}):t(s.sensor_valid?'state.ok':'m1.no_data')],
  ['diag.c_clock',s.ntp_synced?'ok':'warn',t('s.'+(s.ntp_status||'syncing'))]],
 onStatus:renderHome
});
I18N.onChange(()=>{renderPeers();const p=document.querySelector('[data-settings-page="binding"]');if(p&&!p.hidden)showBinding();});
// The linked list is small and changes rarely; refresh it with the status.
(async function poll(){try{bindingStatus=await api('/binding/status');renderPeers();}catch{}setTimeout(poll,4000);})();
api('/binding/config').then(c=>{binding=c;renderPeers();}).catch(e=>toast(e.message,true));
})();
