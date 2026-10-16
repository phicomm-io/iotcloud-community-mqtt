/* Entirely in-memory simulator. Never forwards requests to a real device. */
(()=>{
const create=model=>{
 const state={name:'演示设备',model,device_id:{tc1:'aabbccddee01',m1:'aabbccddee02',a1:'aabbccddee03'}[model],version:'演示',ip:'192.0.2.10',ssid:'Demo Wi-Fi',wifi_connected:true,mqtt_connected:true,mqtt_status:'ready',mqtt_host:'demo.invalid',mqtt_user:'demo',mqtt_port:1883,mqtt_topic_mode:'tmcp',mqtt_protocol:'tmcp',brightness_requested:3,brightness:192,red:80,green:200,blue:120,speed:60,percentage:60,applied_speed:60,requested_speed:60,fan_on:true,fan_ready:true,panel_ready:true,light_on:true,sensor_received:true,sensor_valid:true,age_ms:200,temperature:25,humidity:48,formaldehyde:.025,pm25:18,time_synced:true,timezone_minutes:480,ntp_host:'pool.ntp.org',ntp_status:'synced',ntp_synced:true,ota_progress:0,ota_state:'idle',socket_names:['台灯','显示器','音箱','充电器','风扇','备用'],socket_states:[true,true,false,true,false,false],report_seconds:5,powerCoefficientX100:1710,free_memory:48000,uptime_seconds:3600,power_watts:36,energy_kwh:.28,led_enabled:true};
 const actions=['status.get','capabilities.get','config.get','config.set','logs.get','ota.start','schedules.get','schedules.put','schedules.delete','schedules.enable','night.get','night.set','time.get','time.set','ota.sources.get','ota.check','ota.check.get','ota.install',...(model==='tc1'?['outlet.set','indicator.set','child_lock.set','buttons.get','buttons.set']:model==='m1'?['display.set','binding.get','binding.set','binding.status.get']:['fan.set','light.set','child_lock.set','buttons.get','buttons.set','binding.get','binding.set','binding.status.get','binding.probe','linkage.get','linkage.set','light_effect.get','light_effect.set'])];
 const fixtures={'night.get':{windows:[],revision:1},'buttons.get':{items:[]},'schedules.get':{items:[],capacity:8,count:0,next:0},'time.get':{timezone_minutes:480},'logs.get':{records:[{sequence:1,event:'demo_boot',level:'info',module:'system',uptime_ms:1000}],next:1,latest:1},'power.get':{power_watts:36,energy_kwh:.28,powers:[30,35,36]},'binding.get':{response_ids:[],peers:[],members:[],group:'',revision:1},'binding.status.get':{average:{valid:true,sample_count:1,configured_count:1,temperature:25,humidity:48,pm25:18,formaldehyde:.025},peers:[],members:[],linkage_status:'disabled',clock_valid:true,available:0,automatic_on:false,automatic_speed:0},'linkage.get':{enabled:0,mode:0,metrics:1,on_metrics:1,off_metrics:1,statistic:0,on_enabled:0,off_enabled:0,on_all:0,off_all:1,fixed_speed:60,min_speed:20,max_speed:100,debounce_seconds:15,min_switch_seconds:60,on_pm:50,off_pm:35,on_hcho:.1,off_hcho:.08,target_pm:35,target_hcho:.08,low_pm:35,high_pm:150,low_hcho:.08,high_hcho:.3,kp:20,ki:.02,kd:0,revision:1},'light_effect.get':{effect:0,segments:1,follow_fan:0,quality_pm:35,quality_hcho:.08,segment:[{source:0,display:2,minimum:-10,maximum:50,suitable_low:18,suitable_high:26,colors:[[0,0,255],[0,255,0],[255,0,0]]}],revision:1},'wifi.scan.get':{scanning:false,networks:[{ssid:'Demo Wi-Fi',rssi:-40,security:3}]},'ota.sources.get':{sources:['cn','global']},'ota.check.get':{state:'idle',available:false}};
 function command(action,p={}){
  if(action==='status.get')return structuredClone(state);
  if(action==='capabilities.get')return {model,actions};
  if(action==='config.get')return structuredClone(state);
  if(action==='config.set'){Object.assign(state,p.values||{});if(p.values?.names)state.socket_names=p.values.names;}
  if(action==='outlet.set'){state.socket_states[p.index]=p.toggle?!state.socket_states[p.index]:!!p.on;}
  if(action==='display.set')state.brightness_requested=Math.round(p.brightness/25);
  if(action==='fan.set'){if(p.toggle)state.fan_on=!state.fan_on;Object.assign(state,p);state.requested_speed=p.speed??p.percentage??state.requested_speed;state.speed=state.applied_speed=state.requested_speed;}
  if(action==='light.set'){Object.assign(state,p);if(p.toggle)state.light_on=!state.light_on;}
  if(action==='child_lock.set')state.child_lock=p.toggle?!state.child_lock:!!p.on;
  if(action==='indicator.set')state.led_enabled=p.toggle?!state.led_enabled:!!p.on;
  if(action==='schedules.put'){fixtures['schedules.get'].items.push({...p,id:Date.now()%100000});fixtures['schedules.get'].count=fixtures['schedules.get'].items.length;}
  if(action==='schedules.delete')fixtures['schedules.get'].items=fixtures['schedules.get'].items.filter(x=>x.id!==p.id);
  if(action==='schedules.enable'){const item=fixtures['schedules.get'].items.find(x=>x.id===p.id);if(item)item.enabled=p.enabled;}
  if(action==='buttons.set'){const list=fixtures['buttons.get'].items;const i=list.findIndex(x=>x.key===p.key&&x.clicks===p.clicks&&x.seconds===p.seconds);if(i>=0)list.splice(i,1);if(p.action!=='none')list.push({...p});}
  if(action.endsWith('.set')&&fixtures[action.replace(/\.set$/,'.get')])fixtures[action.replace(/\.set$/,'.get')]={...fixtures[action.replace(/\.set$/,'.get')],...p};
  if(action==='ota.check')fixtures['ota.check.get']={state:'up_to_date',version:'演示',source:p.source,available:false};
  if(action.startsWith('ota.')&&['ota.start','ota.install'].includes(action))state.ota_state='idle';
  return structuredClone(fixtures[action]||{});
 }
 return {state,command};
};
window.PhicommDemo={create};
const model='__MODEL__';if(!['tc1','m1','a1'].includes(model))return;
const demo=create(model);
window.fetch=async(url,options={})=>{
 if(url!=='/api/v1/command')throw Error('演示禁止真实网络请求');
 const req=JSON.parse(options.body);return {ok:true,status:200,json:async()=>({code:req.action.endsWith('.get')?'ok':'accepted',data:demo.command(req.action,req.params)})};
};
window.XMLHttpRequest=class{constructor(){this.upload={};}open(){}setRequestHeader(){}send(){this.status=202;this.upload.onprogress?.({lengthComputable:true,loaded:1,total:1});this.onload?.();this.onloadend?.();}};
document.addEventListener('DOMContentLoaded',()=>{const banner=document.createElement('div');banner.textContent='交互演示 · 模拟数据 · 不连接真实设备 · 刷新重置';banner.style='padding:8px;text-align:center;background:#c7ed70;color:#1b3028;font-size:12px';document.body.prepend(banner);});
})();
