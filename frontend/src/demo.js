import '../../../site/demo.js';
const devices=['tc1','m1','a1'].map((model,i)=>({model,mac:'aabbccddee0'+(i+1),version:'v2',username:'demo',mqtt_username:'demo',status:'active',name:model.toUpperCase()}));
window.fetch=async(url,options={})=>{
 let path=String(url).split('?')[0],data={};
 if(!path.startsWith('/api/'))throw Error('演示禁止真实网络请求');
 if(path==='/api/auth/me'||path==='/api/auth/login')data={username:'demo',mqtt_username:'demo',aliases:[],is_admin:false};
 else if(path==='/api/register/status')data={enabled:true,mqtt_ws_uri:'wss://demo.invalid/mqtt'};
 else if(path==='/api/devices'){if(options.method==='POST')devices.push(JSON.parse(options.body));data=[...devices];}
 else if(path.startsWith('/api/devices/')&&options.method==='DELETE'){const mac=path.split('/').pop();const i=devices.findIndex(d=>d.mac===mac);if(i>=0)devices.splice(i,1);}
 else if(path.startsWith('/api/analytics'))data={};
 return {ok:true,status:200,json:async()=>data};
};
const banner=document.createElement('div');banner.textContent='管理平台交互演示 · 模拟数据 · 不连接 MQTT · 刷新重置';banner.style='padding:8px;text-align:center;background:#c7ed70;color:#1b3028;font-size:12px';document.body.prepend(banner);
