async function command(action,params={}){
 const request={action,params};
 if(!action.endsWith('.get'))request.request_id=globalThis.crypto?.randomUUID?crypto.randomUUID():Date.now().toString(36)+'-'+Math.random().toString(36).slice(2);
 const controller=new AbortController(),timer=setTimeout(()=>controller.abort(),20000);
 try{
 const response=await fetch('/api/v1/command',{method:'POST',signal:controller.signal,headers:{'Content-Type':'application/json'},body:JSON.stringify(request),cache:'no-store'});
 const reply=await response.json();
 if(!response.ok||!['ok','accepted'].includes(reply.code)){const error=Error(reply.message||reply.code||('HTTP '+response.status));error.code=reply.code;throw error;}
 return reply.data||{};
 }finally{clearTimeout(timer);}
}
async function api(path,data){
 if(path==='/status')return command('status.get');
 if(path==='/sensors/average')return command('sensors.average.get');
 if(path==='/power')return command('power.get');
 if(path==='/wifi/scan')return command(data===undefined?'wifi.scan.get':'wifi.scan');
 const domains={'/binding/config':'binding','/binding/status':'binding.status','/binding/probe':'binding.probe','/linkage/config':'linkage','/light/advanced':'light_effect','/sleep/config':'night','/time/config':'time'};
 if(domains[path])return command(domains[path]+(path==='/binding/probe'?'':data===undefined?'.get':'.set'),data||{});
 const direct={'/reboot':'system.reboot','/device/reset':'system.reset','/ota':'ota.start','/childLock':'child_lock.set','/child_lock':'child_lock.set','/led':'indicator.set','/outlet':'outlet.set','/fan':'fan.set','/fan/config':'fan.set','/light':'light.set','/light/config':'light.set'};
 if(direct[path]){if(['/light','/light/config'].includes(path)&&data.brightness!==undefined)data={...data,brightness:Math.round(data.brightness*100/255)};return command(direct[path],data);}
 if(path==='/display/brightness')return command('display.set',{brightness:data.level*25});
 if(path==='/socket'){for(let index=0;index<data.states.length;index++)await command('outlet.set',{index,on:!!data.states[index]});return {};}
 const sections={'/wifi/config':'wifi','/mqtt/config':'mqtt','/deviceName':'device','/auth/password':'auth','/socketNames':'socket','/mqtt/report/freq':'report','/power/coefficient':'meter','/ntp/config':'ntp'};
 if(sections[path]){
  let values={...data};
  if(path==='/power/coefficient')values={coefficient_x100:Number(data)};
  if(path==='/mqtt/config'){delete values.topic_mode;delete values.clear_password;if(data.clear_password)values.password='';else if(!values.password)delete values.password;}
  if(path==='/wifi/config'){delete values.open_network;if(data.open_network)values.password='';else if(!values.password)delete values.password;}
  return command('config.set',{section:sections[path],values});
 }
 throw Error('不支持的操作');
}
