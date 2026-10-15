import test from 'node:test';
import assert from 'node:assert/strict';
import {deviceEntry,userKey,baseTopic,topics,command,applyMessage,normalizeMAC} from './deviceProtocol.js';
test('current TMCP roots and request envelope for all models',()=>{for(const model of ['tc1','m1','a1']){const d=deviceEntry({mac:'AA:BB:CC:DD:EE:FF',model,username:'alice',version:'v2'});const base=`phicomm/v2/${userKey('alice')}/${model}/aabbccddeeff`;assert.equal(baseTopic(d),base);assert.deepEqual(topics(d),[base+'/#']);const c=command(d,{action:'status.get',request_id:'test-1'});assert.equal(c.topic,base+'/command');assert.deepEqual(JSON.parse(c.payload),{request_id:'test-1',action:'status.get',params:{}});const state={};assert(applyMessage(d,state,base+'/state',JSON.stringify({name:'demo',sensor_valid:true,temperature:22}),1000));assert.equal(state.temperature,22);assert.equal(state.sensorAt,1000);assert.equal(applyMessage(d,state,base.replace(userKey('alice'),userKey('bob'))+'/state','{}'),false);assert.equal(applyMessage(d,state,`phicomm/${model}/aabbccddeeff/state`,'{}'),false);}});
test('Legacy TC1 identities and original JSON control remain supported',()=>{const d=deviceEntry({mac:'AABBCCDDEEFF',version:'v1'});assert(topics(d).includes('device/ztc1/AA:BB:CC:DD:EE:FF/state'));const c=command(d,{plug_0:{on:1}});assert.equal(c.topic,'device/ztc1/aabbccddeeff/set');assert.equal(JSON.parse(c.payload).plug_0.on,1);const state={};assert(applyMessage(d,state,'device/ztc1/AABBCCDDEEFF/state','{"plug_0":{"on":1}}'));assert.equal(state.plug_0.on,1);assert(!applyMessage(d,state,'device/ztc1/001122334455/state','{}'));});
test('obsolete project versions and mixed MAC syntax rejected',()=>{assert.throws(()=>deviceEntry({model:'m1',mac:'aabbccddeeff',version:'community'}));assert.throws(()=>deviceEntry({model:'a1',mac:'aabbccddeeff',version:'v1'}));assert.throws(()=>normalizeMAC('AA:BB-CC:DD:EE:FF'));});
test('result query merges only matching status receipts',()=>{const d=deviceEntry({mac:'aabbccddeeff',model:'m1'}),state={pending:{q:'status.get'}};applyMessage(d,state,baseTopic(d)+'/result',JSON.stringify({request_id:'q',code:'ok',data:{sensor_valid:false,temperature:1}}),10);assert.equal(state.temperature,1);assert.equal(state.sensorFresh,false);applyMessage(d,state,baseTopic(d)+'/state','{"__proto__":{"polluted":true},"model":"tc1"}');assert.equal({}.polluted,undefined);assert.equal(state.model,undefined);});
test('Legacy HA text mode keeps original uppercase topics and controls',()=>{const d=deviceEntry({mac:'aabbccddeeff',version:'v1'}),state={};assert(applyMessage(d,state,'homeassistant/switch/AABBCCDDEEFF/socket_2/state','set socket AABBCCDDEEFF 2 1'));assert.equal(state.plug_2.on,1);assert.deepEqual(command({...d,...state},{plug_2:{on:0}}),{topic:'device/ztc1/AABBCCDDEEFF/set',payload:'set socket AABBCCDDEEFF 2 0'});assert(!applyMessage(d,state,'homeassistant/switch/001122334455/socket_2/state','set socket AABBCCDDEEFF 2 0'));});
test('migrated HA outlet names survive retained state replay and later status updates',()=>{
 const d=deviceEntry({mac:'d0bae4632b42',version:'v1'}),state={};
 const base='homeassistant/switch/D0BAE4632B42/socket_1';
 applyMessage(d,state,base+'/config',JSON.stringify({name:'服务器',device:{name:'TC1-2B42'}}));
 applyMessage(d,state,base+'/state','set socket D0BAE4632B42 1 1');
 assert.equal(state.name,'TC1-2B42');assert.equal(state.plug_1.setting.name,'服务器');assert.equal(state.plug_1.on,1);
 applyMessage(d,state,'device/ztc1/D0BAE4632B42/state','{"plug_1":{"on":0}}');
 assert.equal(state.plug_1.setting.name,'服务器');assert.equal(state.plug_1.on,0);
 applyMessage(d,state,base+'/config','{"name":"主机"}');assert.equal(state.plug_1.setting.name,'主机');assert.equal(state.plug_1.on,0);
});
test('separate legacy meter messages preserve valid numeric power and energy in any order',()=>{
 const d=deviceEntry({mac:'d0bae4632b42',version:'v1'}),state={};
 const send=(entity,data)=>applyMessage(d,state,`homeassistant/sensor/D0BAE4632B42/${entity}/state`,JSON.stringify(data));
 send('powerConsumption',{powerConsumption:'54.598'});assert.equal(state.energy_kwh,54.598);assert.equal(state.power_watts,undefined);
 send('power',{power:'71.605'});send('startupTime',{startupTime:'3 - 08:50:25'});send('powerConsumptionToday',{powerConsumptionToday:'0.999'});
 assert.equal(state.power_watts,71.605);assert.equal(state.energy_kwh,54.598);
 send('powerConsumption',{powerConsumption:'54.599'});assert.equal(state.power_watts,71.605);assert.equal(state.energy_kwh,54.599);
 for(const power of ['',null,'invalid'])send('power',{power});assert.equal(state.power_watts,71.605);
 send('power',{power:'0'});assert.equal(state.power_watts,0);
 const original={};applyMessage(d,original,'device/ztc1/d0bae4632b42/sensor','{"power":"12.5","powerConsumption":"1.25"}');
 assert.equal(original.power_watts,12.5);assert.equal(original.energy_kwh,1.25);
});
test('legacy HA indicator state uses the same key as the device home adapter',()=>{
 const d=deviceEntry({mac:'d0bae4632b42',version:'v1'}),state={};
 applyMessage(d,state,'homeassistant/switch/D0BAE4632B42/led/state','set led D0BAE4632B42 1');
 assert.equal(state.led_lock.on,1);
});
