import {test,expect} from '@playwright/test';
import fs from 'node:fs';
const file=process.env.PHICOMM_REVIEW_CREDENTIALS;
test('legacy credentials, automatic MQTT, persisted disconnect and logout cleanup',async({page,context,baseURL},testInfo)=>{
 test.skip(!file,'private review credentials required');test.setTimeout(90000);
 const credentials=JSON.parse(fs.readFileSync(file,'utf8'));
 const errors=[];page.on('pageerror',e=>errors.push(e.message));
 await page.goto('/login');
 const legacy={host:'wss://mqtt.fjrcn.cn/mqtt',username:credentials.mqtt_username,password:credentials.mqtt_password,clientId:'legacy-browser-client'};
 await page.evaluate(value=>{localStorage.clear();localStorage.setItem('mqtt_config',JSON.stringify(value));},legacy);
 await page.reload();
 await expect(page.getByLabel('用户名',{exact:true})).toHaveValue(credentials.mqtt_username);
 await expect(page.getByLabel('密码',{exact:true})).toHaveValue(credentials.mqtt_password);
 await expect(page.getByRole('status')).toContainText('首次登录会自动迁移');
 await page.screenshot({path:testInfo.outputPath('01-upgrade-login.png'),fullPage:true});
 await page.getByRole('button',{name:'登录',exact:true}).click();
 await expect(page.getByText('MQTT 已连接',{exact:true})).toBeVisible({timeout:15000});
 await page.reload();await expect(page.getByText('MQTT 已连接',{exact:true})).toBeVisible({timeout:15000});
 await page.screenshot({path:testInfo.outputPath('02-auto-connected.png'),fullPage:true});
 // A new tab restores the HTTP session and MQTT without another password entry.
 const tab=await context.newPage();await tab.goto(baseURL+'/');await expect(tab.getByText('MQTT 已连接',{exact:true})).toBeVisible({timeout:15000});await tab.close();
 await page.getByRole('button',{name:'设置',exact:true}).click();
 await page.screenshot({path:testInfo.outputPath('03-connection-settings.png'),fullPage:true});
 await page.getByRole('button',{name:'断开连接',exact:true}).click();
 await expect(page.locator('.status-pill').filter({hasText:'MQTT 未连接'})).toBeVisible();
 await page.reload();await expect(page.locator('.status-pill').filter({hasText:'MQTT 未连接'})).toBeVisible();
 await page.screenshot({path:testInfo.outputPath('04-stays-disconnected.png'),fullPage:true});
 await page.waitForTimeout(3500);await expect(page.locator('.status-pill').filter({hasText:'MQTT 未连接'})).toBeVisible();
 await page.getByRole('button',{name:'设置',exact:true}).click();await page.getByRole('button',{name:'连接服务器',exact:true}).click();
 await expect(page.getByText('MQTT 已连接',{exact:true})).toBeVisible({timeout:15000});
 await page.reload();await expect(page.getByText('MQTT 已连接',{exact:true})).toBeVisible({timeout:15000});
 await page.getByRole('button',{name:'退出',exact:true}).click();await expect(page).toHaveURL(/\/login$/);
 expect(await page.evaluate(()=>localStorage.getItem('phicomm.mqtt-login'))).toBeNull();
 expect(await page.evaluate(()=>localStorage.getItem('mqtt_config'))).toBe(JSON.stringify(legacy));
 // Wrong passwords must not be saved as the new platform credential.
 await page.getByLabel('密码',{exact:true}).fill('incorrect-review-password');await page.getByRole('button',{name:'登录',exact:true}).click();
 await expect(page.getByRole('alert')).toContainText(/错误|凭据|认证/);
 expect(await page.evaluate(()=>localStorage.getItem('phicomm.mqtt-login'))).toBeNull();expect(errors).toEqual([]);
});
test('malformed legacy storage does not break login or prefill bogus credentials',async({page})=>{
 await page.goto('/login');await page.evaluate(()=>localStorage.setItem('mqtt_config','{broken'));await page.reload();
 await expect(page.getByLabel('用户名',{exact:true})).toHaveValue('');await expect(page.getByLabel('密码',{exact:true})).toHaveValue('');
 await expect(page.locator('.migration-notice')).toHaveCount(0);
});
