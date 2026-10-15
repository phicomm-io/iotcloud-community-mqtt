<script setup>
import {ref,reactive,computed,onMounted,onBeforeUnmount,watch,nextTick} from 'vue';
import {useRoute,useRouter} from 'vue-router';
import mqtt from 'mqtt';
import {account,api,restoreAccount} from './account';
import {track} from './analytics';
import {autoConnect,setAutoConnect,rememberLogin,savedPassword,forgetLogin} from './connectionPreferences';
import {userKey,requestId,deviceKey,deviceEntry,localDevices,topics,applyMessage,command} from './deviceProtocol';
import {deviceType} from './devicePresentation';
import DeviceSummary from './components/DeviceSummary.vue';
import DeviceNameDialog from './components/DeviceNameDialog.vue';
import AppIcon from './components/AppIcon.vue';
import DeviceHome from './components/DeviceHome.vue';
import PlatformSettings from './components/PlatformSettings.vue';
import ThemeToggle from './components/ThemeToggle.vue';

const route=useRoute(),router=useRouter();
const home=ref(null),nameDialog=ref(null),aliases=ref({});
function displayName(d){const name=aliases.value[deviceKey(d)]||states[deviceKey(d)]?.name||d.name;if(name)return name;const peers=list.value.filter(x=>x.model===d.model);return deviceType(d)+(peers.length>1?' '+(peers.findIndex(x=>deviceKey(x)===deviceKey(d))+1):'');}
function renameDevice(d,name,done){
 if(d.version==='v1'){aliases.value[deviceKey(d)]=name;localStorage.setItem('phicomm.names.'+(account.username||'guest'),JSON.stringify(aliases.value));done();return;}
 deviceCommand({action:'config.set',params:{section:'device',values:{name}}},reply=>{if(!['ok','accepted'].includes(reply.code)){done(reply.message||'保存失败');return;}const state=states[deviceKey(d)]||={};state.name=name;done();},d);
}
const page=ref('home'),thirdParty=ref(route.name==='Guest'),list=ref([]),selected=ref(''),states=reactive({}),connected=ref(false),error=ref(''),notice=ref(''),busy=ref(false),now=ref(Date.now());
const connection=reactive({host:`${location.protocol==='https:'?'wss':'ws'}://${location.host}/mqtt`,username:'',password:''});
const addForm=reactive({mac:'',model:'tc1',version:'v2'});
const active=computed(()=>{const d=list.value.find(x=>deviceKey(x)===selected.value);return d?{...states[deviceKey(d)],...d}:null;});

