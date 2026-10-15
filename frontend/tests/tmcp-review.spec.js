import {test,expect} from '@playwright/test';
import fs from 'node:fs';
import mqtt from 'mqtt';
const secrets=process.env.PHICOMM_REVIEW_CREDENTIALS?JSON.parse(fs.readFileSync(process.env.PHICOMM_REVIEW_CREDENTIALS,'utf8')):null;
const key=u=>'u_'+Buffer.from(u).toString('hex');
function connect(url,user,password){return new Promise((resolve,reject)=>{const c=mqtt.connect(url,{username:user,password,reconnectPeriod:0,connectTimeout:5000});c.once('connect',()=>resolve(c));c.once('error',e=>{c.end(true);reject(e);});});}
function publish(c,t,data){return new Promise((resolve,reject)=>c.publish(t,typeof data==='string'?data:JSON.stringify(data),{qos:1,retain:false},e=>e?reject(e):resolve()));}
function subscribe(c,t){return new Promise((resolve,reject)=>c.subscribe(t,{qos:1},(e,g)=>e?resolve([{qos:128}]):resolve(g)));}
test('migration credentials, navigation, live TMCP and isolation',async({page,baseURL})=>{
 test.skip(!secrets,'private migration credentials required');
 const errors=[];page.on('pageerror',e=>errors.push(e.message));
 await page.goto('/');await expect(page).toHaveURL(/\/login$/);await expect(page.getByText('自建 Broker 模式')).toBeVisible();
 await page.getByLabel('用户名',{exact:true}).fill(secrets.mqtt_username);await page.getByLabel('密码',{exact:true}).fill(secrets.mqtt_password);await page.getByRole('button',{name:'登录',exact:true}).click();await expect(page).toHaveURL(/\/$/);await expect(page.getByRole('heading',{name:'设备',exact:true})).toBeVisible();await expect(page.getByText('MQTT 已连接',{exact:true})).toBeVisible();
 await expect(page.getByRole('heading',{name:'MQTT 连接',exact:true})).toHaveCount(0);await page.getByRole('button',{name:'设置',exact:true}).click();await expect(page.getByRole('heading',{name:'MQTT 连接',exact:true})).toBeVisible();await page.getByRole('radio',{name:/第三方 Broker/}).click();await expect(page.getByLabel('WebSocket 地址')).toBeEnabled();await page.getByRole('radio',{name:/平台 Broker/}).click();await page.getByRole('button',{name:'连接服务器',exact:true}).click();await expect(page.getByText('MQTT 已连接',{exact:true})).toBeVisible();await page.getByRole('button',{name:'首页',exact:true}).click();
 const ws=baseURL.replace(/^http/,'ws')+'/mqtt';const c=await connect(ws,secrets.mqtt_username,secrets.mqtt_password);
 try {
 const root=`phicomm/v2/${key(secrets.mqtt_username)}/m1/b0f8932864b6`;
 const grants=await subscribe(c,root+'/#');expect(grants[0].qos).not.toBe(128);
 const deny=await subscribe(c,`phicomm/v2/${key('unrelated_account')}/m1/b0f8932864b6/#`);expect(deny[0].qos).toBe(128);
 const obsolete=await subscribe(c,'phicomm/m1/b0f8932864b6/#');expect(obsolete[0].qos).toBe(128);
 // Synthetic publication only in the isolated migrated copy, never production.
 const sample={name:'M1 · 走查测试样本',sensor_valid:true,sensor_received:true,age_ms:0,temperature:23.5,humidity:50,pm25:12,formaldehyde:0.01,brightness_requested:4};
 c.on('message',(topic,payload)=>{if(topic!==root+'/command')return;const q=JSON.parse(payload.toString());const data=q.action==='status.get'?sample:q.action==='binding.get'?{response_ids:[]}:{peers:[]};void publish(c,root+'/result',{request_id:q.request_id,code:'ok',data});});
 await publish(c,root+'/availability','online');await publish(c,root+'/state',{name:'M1 · 走查测试样本',sensor_valid:true,temperature:23.5,humidity:50,pm25:12,formaldehyde:0.01,brightness_requested:4});
 await page.locator('.device-select').filter({hasText:'M1'}).click();await expect(page.frameLocator('iframe').getByText('23.5',{exact:false})).toBeVisible();
 const received=new Promise(resolve=>c.on('message',(t,p)=>{if(t===root+'/command')resolve(JSON.parse(p.toString()));}));await page.getByRole('button',{name:'刷新状态',exact:true}).click();const cmd=await received;expect(cmd.action).toBe('status.get');expect(cmd.request_id).toBeTruthy();
 await page.screenshot({path:process.env.PHICOMM_REVIEW_ARTIFACTS+'/desktop.png',fullPage:true});await page.setViewportSize({width:390,height:844});await page.screenshot({path:process.env.PHICOMM_REVIEW_ARTIFACTS+'/mobile.png',fullPage:true});expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
 await publish(c,root+'/availability','offline');
 }finally{c.end(true);}
 await page.getByRole('button',{name:'退出',exact:true}).click();await page.getByLabel('用户名',{exact:true}).fill(secrets.admin_username);await page.getByLabel('密码',{exact:true}).fill(secrets.admin_password);await page.getByRole('button',{name:'登录',exact:true}).click();await expect(page).toHaveURL(/\/$/);await page.getByRole('button',{name:'设置',exact:true}).click();await page.getByRole('navigation',{name:'设置分类'}).getByRole('button',{name:/账号安全/}).click();await page.getByRole('link',{name:'平台管理',exact:true}).click();await expect(page).toHaveURL(/\/admin$/);expect((await page.request.get('/api/admin/users')).status()).toBe(200);expect(errors).toEqual([]);
});
test('unauthenticated self-hosted entry',async({page})=>{await page.goto('/login');await page.getByRole('link',{name:'自建 Broker 模式'}).click();await expect(page).toHaveURL(/\/guest$/);await page.getByRole('button',{name:'设置',exact:true}).click();await expect(page.getByLabel('WebSocket 地址')).toBeEnabled();await expect(page.getByRole('heading',{name:'账号安全'})).toHaveCount(0);});
