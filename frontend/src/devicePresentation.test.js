import {test} from 'node:test';
import assert from 'node:assert/strict';
import {deviceSummary} from './devicePresentation.js';
test('summaries prioritize useful readings and preserve unknown and offline states',()=>{
 assert.equal(deviceSummary({model:'tc1'},{power:23.5,socket_states:[true,false,true,false,false,false]},true),'23.5 W · 2 开 · 4 关');
 assert.equal(deviceSummary({model:'tc1'},{},true),'等待设备数据');
 assert.equal(deviceSummary({model:'a1'},{fan_on:true,applied_speed:70,led_enabled:false},true),'运行 70% · 灯光关闭');
 assert.equal(deviceSummary({model:'a1'},{fan_on:false,led_enabled:true},true),'已停止 · 灯光开启');
 assert.equal(deviceSummary({model:'a1'},{available:false,fan_on:true},true),'设备离线');
 assert.equal(deviceSummary({model:'m1'},{sensor_valid:true,sensorAt:100,pm25:19},true,101),'空气良好 · PM2.5 19');
 assert.equal(deviceSummary({model:'m1'},{sensor_valid:true,sensorAt:100,pm25:19},true,61000),'等待有效空气数据');
 assert.equal(deviceSummary({model:'tc1'},{power:23},false),'尚未连接');
});
