/* TC1 smart power strip page: six socket tiles, power trend and the metering
 * settings the strip alone has. */
(()=>{
'use strict';
const {t}=I18N;
const {$,h,icon,toast,run,onSubmit,switchRow,summary,card,pageHead,seg}=UI;
const pendingSockets=new Map();
const names=()=>(UI.state&&UI.state.socket_names)||Array.from({length:6},(_,i)=>t('tc1.socket_n',{n:i+1}));

/* ---------- Home: sockets ---------- */
const tiles=[];
for(let i=0;i<6;i++){
 const name=h('b',{class:'name'}),state=h('span',{class:'state'});
 const tile=h('button',{type:'button',class:'tile',id:'socket'+i,'aria-pressed':'false'},
  h('span',{class:'tile-top'},icon('plug'),h('span',{class:'idx num'},String(i+1))),name,state);
 tile.onclick=()=>toggle(i);
 const edit=h('button',{type:'button',class:'btn ghost icon tile-edit','aria-label':t('tc1.rename')},icon('edit'));
 edit.onclick=e=>{e.stopPropagation();rename(i);};
 tiles.push({tile,name,state});
 $('tiles').append(h('div',{class:'tile-wrap'},tile,edit));
}
function lock(){for(let i=0;i<tiles.length;i++){const pending=pendingSockets.has(i)||pendingSockets.has('all');tiles[i].tile.disabled=pending;tiles[i].tile.setAttribute('aria-busy',String(pending));}$('allOn').disabled=pendingSockets.size>0;$('allOff').disabled=pendingSockets.size>0;}
async function setStates(params){
 const key=params.index===undefined?'all':params.index;if(pendingSockets.has(key)||(key==='all'&&pendingSockets.size)||pendingSockets.has('all'))return;
 pendingSockets.set(key,params.on);lock();
 const states=Array.from(UI.state.socket_states||[],Boolean);if(params.index===undefined)states.fill(params.on);else states[params.index]=params.on;
 UI.state={...UI.state,socket_states:states};renderSockets(UI.state);
 try{await api('/outlet',params);}
 catch(e){toast(e.message,true);}
 finally{pendingSockets.delete(key);lock();UI.reload().catch(()=>{});}
}
function toggle(i){setStates({index:i,on:!UI.state.socket_states[i]});}
$('allOn').onclick=()=>setStates({on:true});
$('allOff').onclick=()=>setStates({on:false});
async function rename(i){
 const input=h('input',{value:names()[i],maxLength:63,required:true});
 const d=h('dialog',{},h('form',{method:'dialog',class:'stack'},h('h2',{},t('tc1.rename')),
  h('label',{class:'field'},h('span',{},t('tc1.socket_n',{n:i+1})),input),
  h('div',{class:'form-actions'},h('button',{class:'btn primary',value:'ok'},t('common.save')),
   h('button',{class:'btn ghost',value:'cancel',formnovalidate:true},t('common.cancel')))));
 document.body.append(d);d.showModal();input.select();
 d.addEventListener('close',()=>{if(d.returnValue==='ok'&&input.value.trim())run(null,()=>api('/socketNames',{index:i,name:input.value.trim()}).then(UI.reload),'common.saved');d.remove();});
}
function renderSockets(s){
 lock();
 const states=Array.from(s.socket_states||[0,0,0,0,0,0]);
 for(const [key,on] of pendingSockets){if(key==='all')states.fill(on);else states[key]=on;}
 UI.state={...s,socket_states:states};
 $('onCount').textContent=t('tc1.on_count',{n:states.filter(Boolean).length});
 states.forEach((v,i)=>{const x=tiles[i];x.tile.setAttribute('aria-pressed',String(!!v));
  x.name.textContent=(s.socket_names||[])[i]||t('tc1.socket_n',{n:i+1});x.state.textContent=t(v?'tc1.on':'tc1.off')+(pendingSockets.has(i)||pendingSockets.has('all')?' · '+t('control.syncing'):'');});
 for(const id of ['childLock','childLockDevice'])$(id).checked=!!s.child_lock;
 for(const id of ['ledEnabled','ledEnabledDevice'])$(id).checked=!!s.led_enabled;
}
for(const id of ['childLock','childLockDevice'])$(id).onchange=e=>run(null,()=>UI.control('child_lock.set',{enabled:e.target.checked},{child_lock:e.target.checked}));
for(const id of ['ledEnabled','ledEnabledDevice'])$(id).onchange=e=>run(null,()=>UI.control('indicator.set',{enabled:e.target.checked},{led_enabled:e.target.checked}));

/* ---------- Home: power trend ---------- */
const powerHistory=[];
async function pollPower(){
 try{
  if($('homePage').hidden)return;
  const p=await api('/power');
  $('watts').firstChild.textContent=Number(p.power_watts||0).toFixed(1);
  $('energy').textContent=(p.energy_kwh<1?(p.energy_kwh*1000).toFixed(1)+' Wh':p.energy_kwh.toFixed(3)+' kWh');
  powerHistory.push(Number(p.power_watts)||0);if(powerHistory.length>60)powerHistory.shift();
  const values=powerHistory;
  const max=Math.max(...values,1),min=Math.min(...values),top=max*1.25;
  $('peak').textContent=max+' W';$('sparkRange').textContent=min+'–'+max+' W';
  const points=values.map((y,i)=>(i/Math.max(values.length-1,1)*300).toFixed(1)+','+(62-(y/top)*58).toFixed(1)).join(' ');
  $('spark').innerHTML='<polyline points="0,64 '+points+' 300,64" class="area"/><polyline points="'+points+'" class="line"/>';
 }catch{}
 finally{setTimeout(pollPower,2000);}
}

/* ---------- Settings: general ---------- */
const nameInputs=Array.from({length:6},(_,i)=>h('input',{maxLength:63,required:true}));
$('nameFields').append(...nameInputs.map((input,i)=>h('label',{class:'field'},h('span',{},t('tc1.socket_n',{n:i+1})),input)));
UI.onSubmit($('socketNamesForm'),async()=>{
 const s=UI.state;
 for(let i=0;i<6;i++)if(nameInputs[i].value!==s.socket_names[i])await api('/socketNames',{index:i,name:nameInputs[i].value});
},'common.saved');
UI.onSubmit($('reportForm'),()=>api('/mqtt/report/freq',{seconds:Number($('reportSeconds').value)}));
UI.onSubmit($('coefForm'),()=>api('/power/coefficient',String($('coef').value)));
let initialized=false;

UI.start({
 model:'TC1',kindKey:'kind.tc1',icon:'plug',mqttHostMax:127,factoryReset:false,
 otaName:'phicomm_tc1_a1.ota.bin.signed',haHintKey:'mqtt.ha_tc1',otaHintKey:'fw.hint_tc1',buttonHintKey:'sys.hint_tc1',
 scheduleLead:'sch.lead_tc1',interval:3000,hasButtons:true,
 groups:[['grp.network',['network','mqtt']],['grp.device',['device']],['grp.auto',['schedule','buttons']],['grp.system',['firmware','diagnostics','logs','maintenance']]],
 nav:{network:'nav.wifi',mqtt:'nav.mqtt',device:'nav.general',schedule:'nav.schedule',buttons:'nav.buttons',firmware:'nav.firmware',diagnostics:'nav.diagnostics',logs:'nav.logs',maintenance:'nav.maintenance'},
 pages:{},
 diagGroups:[['diag.g_sockets',['socket_names','socket_states','child_lock','led_enabled']],
  ['diag.g_power',['report_seconds','powerCoefficientX100']],
  ['diag.g_time',['time_synced','utc_time']],['diag.g_lan',['mask','gateway']]],
 healthChecks:s=>[['diag.c_clock',s.time_synced?'ok':'warn',t(s.time_synced?'s.synced':'s.syncing')],
  ['diag.c_memory',s.free_memory>16384?'ok':'warn',(Math.round((s.free_memory||0)/1024))+' KB']],
 chips:s=>s.time_synced?[]:[h('span',{class:'chip'},h('span',{class:'dot warn'}),h('span',{i18n:'chip.clock'}))],
 onStatus:s=>{
  renderSockets(s);
  if(!initialized){nameInputs.forEach((x,i)=>x.value=(s.socket_names||[])[i]||'');
   $('reportSeconds').value=s.report_seconds;$('coef').value=s.powerCoefficientX100;initialized=true;}
 }
});
I18N.onChange(()=>{
 nameInputs.forEach((x,i)=>{x.previousSibling.textContent=t('tc1.socket_n',{n:i+1});});
 tiles.forEach((x,i)=>{x.tile.nextSibling.setAttribute('aria-label',t('tc1.rename'));
  if(UI.state)x.name.textContent=(UI.state.socket_names||[])[i]||t('tc1.socket_n',{n:i+1});});
});
pollPower();
})();
