/* A1 air purifier page. Product-specific pages: light effects, M1 binding and
 * automatic control. Shared pages come from ui.js and automation.js. */
(()=>{
'use strict';
const {t}=I18N;
const {$,h,icon,toast,run,onSubmit,confirm,switchRow,seg,summary,card,info,pageHead,saveBar,num,flag,slider,hhmm,toMinutes,normalizeMac,prettyMac,clone}=UI;

let advanced={effect:0,segments:3,segment:[]},bindingStatus={peers:[],members:[],linkage_status:'disabled'};
const refresh=()=>UI.reload();

/* ---------- Home: fan ---------- */
const power=$('power');power.append(icon('power'));
const speed=$('speed');let dragging=false;
const presets=seg([[20,()=>t('a1.p_quiet')+' 20%'],[40,()=>t('a1.p_low')+' 40%'],[60,()=>t('a1.p_mid')+' 60%'],[100,()=>t('a1.p_max')+' 100%']],v=>setSpeed(v),{full:true});
$('presets').replaceWith(presets);
function setSpeed(v){speed.value=v;$('speedValue').textContent=v+'%';return run(null,()=>api('/fan/config',{speed:Number(v)}).then(refresh),null);}
speed.addEventListener('input',()=>{dragging=true;$('speedValue').textContent=speed.value+'%';presets.set(Number(speed.value));});
speed.addEventListener('change',()=>{dragging=false;setSpeed(speed.value);});
power.onclick=()=>{const on=!UI.state.fan_on;power.setAttribute('aria-pressed',String(on));run(power,()=>UI.control('fan.set',{on},{fan_on:on}),null);};
for(const id of ['childLock','childLockDevice'])$(id).onchange=e=>run(null,()=>UI.control('child_lock.set',{enabled:e.target.checked},{child_lock:e.target.checked}),e.target.checked?'a1.lock_on':'a1.lock_off');
function renderFan(s){
 const locked=s.ota_active||s.reboot_pending||UI.controlBusy;
 power.setAttribute('aria-pressed',String(s.fan_on));power.disabled=locked;speed.disabled=locked;
 presets.querySelectorAll('button').forEach(b=>b.disabled=locked);
 const auto=bindingStatus.linkage_status==='active'&&bindingStatus.automatic_on;
 $('fanState').textContent=s.fan_on?t('a1.running'):t('a1.off');
 $('fanSub').textContent=locked?t('a1.locked_ota'):s.fan_on?t(auto?'a1.sub_auto':'a1.sub_manual',{p:s.percentage}):t('a1.sub_off',{p:s.requested_speed});
 const previewSpeed=$('previewSpeed');
 if(previewSpeed&&document.activeElement!==previewSpeed){const value=s.fan_on?s.percentage:s.requested_speed;previewSpeed.value=value;$('previewSpeedValue').textContent=value+'%';}
 if(!dragging&&document.activeElement!==speed){const v=s.fan_on?s.percentage:s.requested_speed;speed.value=v;$('speedValue').textContent=v+'%';presets.set(s.fan_on?s.percentage:null);}
 $('outBar').style.width=s.applied_speed+'%';$('targetBar').style.width=s.percentage+'%';$('targetBar').hidden=!s.percentage;
 $('outText').textContent=!s.fan_on&&!s.applied_speed?t('a1.out_stopped'):s.applied_speed!==s.percentage?t('a1.out_ramp',{a:s.applied_speed,p:s.percentage}):t('a1.out_steady',{a:s.applied_speed});
 for(const [id,badge] of [['panelBadge',$('panelBadge')],['panelBadge2',$('panelBadge2')]]){badge.className='badge '+(s.panel_ready?'ok':'bad');badge.textContent=t(s.panel_ready?'a1.panel_ok':'a1.panel_bad');}
 for(const id of ['childLock','childLockDevice'])$(id).checked=s.child_lock;
}

/* ---------- Home: automatic control summary ---------- */
function renderAuto(){
 const c=$('autoCard'),st=bindingStatus.linkage_status,peers=bindingStatus.peers||[];
 const head=h('div',{class:'card-head'},h('div',{},h('h2',{i18n:'a1.auto'}),h('p',{class:'help',i18n:'a1.auto_lead'})),
  h('span',{class:'badge '+(st==='active'?'ok':st==='sensor_failed'?'warn':'')},t('s.link_'+st)));
 if(!peers.length){c.replaceChildren(head,h('div',{class:'empty'},h('p',{},t('a1.auto_empty')),
  h('a',{class:'btn',href:'#settings/binding',style:'margin-top:12px'},icon('link'),h('span',{},t('a1.auto_link')))));return;}
 const fresh=peers.filter(p=>p.fresh).length;
 const meta=h('p',{class:'help'},t('a1.auto_peers',{n:peers.length,f:fresh})+' · '+peers.map(p=>String(p.transport||'mqtt').toUpperCase()).join('/'));
 const avg=bindingStatus.average||{};
 const readings=h('div',{class:'fields'},...[["temperature","°C"],["humidity","%"],["pm25","µg/m³"],["formaldehyde","mg/m³"]].map(([key,unit])=>h('div',{class:'field'},h('span',{},t('average.'+key)),h('b',{},avg.valid&&Number.isFinite(avg[key])?Number(avg[key]).toFixed(key==='formaldehyde'?3:1)+' '+unit:'—'))));
 if(st==='disabled'){c.replaceChildren(head,meta,readings,h('a',{class:'btn',href:'#settings/linkage',style:'margin-top:12px'},h('span',{},t('a1.auto_enable'))));return;}
 // replaceChildren() stringifies null, so build the list explicitly.
 const rows=[head,readings];
 if(st==='sensor_failed')rows.push(h('div',{class:'banner warn'},icon('alert'),h('span',{},t('a1.auto_failed'))));
 rows.push(h('div',{class:'row between'},meta,h('span',{class:'badge info'},t('a1.auto_speed',{p:bindingStatus.automatic_speed}))));
 c.replaceChildren(...rows);
}

/* ---------- Home: light ring ---------- */
const SWATCHES=[[217,80,0],[255,255,255],[255,170,60],[0,90,255],[0,200,120],[170,60,255],[255,30,60]];
let lightDirty=false,lightTimer=null,ringAnimationTimer=null,previewTarget=null,ringPolling=false,ringPolledAt=0,color=[0,0,255];
const hex=c=>'#'+c.map(n=>n.toString(16).padStart(2,'0')).join('');
const custom=h('input',{type:'color','aria-label':'custom'});
$('swatches').append(...SWATCHES.map(c=>h('button',{type:'button',class:'swatch',style:'--c:'+hex(c),'aria-label':hex(c),onclick:()=>{color=c;lightChanged();}})),custom);
custom.oninput=()=>{const v=custom.value;color=[1,3,5].map(i=>parseInt(v.slice(i,i+2),16));lightChanged();};
for(const id of ['ledEnabled','ledAlways'])$(id).onchange=lightChanged;
$('bright').oninput=lightChanged;
function lightChanged(){
 lightDirty=true;renderLightControls();if(UI.state)renderRing(UI.state);
 $('lightSave').textContent=t('common.saving');clearTimeout(lightTimer);
 // Debounced auto-save replaces the old "Apply" button.
 lightTimer=setTimeout(async()=>{try{
  await api('/light/config',{enabled:$('ledEnabled').checked,always_on:$('ledAlways').checked,brightness:Number($('bright').value),color:{r:color[0],g:color[1],b:color[2]}});
  $('lightSave').textContent=t('common.saved');lightDirty=false;}catch(e){$('lightSave').textContent='';toast(e.message,true);}},600);
}
function renderLightControls(){
 const on=$('ledEnabled').checked;
 $('brightValue').textContent=Math.round($('bright').value/255*100)+'%';
 document.querySelectorAll('.swatch').forEach((b,i)=>b.setAttribute('aria-pressed',String(hex(SWATCHES[i])===hex(color))));
 custom.value=hex(color);
 for(const el of [$('ledAlways'),$('bright'),custom,...document.querySelectorAll('.swatch')])el.disabled=!on;
}
function renderRing(s){
 if(!s)return;const N=31,svgs=document.querySelectorAll('.ring-preview-svg'),enabled=$('ledEnabled').checked,brightness=Number($('bright').value)/255;
 const touching=previewTarget!==null||!!s.touch_active,eff=touching?0:advanced.effect,now=performance.now(),speed=Number(s.percentage??s.speed??s.requested_speed??0),sources=Array.from({length:N},()=>[0,0,0]);
 const feedbackSpeed=Math.max(0,Math.min(100,Number(touching?(previewTarget??s.requested_speed??s.speed??0):(s.applied_speed??0))));
 const followSpeed=s.fan_on?speed:0;
 // Physical LEDs 1..31 occupy output slots 18..31,1..17; slot 0 is not on the ring.
 const slotForLed=index=>((index+17)%N)+1;
 const slotColors=Array.from({length:32},()=>[0,0,0]);
 const clip=v=>Math.max(0,Math.min(1,v));
 if(enabled&&brightness&&eff===0){
  for(let i=0;i<N;i++){
   const delta=Math.abs(i-9),distance=Math.min(delta,N-delta);
   const x=Math.max(0,Math.min(200,16*feedbackSpeed-100*(distance-1)));
   const level=!feedbackSpeed?0:!distance?255:Math.floor((255*x*x*(600-2*x)+4000000)/8000000);
   sources[i]=color.map(v=>Math.floor(Math.floor(v*brightness)*level/255)/255*(advanced.follow_fan&&!touching?followSpeed/100:1));
  }
 }else if(enabled&&brightness&&eff===4){
  const cfg=advanced,n=Math.max(1,Math.min(4,Number(cfg.segments)||1)),avg=bindingStatus.average||{};let at=0;
  for(let part=0;part<n;part++){
   const length=Math.floor(32/n)+(part<32%n?1:0),lit=length-(n>1?1:0),segment=cfg.segment?.[part];
   if(segment){
    let value=0,valid=true;
    if(segment.source<4){const key=['temperature','humidity','pm25','formaldehyde'][segment.source];value=Number(avg[key]??s[key]??0);valid=avg.valid===true||s.sensor_valid===true;}
    else if(segment.source===5)value=followSpeed;
    else{valid=avg.valid===true||s.sensor_valid===true;value=Math.max((Number(avg.pm25??s.pm25??0)/Math.max(Number(cfg.quality_pm)||1,0.01)),(Number(avg.formaldehyde??s.formaldehyde??0)/Math.max(Number(cfg.quality_hcho)||1,0.001)));}
    if(valid){
     let colorIndex=value<segment.suitable_low?0:value>segment.suitable_high?2:1;if(segment.display===0)colorIndex=1;
     const range=Number(segment.maximum)-Number(segment.minimum),level=segment.display===1?1:range>0?clip((value-Number(segment.minimum))/range):0;
     const levelBrightness=brightness*level*(cfg.follow_fan?followSpeed/100:1),rgb=(segment.colors?.[colorIndex]||[0,0,0]).map(v=>Number(v)*levelBrightness/255);
     for(let i=0;i<lit;i++)slotColors[at+i]=rgb;
    }
   }
   at+=length;
  }
  for(let i=0;i<N;i++)sources[i]=slotColors[slotForLed(i)];
 }else if(enabled&&brightness){
  let level=brightness*(advanced.follow_fan?followSpeed/100:1);
  if(eff===2)level*=((1+Math.sin(now/2000))/2);
  for(let i=0;i<N;i++){
   let rgb=color;
   if(eff===3){const p=(Math.floor(slotForLed(i)*768/32)+Math.floor(now/30))%768;rgb=[p<256?255-p:p>512?p-512:0,p<256?p:p<512?511-p:0,p<256?0:p<512?p-256:767-p];}
   sources[i]=rgb.map(v=>v/255*level);
  }
 }
 const step=Math.PI*2/N,base=Math.PI/2-9*step,radius=77,samples=24,parts=[
  '<circle class="ring-guide" cx="100" cy="100" r="77"></circle>',
  '<circle class="ring-edge" cx="100" cy="100" r="85"></circle>',
  '<circle class="ring-edge" cx="100" cy="100" r="69"></circle>',
  '<text class="ring-anchor" x="100" y="198" text-anchor="middle">10</text>'
 ];
 // Wendland C2 gives a smooth, compact light spread that reaches zero at two LED pitches.
 const falloff=d=>{const r=d/2;if(r>=1)return 0;const q=1-r;return q*q*q*q*(4*r+1);};
 const point=a=>[100+radius*Math.cos(a),100+radius*Math.sin(a)];
 for(let sample=0;sample<N*samples;sample++){
  const position=(sample+0.5)/samples,a0=base+sample/samples*step,a1=base+(sample+1)/samples*step,[x0,y0]=point(a0),[x1,y1]=point(a1),mix=[0,0,0];
  for(let i=0;i<N;i++){
   if(!sources[i].some(v=>v>0))continue;
   const raw=Math.abs(position-i),distance=Math.min(raw,N-raw),weight=falloff(distance);
   if(!weight)continue;
   for(let c=0;c<3;c++)mix[c]=Math.min(1,mix[c]+sources[i][c]*weight);
  }
  const strength=Math.max(...mix);
  if(strength>0.00001){
   const rgb=mix.map(v=>Math.round(v/strength*255));
   parts.push('<path class="ring-light-segment" d="M'+x0.toFixed(3)+' '+y0.toFixed(3)+' A'+radius+' '+radius+' 0 0 1 '+x1.toFixed(3)+' '+y1.toFixed(3)+'" stroke="rgb('+rgb.join(',')+')" stroke-opacity="'+strength.toFixed(4)+'"></path>');
  }
 }
 for(const svg of svgs){
  svg.dataset.feedbackSpeed=String(feedbackSpeed);svg.dataset.feedbackSource=touching?'target':'pwm';
  svg.setAttribute('role','img');svg.removeAttribute('aria-hidden');svg.setAttribute('aria-label','A1 圆环导光预览：31 颗光源均匀分布，10 号在 6 点，25 和 26 号分列 12 点两侧');
  svg.innerHTML=parts.join('')+'<text x="100" y="98" text-anchor="middle" class="ring-v">'+(enabled?(eff===0?feedbackSpeed:Math.round(brightness*100))+'%':t('state.off'))+'</text><text x="100" y="122" text-anchor="middle" class="ring-l">'+(eff===0?t(touching?'a1.feedback_target':'a1.feedback_pwm'):t('a1.effect.'+eff))+'</text>';
 }
}
function updateRingAnimation(){
 clearInterval(ringAnimationTimer);
 ringAnimationTimer=setInterval(()=>{
  if(!UI.state)return;
  const lightPage=document.querySelector('[data-settings-page="light"]'),ramping=UI.state.applied_speed!==UI.state.percentage;
  if(!document.hidden&&(ramping||lightPage&&!lightPage.hidden)&&!ringPolling&&performance.now()-ringPolledAt>=(ramping?200:500)){
   ringPolling=true;ringPolledAt=performance.now();UI.reload().finally(()=>{ringPolling=false;});
  }
  if(advanced.effect===2||advanced.effect===3||previewTarget!==null)renderRing(UI.state);
 },50);
}
function renderLight(s){
 if(!lightDirty){$('ledEnabled').checked=s.led_enabled;$('ledAlways').checked=s.led_always_on;$('bright').value=s.brightness;color=[s.red,s.green,s.blue];renderLightControls();}
 $('effectNote').replaceChildren(t('a1.effect_note',{e:t('a1.effect.'+advanced.effect)})+' ',h('a',{href:'#settings/light'},t('a1.effect_change')));
 renderRing(s);
}

/* ---------- Settings: light effects ---------- */
const EFFECTS=[0,1,2,3,4];
async function showLight(){
 const page=document.querySelector('[data-settings-page="light"]');
 const m=clone(advanced=await api('/light/advanced'));updateRingAnimation();
 const refreshDraft=()=>{advanced=clone(m);updateRingAnimation();if(UI.state)renderRing(UI.state);};
 page.oninput=refreshDraft;page.onchange=refreshDraft;
 const draw=()=>{
  const opts=h('div',{class:'options',role:'radiogroup'},EFFECTS.map(e=>h('label',{class:'option'},h('input',{type:'radio',name:'effect',value:e,checked:m.effect===e,onchange:()=>{m.effect=e;refreshDraft();draw();}}),
   h('b',{},t('a1.effect.'+e)),h('span',{},t('a1.effect_desc.'+e)))));
  const segs=m.effect===4?h('div',{class:'stack'},
   h('div',{class:'row between'},h('span',{class:'label',i18n:'fx.count'}),seg([1,2,3,4].map(n=>[n,String(n)]),n=>{m.segments=n;draw();})),
   ...m.segment.slice(0,m.segments).map((sg,i)=>h('div',{class:'subcard stack'},h('h3',{},t('fx.segment',{n:i+1})),
    h('div',{class:'fields'},
     h('label',{class:'field'},h('span',{i18n:'fx.source'}),h('select',{onchange:e=>sg.source=+e.target.value},[0,1,2,3,4,5].map(v=>h('option',{value:v,selected:sg.source===v},t('fx.src.'+v))))),
     h('label',{class:'field'},h('span',{i18n:'fx.display'}),h('select',{onchange:e=>sg.display=+e.target.value},[0,1,2].map(v=>h('option',{value:v,selected:sg.display===v},t('fx.disp.'+v)))))),
    h('div',{class:'sentence',style:'margin-top:12px'},t('fx.range'),num(sg,'minimum'),'–',num(sg,'maximum'),h('span',{class:'help'},t('fx.comfort')),num(sg,'suitable_low'),'–',num(sg,'suitable_high')),
    h('div',{class:'row',style:'margin-top:12px'},['fx.low','fx.ok','fx.high'].map((k,j)=>h('label',{class:'row small'},h('input',{type:'color',value:hex(sg.colors[j]),oninput:e=>{const v=e.target.value;sg.colors[j]=[1,3,5].map(x=>parseInt(v.slice(x,x+2),16));}}),t(k)))))))
   :null;
  if(segs)segs.querySelector('.seg').set(m.segments);
  const previewSpeed=h('input',{id:'previewSpeed',type:'range',min:1,max:100,step:1,value:UI.state?.fan_on?UI.state.percentage:UI.state?.requested_speed||60,'aria-label':t('a1.speed'),
   oninput:e=>{previewTarget=Number(e.target.value);$('previewSpeedValue').textContent=e.target.value+'%';renderRing(UI.state);},
   onchange:()=>run(previewSpeed,async()=>{try{await setSpeed(previewSpeed.value);}finally{previewTarget=null;renderRing(UI.state);}},null),
   onblur:()=>{previewTarget=null;if(UI.state)renderRing(UI.state);}});
  const previewRing=document.createElementNS('http://www.w3.org/2000/svg','svg');
  previewRing.setAttribute('class','ring ring-preview-svg');previewRing.setAttribute('viewBox','0 0 200 200');
  page.replaceChildren(pageHead('nav.light','fx.lead'),
   h('div',{class:'ring-effect-preview'},previewRing,
    h('div',{class:'ring-preview-controls'},h('div',{class:'row between'},h('span',{class:'label',i18n:'a1.speed'}),h('b',{class:'num',id:'previewSpeedValue'},previewSpeed.value+'%')),previewSpeed)),
   h('section',{class:'card'},h('div',{class:'card-head'},h('h2',{i18n:'fx.effect'})),opts,
    h('div',{style:'margin-top:16px'},flag(m,'follow_fan','fx.follow','fx.follow_desc')),segs,
    h('details',{class:'more'},h('summary',{i18n:'common.advanced'}),h('p',{class:'help',i18n:'fx.quality_hint'}),
     h('div',{class:'fields'},h('label',{class:'field'},h('span',{i18n:'fx.quality_pm'}),num(m,'quality_pm',{unit:'µg/m³',step:0.01})),
      h('label',{class:'field'},h('span',{i18n:'fx.quality_hcho'}),num(m,'quality_hcho',{unit:'mg/m³',step:0.001})))),
   saveBar(async()=>{await api('/light/advanced',m);advanced=clone(m);updateRingAnimation();if(UI.state)renderLight(UI.state);})));
  if(UI.state)renderRing(UI.state);
 };
 draw();
}

/* ---------- Settings: M1 binding ---------- */
async function showBinding(){
 bindingStatus=await api('/binding/status').catch(()=>bindingStatus);
 const m=await api('/binding/config');const page=document.querySelector('[data-settings-page="binding"]');
 const save=()=>api('/binding/config',m).then(showBinding);
 const st=mac=>(bindingStatus.peers||[]).find(p=>p.mac===mac)||{};
 const peerItems=m.peers.map((p,i)=>{const s=st(p.mac);
  const probe=h('button',{type:'button',class:'btn ghost icon',title:t('bind.probe'),'aria-label':t('bind.probe')},icon('auto'));
  probe.onclick=()=>run(probe,()=>api('/binding/probe',{mac:p.mac}),'bind.probe_sent');
  const del=h('button',{type:'button',class:'btn ghost icon','aria-label':t('common.delete')},icon('trash'));
  del.onclick=async()=>{if(await confirm({title:t('bind.remove_q'),body:t('bind.remove_body',{m:prettyMac(p.mac)}),ok:t('common.delete'),danger:true})){m.peers.splice(i,1);run(del,save);}};
  return h('div',{class:'item'},icon('air'),h('div',{class:'grow'},h('b',{class:'num'},'M1 · '+prettyMac(p.mac)),h('span',{},s.ip||p.ip||t('bind.ip_auto'))),
   h('span',{class:'badge'},String(s.transport||'mqtt').toUpperCase()),
   h('span',{class:'badge '+(s.pending?'warn':s.fresh?'ok':'bad')},t(s.pending?'bind.pending':s.fresh?'bind.fresh':'bind.stale')),probe,del);});
 const mac=h('input',{placeholder:'B0:F8:93:28:64:B6',maxLength:17,required:true,autocomplete:'off'});
 const ip=h('input',{placeholder:t('bind.ip_ph'),maxLength:15});
 const opts={force_mqtt:false,recovery:true,timeout_ms:1000,recovery_seconds:60};
 const add=h('form',{class:'subcard stack'},h('h3',{i18n:'bind.add'}),
  h('div',{class:'fields'},h('label',{class:'field'},h('span',{i18n:'bind.mac'}),mac),h('label',{class:'field'},h('span',{i18n:'bind.ip'}),ip)),
  h('details',{class:'more'},h('summary',{i18n:'common.advanced'}),flag(opts,'force_mqtt','bind.force','bind.force_desc'),flag(opts,'recovery','bind.recovery','bind.recovery_desc'),
   h('div',{class:'fields'},h('label',{class:'field'},h('span',{i18n:'bind.timeout'}),num(opts,'timeout_ms',{unit:'ms',min:100})),
    h('label',{class:'field'},h('span',{i18n:'bind.recovery_s'}),num(opts,'recovery_seconds',{unit:t('unit.s'),min:5})))),
  h('div',{class:'form-actions'},h('button',{class:'btn primary',type:'submit'},icon('plus'),h('span',{},t('bind.add_btn')))));
 onSubmit(add,()=>{const id=normalizeMac(mac.value);if(!id)throw Error(t('bind.bad_mac'));
  if(m.peers.some(p=>p.mac===id))throw Error(t('bind.dup'));if(m.peers.length>=8)throw Error(t('common.max',{n:8}));
  m.peers.push({mac:id,ip:ip.value.trim(),kind:1,confirmed:false,...opts});return save();},'bind.added');
 const ids=m.response_ids.length?h('div',{class:'banner info',style:'display:block'},h('b',{},t('bind.ids_title')),h('p',{class:'small',style:'margin:4px 0 8px'},t('bind.ids_body')),
  ...m.response_ids.map(id=>h('div',{class:'row'},h('code',{class:'grow num'},id),
   h('button',{type:'button',class:'btn ghost icon','aria-label':t('common.copy'),onclick:()=>{if(navigator.clipboard)navigator.clipboard.writeText(id);toast(t('common.copied'));}},icon('copy'))))):null;
 // Purifier group stays collapsed: it is only used with several A1 units.
 const members=h('div',{class:'list'},m.members.map((mb,i)=>h('div',{class:'item'},icon('fan'),
  h('div',{class:'grow'},h('b',{class:'num'},'A1 · '+prettyMac(mb.mac)),h('span',{},mb.ip||t('bind.ip_auto'))),
  h('button',{type:'button',class:'btn ghost icon','aria-label':t('common.delete'),onclick:()=>{m.members.splice(i,1);save();}},icon('trash')))));
 const gmac=h('input',{placeholder:'B0:F8:93:30:31:34',maxLength:17});
 const group=h('details',{class:'card more'},h('summary',{i18n:'bind.group'}),h('p',{class:'help',i18n:'bind.group_lead'}),
  h('label',{class:'field'},h('span',{i18n:'bind.group_name'}),h('input',{value:m.group,maxLength:32,placeholder:t('bind.group_ph'),oninput:e=>m.group=e.target.value})),
  members,h('div',{class:'row'},h('div',{class:'grow'},gmac),
   h('button',{type:'button',class:'btn',onclick:()=>{const id=normalizeMac(gmac.value);if(!id){toast(t('bind.bad_mac'),true);return;}m.members.push({mac:id,ip:''});save();}},icon('plus'),h('span',{},t('bind.member_add')))),
  flag(m,'group_force_mqtt','bind.force','bind.force_desc'),flag(m,'group_recovery','bind.recovery','bind.recovery_desc'),
  h('div',{class:'fields'},h('label',{class:'field'},h('span',{i18n:'bind.timeout'}),num(m,'group_timeout_ms',{unit:'ms'})),
   h('label',{class:'field'},h('span',{i18n:'bind.recovery_s'}),num(m,'group_recovery_seconds',{unit:t('unit.s')}))),
  saveBar(save));
 page.replaceChildren(pageHead('nav.binding','bind.lead'),
  h('section',{class:'card'},h('div',{class:'card-head'},h('h2',{i18n:'bind.sensors'}),h('span',{class:'badge'},m.peers.length+' / 8')),
   peerItems.length?h('div',{class:'list'},peerItems):h('div',{class:'empty'},t('bind.empty')),ids,add,info('bind.hint')),group);
}

/* ---------- Settings: automatic control rules ---------- */
async function showLinkage(){
 bindingStatus=await api('/binding/status').catch(()=>bindingStatus);
 const m=await api('/linkage/config');const page=document.querySelector('[data-settings-page="linkage"]');
 const has=(mask,bit)=>!!(mask&bit);
 const metricSeg=()=>{const s=seg([[1,'PM2.5'],[2,()=>t('m.formaldehyde')],[3,'link.both']],v=>{m.metrics=v;draw();});s.set(m.metrics);return s;};
 const draw=()=>{
  const modes=h('div',{class:'options',role:'radiogroup'},[0,1,2].map(v=>h('label',{class:'option'},h('input',{type:'radio',name:'mode',value:v,checked:m.mode===v,onchange:()=>{m.mode=v;draw();}}),
   h('b',{},t('link.mode.'+v)),h('span',{},t('link.mode_desc.'+v)))));
  let detail;
  if(m.mode===0){const [o,r]=slider(m,'fixed_speed',{min:1});detail=h('div',{},h('div',{class:'row between'},h('span',{class:'label',i18n:'link.fixed'}),o),r);}
  else if(m.mode===1)detail=h('div',{class:'stack'},
   has(m.metrics,1)&&h('div',{class:'sentence'},t('link.lin_pm_a'),num(m,'low_pm',{unit:'µg/m³',step:1}),t('link.lin_b'),num(m,'min_speed',{unit:'%',step:1}),t('link.lin_c'),num(m,'high_pm',{unit:'µg/m³',step:1}),t('link.lin_b'),num(m,'max_speed',{unit:'%',step:1})),
   has(m.metrics,2)&&h('div',{class:'sentence'},t('link.lin_hcho_a'),num(m,'low_hcho',{unit:'mg/m³',step:.001}),t('link.lin_b'),num(m,'min_speed',{unit:'%',step:1}),t('link.lin_c'),num(m,'high_hcho',{unit:'mg/m³',step:.001}),t('link.lin_b'),num(m,'max_speed',{unit:'%',step:1})));
  else detail=h('div',{class:'stack'},
   has(m.metrics,1)&&h('div',{class:'sentence'},t('link.pid_pm'),num(m,'target_pm',{unit:'µg/m³',step:1})),
   has(m.metrics,2)&&h('div',{class:'sentence'},t('link.pid_hcho'),num(m,'target_hcho',{unit:'mg/m³',step:.001})),
   h('div',{class:'sentence'},t('link.range'),num(m,'min_speed',{unit:'%',step:1}),'–',num(m,'max_speed',{unit:'%',step:1})),
   h('details',{class:'more'},h('summary',{i18n:'link.pid_tune'}),h('div',{class:'fields'},['kp','ki','kd'].map(k=>h('label',{class:'field'},h('span',{},k.toUpperCase()),num(m,k,{step:.01}))))));
  const rule=kind=>{
   const en=kind+'_enabled',mk=kind+'_metrics',all=kind+'_all',cmp=t(kind==='on'?'link.above':'link.below');
   const which=seg([[1,'PM2.5'],[2,()=>t('m.formaldehyde')],[3,'link.both']],v=>{m[mk]=v;draw();});which.set(m[mk]);
   const anyAll=seg([[0,'link.any'],[1,'link.all']],v=>{m[all]=v;});anyAll.set(m[all]);
   const warn=kind==='off'&&m.on_enabled&&(m.off_pm>=m.on_pm||m.off_hcho>=m.on_hcho)?h('p',{class:'help',style:'color:var(--warn)'},t('link.hyst_warn')):null;
   const box=h('div',{class:'subcard stack'},flag(m,en,'link.'+kind+'_rule','link.'+kind+'_desc'),
    h('div',{class:'row'},h('span',{class:'help',i18n:'link.watch'}),which,m[mk]===3?anyAll:null),
    has(m[mk],1)&&h('div',{class:'sentence'},'PM2.5 '+cmp,num(m,kind+'_pm',{unit:'µg/m³',step:1})),
    has(m[mk],2)&&h('div',{class:'sentence'},t('m.formaldehyde')+' '+cmp,num(m,kind+'_hcho',{unit:'mg/m³',step:.001})),warn);
   return box;};
  const master=flag(m,'enabled','link.enable','link.enable_desc');master.querySelector('input').addEventListener('change',draw);
  // replaceChildren() stringifies a null child, so assemble the list first.
  const rows=[pageHead('nav.linkage','link.lead')];
  if(!(bindingStatus.peers||[]).length)rows.push(h('div',{class:'banner warn'},icon('alert'),
   h('span',{},t('link.no_peer')+' '),h('a',{href:'#settings/binding'},t('a1.auto_link'))));
  rows.push(h('section',{class:'card'},master),
   h('fieldset',{class:'bare',disabled:!m.enabled},
    h('section',{class:'card stack'},h('h2',{i18n:'link.speed'}),h('div',{class:'row between'},h('span',{class:'label',i18n:'link.metric'}),metricSeg()),modes,detail),
    h('section',{class:'card stack',style:'margin-top:16px'},h('div',{},h('h2',{i18n:'link.onoff'}),h('p',{class:'help',i18n:'link.onoff_lead'})),rule('on'),rule('off'),
     h('details',{class:'more'},h('summary',{i18n:'common.advanced'}),
      h('div',{class:'fields'},
       h('label',{class:'field'},h('span',{i18n:'link.statistic'}),h('select',{onchange:e=>m.statistic=+e.target.value},[0,1,2,3,4,5].map(v=>h('option',{value:v,selected:m.statistic===v},t('link.stat.'+v))))),
       h('label',{class:'field'},h('span',{i18n:'link.debounce'}),num(m,'debounce_seconds',{unit:t('unit.s'),min:0})),
       h('label',{class:'field'},h('span',{i18n:'link.min_switch'}),num(m,'min_switch_seconds',{unit:t('unit.s'),min:0})))))),
   saveBar(()=>api('/linkage/config',m)));
  page.replaceChildren(...rows);
 };
 draw();
}

/* ---------- Boot ---------- */
const clock=UI.clockCard({
 load:()=>api('/time/config'),
 save:v=>api('/time/config',v),
 status:s=>[['clock.synced',t((bindingStatus&&bindingStatus.clock_valid)?'s.synced':'s.syncing'),(bindingStatus&&bindingStatus.clock_valid)?'ok':'warn'],
  ['clock.uptime',UI.duration(s.uptime_seconds||0)],['clock.memory',(Math.round((s.free_memory||0)/1024))+' KB']]});
$('clockCard').replaceWith(clock.el);

UI.start({
 model:'A1',kindKey:'kind.a1',icon:'fan',mqttHostMax:127,factoryReset:true,
 otaName:'phicomm_a1.ota.bin.signed',haHintKey:'mqtt.ha_a1',otaHintKey:'fw.hint_a1',buttonHintKey:'sys.hint_a1',
 scheduleLead:'sch.lead_a1',interval:2000,hasButtons:true,
 groups:[['grp.network',['network','mqtt']],['grp.device',['device','light']],['grp.auto',['binding','linkage','schedule','night']],['grp.system',['firmware','diagnostics','logs','maintenance']]],
 nav:{network:'nav.wifi',mqtt:'nav.mqtt',device:'nav.general',light:'nav.light',binding:'nav.binding',linkage:'nav.linkage',schedule:'nav.schedule',night:'nav.sleep',firmware:'nav.firmware',diagnostics:'nav.diagnostics',logs:'nav.logs',maintenance:'nav.maintenance'},
 pages:{device:{show:clock.show,status:clock.status},light:{show:showLight},binding:{show:showBinding},linkage:{show:showLinkage}},
 diagGroups:[['diag.g_fan',['fan_on','requested_fan_on','requested_speed','speed','percentage','applied_speed','fan_ready','child_lock','panel_ready','panel_errors','hardware_errors','panel_raw']],
  ['diag.g_light',['led_enabled','led_visible','led_always_on','brightness','red','green','blue','config_dirty','save_error']],
  ['diag.g_ota_img',['ota_signature_verified','ota_image_verified','ota_key_id','ota_image_length','ota_release_counter','ota_version']]],
 healthChecks:s=>[['diag.c_panel',s.panel_ready?'ok':'bad',s.panel_ready?t('state.ok'):t('diag.errors',{n:s.panel_errors})],
  ['diag.c_fan',s.fan_ready&&!s.hardware_errors?'ok':'bad',s.hardware_errors?t('diag.errors',{n:s.hardware_errors}):t('state.ok')],
  ['diag.c_storage',s.save_error?'bad':'ok',s.save_error||t('state.ok')]],
 chips:s=>s.child_lock?[h('span',{class:'chip'},icon('lock'),h('b',{},t('a1.child_lock')))]:[],
 onStatus:s=>{renderFan(s);renderLight(s);}
});
I18N.onChange(()=>{if(UI.state){renderAuto();for(const key of ['device','light','binding','linkage'])if(!document.querySelector('[data-settings-page="'+key+'"]').hidden&&UI.pages[key].show)UI.pages[key].show();}});
// Keep the automatic-control summary fresh even while the home page is idle.
(async function poll(){try{bindingStatus=await api('/binding/status');if(UI.state)renderFan(UI.state);renderAuto();}catch{}setTimeout(poll,3000);})();
api('/light/advanced').then(a=>{advanced=a;updateRingAnimation();if(UI.state)renderLight(UI.state);}).catch(e=>toast(e.message,true));
})();