watch(selected,async()=>{
 await nextTick();
 if(!matchMedia('(max-width:800px)').matches)return;
 const item=document.querySelector('.device-select[aria-pressed=true]'),bar=item?.closest('.device-list');
 if(bar)bar.scrollLeft+=item.getBoundingClientRect().left-bar.getBoundingClientRect().left;
});
watch(page,value=>track('pageview',thirdParty.value?(value==='settings'?'/guest/settings':'/guest'):(value==='settings'?'/settings':'/')));
watch(()=>active.value?deviceKey(active.value):'',value=>{if(value)track('device_view',thirdParty.value?'/guest':'/',active.value.model);});
let client,timer,poll;
const replies=new Map();
function deviceCommand(request,reply,target=active.value){
 if(!connected.value||!target){reply({code:'offline',message:'请先连接 MQTT'});return;}
 if(target.version==='v1'){legacyRequest(request,reply);return;}
 const signature=JSON.stringify([deviceKey(target),request.action,request.params||{}]);
 if(request.action.endsWith('.get')){const existing=[...replies.values()].find(p=>p.signature===signature);if(existing){const previous=existing.reply;existing.reply=value=>{previous(value);reply(value);};return;}}
 const id=requestId(),d=target;
 const state=states[deviceKey(d)]||={};(state.pending||={})[id]=request.action;
 const timeout=setTimeout(()=>{const pending=replies.get(id);replies.delete(id);delete state.pending[id];pending?.reply({code:'timeout',message:'暂未收到操作回执，请等待状态更新'});},20000);
 replies.set(id,{reply,timeout,key:deviceKey(d),signature});
 const c=command(d,{...request,request_id:id});
 client.publish(c.topic,c.payload,{qos:1,retain:false},e=>{if(e){clearTimeout(timeout);const pending=replies.get(id);replies.delete(id);delete state.pending[id];pending?.reply({code:'error',message:'发送失败'});}});
}
const connectionOwner=()=>thirdParty.value?'guest':account.username;
function manualDisconnect(){setAutoConnect(connectionOwner(),false);disconnect();}
function manualConnect(){setAutoConnect(connectionOwner(),true);connect();}
function disconnect(){busy.value=false;for(const {reply,timeout} of replies.values()){clearTimeout(timeout);reply({code:'offline',message:'MQTT 已断开'});}replies.clear();client?.end(true);client=null;connected.value=false;Object.keys(states).forEach(k=>delete states[k]);}
function subscribe(){if(thirdParty.value)client?.subscribe(`phicomm/v2/${connection.username?userKey(connection.username):'local'}/discovery/+`,{qos:1});for(const d of list.value.filter(d=>!d.status||d.status==='active'))client?.subscribe(topics(d),{qos:1},(e,g)=>{if(e||g?.some(x=>x.qos===128))error.value='订阅被拒绝，请检查账号与设备归属';});}
async function refresh(){try{list.value=(thirdParty.value?localDevices().map(d=>({...d,username:connection.username})):await api('/devices')).map(deviceEntry);if(!list.value.some(d=>deviceKey(d)===selected.value))selected.value=list.value[0]?deviceKey(list.value[0]):'';if(connected.value)subscribe();}catch(e){error.value=e.message;}}
function connect(){if(!thirdParty.value&&connection.password)rememberLogin(account.username,connection.password);disconnect();error.value='';if(!/^wss?:\/\//.test(connection.host)){error.value='请输入 ws:// 或 wss:// 地址';return;}busy.value=true;
 client=mqtt.connect(connection.host,{username:connection.username,password:connection.password,clientId:'web_'+requestId(),connectTimeout:5000,reconnectPeriod:3000,clean:true});
 client.on('connect',()=>{connected.value=true;busy.value=false;subscribe();track('mqtt_connect',thirdParty.value?'/guest':'/');});
 client.on('close',()=>{connected.value=false;busy.value=false;});client.on('error',()=>{error.value='MQTT 连接失败，请检查地址、凭据和权限';busy.value=false;});
 client.on('message',(topic,payload)=>{if(thirdParty.value && topic.startsWith(`phicomm/v2/${connection.username?userKey(connection.username):'local'}/discovery/`)){try{const a=JSON.parse(payload.toString());if(a.protocol!=='tmcp'||a.protocol_version!==2||a.version!=='v2')return;const d=deviceEntry({mac:topic.split('/')[4],model:a.model,version:'v2',username:connection.username});if(!list.value.some(x=>deviceKey(x)===deviceKey(d))){list.value.push(d);localStorage.setItem('phicomm_devices',JSON.stringify(list.value));if(!selected.value)selected.value=deviceKey(d);subscribe();sendTo(d,'status.get');}}catch{}return;}for(const d of list.value){const s=states[deviceKey(d)] ||= {};if(applyMessage(d,s,topic,payload.toString())){if(topic.endsWith('/result')){const pending=replies.get(s.result?.request_id);if(pending&&pending.key===deviceKey(d)){clearTimeout(pending.timeout);replies.delete(s.result.request_id);pending.reply(s.result);}delete s.pending?.[s.result?.request_id];}break;}}});
 if(thirdParty.value)localStorage.setItem('tmcp_broker',JSON.stringify({host:connection.host,username:connection.username}));
}
function legacyRequest({action,params={}},reply){
 const d=active.value;
 const ok=data=>reply({code:'ok',data});
 const fail=message=>reply({code:'unsupported',message});
 if(action==='capabilities.get'){ok({actions:['outlet.set','child_lock.set','indicator.set']});return;}
 if(action==='status.get'){
  if(!Array.from({length:6},(_,i)=>d[`plug_${i}`]).every(p=>p?.on!==undefined)){fail('尚未收到设备状态');return;}
  ok({name:d.name||'TC1',socket_states:Array.from({length:6},(_,i)=>!!d[`plug_${i}`]?.on),socket_names:Array.from({length:6},(_,i)=>d[`plug_${i}`]?.setting?.name||`插座 ${i+1}`),child_lock:!!(d.child_lock?.on??d.child_lock),led_enabled:!!(d.led_lock?.on??d.led_lock),mqtt_connected:true,mqtt_host:connection.host});return;
 }
 if(action==='power.get'){
  if(d.power_watts===undefined&&d.power===undefined){fail('尚未收到电量数据');return;}
  ok({power_watts:d.power_watts??Number(d.power),energy_kwh:d.energy_kwh??Number(d.powerConsumption)});return;
 }
 let values;
 if(action==='outlet.set'){
  const indexes=params.index===undefined?[0,1,2,3,4,5]:[params.index];
  if(indexes.some(i=>!Number.isInteger(i)||i<0||i>5)){fail('插座编号无效');return;}
  values=Object.fromEntries(indexes.map(i=>[`plug_${i}`,{on:params.on?1:0}]));
 }else if(action==='child_lock.set')values={child_lock:{on:params.enabled?1:0}};
 else if(action==='indicator.set')values={led_lock:{on:params.enabled?1:0}};
 else if(action==='config.set'&&params.section==='socket'&&!d.legacyText)values={[`plug_${params.values.index}`]:{setting:{name:params.values.name}}};
 else {fail('此 Legacy 设备不支持该操作');return;}
 try{
  // Text protocol accepts one outlet per publication.
  for(const [key,value] of Object.entries(values)){
   const c=command(d,{[key]:value});client.publish(c.topic,c.payload,{qos:1,retain:false},e=>{if(e)error.value='发送失败';});
  }
  reply({code:'accepted',data:{}});
 }catch(e){fail(e.message);}
}
function sendTo(d,action,params={}){if(!connected.value){error.value='请先连接 MQTT';return;}const id=requestId(),s=states[deviceKey(d)] ||= {}; (s.pending ||= {})[id]=action;const c=command(d,{action,params,request_id:id});client.publish(c.topic,c.payload,{qos:1,retain:false},e=>{if(e)error.value='发送失败';});}
async function changeMode(){disconnect();notice.value='';error.value='';if(thirdParty.value){Object.assign(connection,{host:`${location.protocol==='https:'?'wss':'ws'}://${location.host}/mqtt`,username:'',password:''},JSON.parse(localStorage.getItem('tmcp_broker')||'{}'));}else{const s=await api('/register/status');Object.assign(connection,{host:s.mqtt_ws_uri||`${location.protocol==='https:'?'wss':'ws'}://${location.host}/mqtt`,username:account.mqttUsername||account.username,password:account.password||savedPassword(account.username)});}await refresh();if(autoConnect(connectionOwner())&&connection.password)connect();}
async function add(){try{const d=deviceEntry({...addForm,version:addForm.model==='tc1'?addForm.version:'v2',...(thirdParty.value?{username:connection.username}:{})});if(thirdParty.value){if(list.value.some(x=>x.mac===d.mac))throw Error('设备已存在');localStorage.setItem('phicomm_devices',JSON.stringify([...list.value,d]));}else await api('/devices',{method:'POST',body:JSON.stringify(d)});await refresh();addForm.mac='';}catch(e){error.value=e.message;}}
async function remove(d){if(!confirm(`移除 ${d.mac}？`))return;try{client?.unsubscribe(topics(d));if(thirdParty.value)localStorage.setItem('phicomm_devices',JSON.stringify(list.value.filter(x=>deviceKey(x)!==deviceKey(d))));else await api(`/devices/${d.namespace?'scoped/':''}${d.mac}${d.namespace?'?mqtt_username='+encodeURIComponent(d.mqtt_username||d.username):''}`,{method:'DELETE'});await refresh();}catch(e){error.value=e.message;}}
async function logout(){try{await api('/auth/logout',{method:'POST',body:'{}'});}catch(e){error.value=e.message;}account.username=null;account.mqttUsername=null;account.isAdmin=false;account.aliases=[];account.password='';forgetLogin();disconnect();await router.replace('/login');}
async function changePassword(values){try{await api('/auth/password',{method:'POST',body:JSON.stringify({current_password:values.current,password:values.password})});await logout();}catch(e){error.value=e.message;}}
onMounted(async()=>{if(route.name!=='Guest')await restoreAccount();try{aliases.value=JSON.parse(localStorage.getItem('phicomm.names.'+(account.username||'guest'))||'{}');}catch{}await changeMode();timer=setInterval(()=>now.value=Date.now(),1000);poll=setInterval(()=>{if(!thirdParty.value&&!document.hidden)refresh();},5000);});
onBeforeUnmount(()=>{disconnect();clearInterval(timer);clearInterval(poll);});
</script>
<template>
<main class="console-page">
<header class="console-topbar topbar"><div class="brand"><span class="kind">MQTT 管理平台</span><h1>Phicomm</h1></div><nav class="console-nav"><span class="status-pill" :class="{online:connected}">{{connected?'MQTT 已连接':'MQTT 未连接'}}</span><RouterLink v-if="account.isAdmin" class="btn" to="/admin">平台管理</RouterLink><ThemeToggle/><button v-if="account.username" @click="logout">退出</button><RouterLink v-else to="/login">登录</RouterLink></nav></header>
<nav class="platform-tabs tabs" aria-label="主导航"><button :aria-current="page==='home'?'page':undefined" @click="page='home'">首页</button><button :aria-current="page==='settings'?'page':undefined" @click="page='settings'">设置</button></nav>
<p v-if="error" role="alert" class="feedback error">{{error}}</p><p v-if="notice" role="status" class="feedback">{{notice}}</p>
<template v-if="page==='home'">
<div class="dashboard-grid"><aside class="surface devices-panel"><h2>设备</h2><ul class="device-list"><li v-for="d in list" :key="deviceKey(d)" class="device-list-item"><button class="device-select" :aria-pressed="selected===deviceKey(d)" @click="selected=deviceKey(d)"><DeviceSummary :device="d" :state="states[deviceKey(d)]||{}" :name="displayName(d)" :connected="connected" :now="now"/></button><button class="device-rename btn ghost icon" :aria-label="`修改 ${displayName(d)} 的名称`" @click="nameDialog.open(d,displayName(d))"><AppIcon name="edit" :size="15"/></button></li></ul><button @click="refresh">刷新列表</button><p v-if="!list.length" class="helper-text">暂无设备。设备上线后将自动显示，也可在设置中添加。</p></aside>
<div v-if="active" class="device-area"><div class="device-heading"><div><h2>{{displayName(active)}}</h2><p class="help">{{deviceType(active)}}</p></div><span class="badge" :class="active.available===true&&connected?'ok':''">{{!connected?'MQTT 未连接':active.available===false?'离线':active.available===true?'在线':'等待设备状态'}}</span><button :disabled="!connected" @click="home?.refresh()">刷新状态</button></div>
<DeviceHome ref="home" :key="selected" :device="active" @command="deviceCommand" @settings="page='settings'"/>
</div><section v-else class="surface empty-control"><h2>从一台设备开始</h2><p>在设置中连接 Broker 并添加设备。</p><button @click="page='settings'">打开设置</button></section></div>
</template>
<template v-else>
<PlatformSettings :connection="connection" :connected="connected" :busy="busy" :third-party="thirdParty" :devices="list" :states="states" :add-form="addForm" :device-name="displayName" :now="now" @rename="nameDialog.open($event,displayName($event))" @mode-change="thirdParty=$event;changeMode()" @connect="manualConnect" @disconnect="manualDisconnect" @add="add" @remove="remove" @password="changePassword" @home="page='home'" @refresh="refresh"/>
</template>
<DeviceNameDialog ref="nameDialog" @save="renameDevice"/>
<footer class="console-footer">Phicomm</footer></main>
</template>
