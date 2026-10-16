/* Automation pages shared by the three products: schedules, custom button
 * events and night mode. Installed by ui.js when the device reports the
 * matching capability. All traffic goes through command() from service.js. */
(()=>{
'use strict';
const {t}=I18N;
const {h,icon,toast,run,switchRow,seg,daysPicker,presetDays,daysText,hhmm,toMinutes,info,card,pageHead,slider,clone}=UI;

/* Action kinds a schedule or button event can run, keyed by action name. */
const ACTIONS={
 'outlet.set':'op.outlet','fan.set':'op.fan','light.set':'op.light',
 'child_lock.set':'op.child_lock','indicator.set':'op.indicator',
 'display.set':'op.display','system.reboot':'op.reboot'};
const actKey=a=>ACTIONS[a]||'op.none';
const sel=(parent,labelKey,options)=>{const s=h('select',{},options.map(([v,k])=>h('option',{value:v},typeof k==='function'?k():t(k))));parent.append(h('label',{class:'field'},h('span',{i18n:labelKey}),s));return s;};

/* Shared action editor: one control group per supported action. */
function actionEditor(host,{keys=false}={}){
 const caps=UI.caps||{},available=Object.keys(ACTIONS).filter(a=>(caps.actions||[]).includes(a));
 const box=h('div',{class:'stack'}),fields=h('div',{class:'stack'});
 const options=[...(keys?[['none','op.none']]:[]),...available.map(a=>[a,actKey(a)])];
 const pick=keys?sel(host,'btn.action',options):seg(options,v=>draw(v),{full:true});
 if(keys){pick.set=v=>{pick.value=v;};pick.addEventListener('change',()=>draw(pick.value));host.append(box);}
 else host.append(pick,box);
 box.append(fields);
 let read=()=>({}),current=keys?'none':(available[0]||'none');
 function draw(a,params={}){
  current=a;fields.replaceChildren();read=()=>({});
  if(a==='none'||!a)return;
  const parts=[];
  if(a==='outlet.set'){
   const index=sel(fields,'op.target',[['all','op.all'],...[0,1,2,3,4,5].map(i=>[String(i),()=>t('tc1.socket_n',{n:i+1})])]);
   if(params.index!=null)index.value=String(params.index);
   parts.push(()=>index.value==='all'?{}:{index:Number(index.value)});
  }
  const switchable=['outlet.set','fan.set','light.set','child_lock.set','indicator.set'].includes(a);
  if(switchable){
   const values=keys&&['outlet.set','fan.set'].includes(a)?['on','off','toggle']:['on','off'];
   const on=sel(fields,'op.state',values.map(v=>[v,'op.'+v]));
   on.value=params.toggle?'toggle':(params.on===false||params.enabled===false)?'off':'on';
   const field=['outlet.set','fan.set'].includes(a)?'on':'enabled';
   parts.push(()=>on.value==='toggle'?{toggle:true}:{[field]:on.value==='on'});
  }
  if(a==='fan.set'){
   const m={speed:params.speed||60},[out,range]=slider(m,'speed',{min:1});
   fields.append(h('div',{class:null},h('div',{class:'row between'},h('span',{class:'label',i18n:'op.speed'}),out)),range);
   parts.push(()=>({speed:Number(m.speed)}));
  }
  if(a==='light.set'||a==='display.set'){
   const step=a==='display.set'?25:1,m={brightness:params.brightness!=null?params.brightness:100};
   const [out,range]=slider(m,'brightness',{min:0,max:100,step,fmt:v=>v+'%'});
   fields.append(h('div',{class:null},h('div',{class:'row between'},h('span',{class:'label',i18n:'op.brightness'}),out)),range);
   parts.push(()=>({brightness:Number(m.brightness)}));
  }
  read=()=>Object.assign({},...parts.map(f=>f()));
  current=a;
 }
 draw(keys?'none':(available[0]||'none'));
 return {
  get(){return {action:current,params:read()};},
  set(a,p){current=a;pick.set(a);draw(a,p||{});}
 };
}

/* ---------- Schedules ---------- */
function schedulePage(dev){
 const box=h('div',{class:'stack'}),listBox=h('div',{class:'stack'});
 let offset=0,editing=0,tasks=[];
 const tz=()=>((UI.state&&UI.state.timezone_minutes)||480)*60;
 const atText=sec=>new Date((sec+tz())*1000).toISOString().slice(0,16).replace('T',' ');
 const when=x=>x.trigger==='once'?atText(x.at):hhmm(x.minute);
 const timeValue=()=>{const d=new Date(Date.now()+tz()*1000+86400000);return d.toISOString().slice(0,16);};

 const enabled=switchRow(null,'sch.enabled_row');
 const trigger=seg([['weekly','sch.weekly'],['once','day.once']],v=>syncTrigger(),{full:true});
 const days=daysPicker('sdays',127),time=h('input',{type:'time',value:'08:00'}),at=h('input',{type:'datetime-local',value:timeValue()});
 const weekly=h('div',{class:'field'},h('div',{class:'row between'},h('span',{class:'label',i18n:'sch.repeat'}),presetDays(days)),days);
 const once=h('label',{class:'field',hidden:true},h('span',{i18n:'sch.when'}),at);
 const minute=h('label',{class:'field'},h('span',{i18n:'sch.time'}),time);
 const editorHost=h('div',{class:'stack'});
 function syncTrigger(){const onceMode=trigger.value==='once';weekly.hidden=onceMode;once.hidden=!onceMode;minute.hidden=onceMode;}
 const editor=actionEditor(editorHost);
 const save=h('button',{type:'submit',class:'btn primary'});
 const labelSave=()=>save.replaceChildren(icon('plus'),h('span',{},t(editing?'sch.save_btn':'sch.add_btn')));
 labelSave();
 function row(x){
  return h('div',{class:'item'},h('b',{class:'time num'},when(x)),
   h('div',{class:'grow'},h('b',{},t(actKey(x.action))+(x.trigger==='weekly'?' · '+daysText(x.days):' · '+t('day.once'))),
    h('span',{},t('sch.state.'+(x.state||'waiting'))+(x.enabled?'':' · '+t('sch.off')))),
   h('span',{class:'badge '+(x.enabled?'ok':'')},t(x.enabled?'sch.enabled':'sch.disabled')),
   h('div',{class:'row',style:'gap:4px'},
    h('button',{type:'button',class:'btn ghost',onclick:()=>edit(x)},h('span',{i18n:'sch.edit'})),
    h('button',{type:'button',class:'btn ghost',onclick:ev=>run(ev.currentTarget,()=>command('schedules.enable',{id:x.id,enabled:!x.enabled}).then(load),null)},h('span',{i18n:x.enabled?'sch.disable':'sch.enable'})),
    h('button',{type:'button',class:'btn ghost icon','aria-label':t('common.delete'),onclick:ev=>run(ev.currentTarget,()=>command('schedules.delete',{id:x.id}).then(load),'common.deleted')},icon('trash'))));
 }
 async function load(){
  const d=await command('schedules.get',{offset,limit:8});tasks=d.items;
  listBox.replaceChildren(
   tasks.length?h('div',{class:'list'},tasks.map(row)):h('div',{class:'empty'},t('sch.empty')),
   h('div',{class:'row between'},h('span',{class:'help'},t('sch.used',{a:d.count,b:d.capacity})),
    h('div',{class:'row',style:'gap:4px'},
     h('button',{type:'button',class:'btn ghost',onclick:()=>run(null,()=>{offset=Math.max(0,offset-8);return load();},null)},h('span',{i18n:'common.prev'})),
     h('button',{type:'button',class:'btn ghost',onclick:()=>run(null,()=>{offset=d.next;return load();},null),disabled:offset+8>=d.count},h('span',{i18n:'common.next'})))));
  banner.hidden=!(UI.state&&UI.state.time_synced===false);
 }
 function reset(){editing=0;enabled.querySelector('input').checked=true;trigger.set('weekly');syncTrigger();days.setMask(127);time.value='08:00';editor.set('none');labelSave();}
 function edit(x){
  editing=x.id;enabled.querySelector('input').checked=!!x.enabled;trigger.set(x.trigger);syncTrigger();
  days.setMask(x.days);time.value=hhmm(x.minute);at.value=atText(x.at);editor.set(x.action,x.params);labelSave();
  box.scrollIntoView({block:'start'});
 }
 const banner=h('div',{class:'banner warn',hidden:true},icon('clock'),h('span',{i18n:'sch.no_clock'}));
 const form=h('form',{class:'subcard stack'},h('h3',{i18n:'sch.new'}),trigger,weekly,once,minute,editorHost,
  h('div',{class:'form-actions'},h('button',{class:'btn primary',type:'submit'},icon('plus'),h('span',{},t('sch.add_btn'))),
   h('button',{type:'button',class:'btn ghost',onclick:reset},h('span',{i18n:'sch.reset'}))));
 form.addEventListener('submit',e=>{e.preventDefault();run(save,async()=>{
  const a=editor.get();
  if(a.action==='none')throw Error(t('sch.pick_action'));
  const task={enabled:Number(enabled.querySelector('input').checked),trigger:trigger.value,...a};
  if(editing)task.id=editing;
  if(trigger.value==='weekly'){const d=days.mask();if(!d)throw Error(t('sch.pick_day'));task.days=d;task.minute=toMinutes(time.value);}
  else{const parsed=Date.parse(at.value);if(!Number.isFinite(parsed))throw Error(t('sch.pick_when'));
   task.at=Math.floor(parsed/1000)-tz();if(task.at<=Math.floor(Date.now()/1000))throw Error(t('sch.future'));}
  await command('schedules.put',task);reset();await load();toast(t('sch.added'));
 },null);});
 box.append(form,info('sch.hint'));
 return {el:h('div',{class:'stack'},pageHead('nav.schedule',dev.scheduleLead),banner,card('sch.list',null,listBox,box)),
  async show(){if(!UI.state)await UI.reload();await load();}};
}

/* ---------- Custom button events ---------- */
function buttonsPage(dev){
 const listBox=h('div',{class:'stack'}),keyHost=h('div',{class:'fields'});
 const key=sel(keyHost,'btn.key',dev.model==='A1'?[['0','btn.key_power'],['1','btn.key_wifi']]:[['0','btn.key_fn']]);
 const count=h('input',{type:'number',min:1,max:30,value:1,required:true});
 const countLabel=h('span',{},t('btn.click_count'));
 const kind=seg([['clicks','btn.click'],['seconds','btn.hold']],v=>{
  count.max=v==='seconds'?4:30;count.value=Math.min(Number(count.value)||1,Number(count.max));
  countLabel.textContent=t(v==='seconds'?'btn.hold_count':'btn.click_count');preview();
 },{full:true});kind.set('clicks');
 keyHost.append(h('div',{class:'field'},h('span',{i18n:'btn.kind'}),kind),h('label',{class:'field'},countLabel,count));
 const editorHost=h('div',{class:'stack'}),editor=actionEditor(editorHost,{keys:true});
 const keyName=k=>t(dev.model==='A1'?(Number(k)===0?'btn.key_power':'btn.key_wifi'):'btn.key_fn');
 const gesture=v=>v.seconds?t('btn.hold_full',{n:v.seconds}):t(v.clicks===1?'btn.single':'btn.click_full',{n:v.clicks});
 function actionText(v){
  if(v.action==='none')return t('op.none');
  const p=v.params||{},state=t(p.toggle?'op.toggle':p.on===false||p.enabled===false?'op.off':'op.on');
  const target=v.action==='outlet.set'?(p.index==null?t('op.all'):(UI.state?.socket_names?.[p.index]||t('tc1.socket_n',{n:p.index+1}))):t(actKey(v.action));
  if(v.action==='system.reboot')return target;
  let text=v.action==='display.set'?target:state+' · '+target;
  if(p.speed!=null)text+=' · '+p.speed+'%';
  if(p.brightness!=null)text+=' · '+p.brightness+'%';
  return text;
 }
 const previewBox=h('div',{class:'button-preview','aria-live':'polite'});
 function preview(){
  const v={key:Number(key.value),[kind.value]:Number(count.value),...editor.get()};
  previewBox.textContent=keyName(v.key)+' · '+gesture(v)+' → '+actionText(v);
 }
 const editorCard=h('form',{class:'card stack','data-button-editor':''});
 const selected=()=>({key:Number(key.value),[kind.value]:Number(count.value)});
 function edit(v){
  key.value=String(v.key);kind.set(v.seconds?'seconds':'clicks');count.max=v.seconds?4:30;
  countLabel.textContent=t(v.seconds?'btn.hold_count':'btn.click_count');count.value=v.seconds||v.clicks;
  editor.set(v.action,v.params);preview();editorCard.scrollIntoView({block:'nearest',behavior:'smooth'});count.focus();
 }
 function row(v){
  return h('div',{class:'button-mapping'},h('b',{class:'button-gesture'},gesture(v)),
   h('span',{class:'button-result'},actionText(v)),h('div',{class:'row'},
    h('button',{type:'button',class:'btn ghost',onclick:()=>edit(v)},t('sch.edit')),
    h('button',{type:'button',class:'btn ghost','aria-label':t('common.delete')+' '+gesture(v),onclick:e=>run(e.currentTarget,async()=>{
     await command('buttons.set',{key:v.key,...(v.seconds?{seconds:v.seconds}:{clicks:v.clicks}),action:'none',params:{}});await load();
    },'common.deleted')},t('common.delete'))));
 }
 async function load(){
  const items=[];let offset=0;
  do{const page=await command('buttons.get',{offset,limit:8});items.push(...page.items);if(page.next==null||page.next<=offset)break;offset=page.next;}while(offset<68);
  listBox.replaceChildren();
  for(const k of (dev.model==='A1'?[0,1]:[0])){
   const groups=[];
   for(const type of ['clicks','seconds']){
    const values=items.filter(v=>Number(v.key)===k&&v[type]>0).sort((a,b)=>a[type]-b[type]);
    const runs=[];
    for(const v of values){const last=runs[runs.length-1];
     if(last&&v[type]===last[last.length-1][type]+1&&JSON.stringify([v.action,v.params])===JSON.stringify([last[0].action,last[0].params]))last.push(v);
     else runs.push([v]);
    }
    if(runs.length)groups.push(h('section',{class:'button-group'},h('h3',{},t(type==='clicks'?'btn.click':'btn.hold')),runs.map(run=>run.length===1?row(run[0]):
     h('details',{class:'button-range'},h('summary',{},h('b',{},t(type==='clicks'?'btn.click_range':'btn.hold_range',{a:run[0][type],b:run[run.length-1][type]})),h('span',{},actionText(run[0])),h('span',{class:'help'},t('btn.expand'))),run.map(row)))));
   }
   listBox.append(h('section',{class:'stack'},h('h3',{},keyName(k)),groups.length?groups:h('div',{class:'empty'},t('btn.empty'))));
  }
 }
 const save=h('button',{type:'submit',class:'btn primary'},t('common.save'));
 editorCard.append(h('h2',{i18n:'btn.edit'}),keyHost,editorHost,previewBox,h('div',{class:'form-actions'},save));
 editorCard.addEventListener('input',preview);editorCard.addEventListener('change',preview);editorCard.addEventListener('click',()=>preview());
 editorCard.addEventListener('submit',e=>{e.preventDefault();if(!editorCard.reportValidity())return;
  run(save,async()=>{const a=editor.get();if(a.action==='none')throw Error(t('btn.pick_action'));
   await command('buttons.set',{...selected(),...a});await load();},'common.saved');
 });
 preview();
 return {el:h('div',{class:'stack'},pageHead('nav.buttons','btn.lead'),card('btn.current',null,listBox),
  card('btn.recovery',null,h('div',{class:'button-mapping'},h('b',{},t('btn.hold_full',{n:5})),h('span',{},t('btn.reset_wifi'))),
   h('div',{class:'button-mapping'},h('b',{},t('btn.hold_full',{n:10})),h('span',{},t('op.10'))),info('btn.reset_note')),editorCard),
  async show(){await load();preview();}};
}

/* ---------- Night mode ---------- */
function nightPage(dev){
 const isM1=dev.model==='M1',list=h('div',{class:'stack'});
 let model=[];
 function window(cfg,i){
  const days=daysPicker('ndays'+i,cfg.days),start=h('input',{type:'time',value:hhmm(cfg.start)}),end=h('input',{type:'time',value:hhmm(cfg.end)});
  const dim=switchRow(null,isM1?'night.screen':'night.light','night.light_desc');dim.querySelector('input').checked=!!cfg.light;
  const bright={brightness:Math.round(cfg.brightness*100/255)};
  const [bOut,bRange]=slider(bright,'brightness',{min:0,max:100,step:isM1?25:1,fmt:v=>v?v+'%':t('state.off')});
  const speed={max_speed:cfg.max_speed};
  const [fOut,fRange]=slider(speed,'max_speed',{min:1});
  const fan=switchRow(null,'night.fan','night.fan_desc'),block=switchRow(null,'night.block_t','night.block_desc');
  fan.querySelector('input').checked=!!cfg.fan;block.querySelector('input').checked=!!cfg.block_start;
  const extra=isM1?[]:[fan,h('div',{class:'field'},h('div',{class:'row between'},h('span',{class:'label',i18n:'night.fan_max'}),fOut)),fRange,block];
  const field=h('fieldset',{},h('legend',{},t('night.period',{n:i+1})),
   h('div',{class:'field'},h('div',{class:'row between'},h('span',{class:'label',i18n:'sch.repeat'}),presetDays(days)),days),
   h('div',{class:'fields'},h('label',{class:'field'},h('span',{i18n:'night.start'}),start),h('label',{class:'field'},h('span',{i18n:'night.end'}),end)),
   dim,h('div',{class:'field'},h('div',{class:'row between'},h('span',{class:'label',i18n:'night.light_max'}),bOut)),bRange,...extra,
   h('div',{class:'form-actions'},h('button',{type:'button',class:'btn ghost',onclick:()=>{model.splice(i,1);draw();}},icon('trash'),h('span',{i18n:'common.delete'}))));
  // One listener writes the whole window back, so no control can drift.
  const collect=()=>{
   cfg.days=days.mask();cfg.start=toMinutes(start.value);cfg.end=toMinutes(end.value);
   cfg.light=Number(dim.querySelector('input').checked);cfg.brightness=Math.round(bright.brightness*255/100);
   if(!isM1){cfg.fan=Number(fan.querySelector('input').checked);cfg.block_start=Number(block.querySelector('input').checked);cfg.max_speed=Number(speed.max_speed);}
  };
  field.addEventListener('input',collect);field.addEventListener('change',collect);
  return field;
 }
 function draw(){
  list.replaceChildren(...(model.length?model.map(window):[h('div',{class:'empty'},t('night.empty'))]));
 }
 const add=h('button',{type:'button',class:'btn'},icon('plus'),h('span',{i18n:'night.add_btn'}));
 add.onclick=()=>run(add,async()=>{if(model.length>=4)throw Error(t('common.max',{n:4}));
  model.push({days:127,fan:isM1?0:1,light:1,max_speed:30,brightness:0,block_start:1,start:1320,end:420});draw();},null);
 const save=h('button',{type:'button',class:'btn primary'},h('span',{i18n:'common.save'}));
 save.onclick=()=>run(save,async()=>{await command('night.set',{windows:model});toast(t('common.saved'));});
 return {el:h('div',{class:'stack'},pageHead('nav.sleep','night.lead'),card('night.list',null,list,
  info('night.hint'),h('div',{class:'form-actions'},add,save))),
  async show(){model=clone((await command('night.get')).windows||[]);draw();}};
}

window.automationPages=dev=>{
 const caps=(UI.caps&&UI.caps.actions)||[];
 const pages={};
 if(!caps.length||caps.includes('schedules.get'))pages.schedule=schedulePage(dev);
 if(caps.includes('buttons.set')&&dev.hasButtons)pages.buttons=buttonsPage(dev);
 if(caps.includes('night.get'))pages.night=nightPage(dev);
 return pages;
};
})();
