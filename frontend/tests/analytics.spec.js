import {test,expect} from '@playwright/test';
import fs from 'node:fs';
const file=process.env.PHICOMM_REVIEW_CREDENTIALS;
test('real tracking is aggregated only for administrators with responsive charts',async({page,browser,baseURL},testInfo)=>{
 test.skip(!file,'private review credentials required');test.setTimeout(60000);
 const credentials=JSON.parse(fs.readFileSync(file,'utf8')),payloads=[],errors=[];
 page.on('pageerror',e=>errors.push(e.message));page.on('request',request=>{if(request.url().endsWith('/api/analytics/event'))payloads.push(request.postDataJSON());});
 const event=page.waitForResponse(r=>r.url().endsWith('/api/analytics/event')&&r.status()===204);
 await page.goto('/login');await event;
 await page.getByLabel('用户名',{exact:true}).fill(credentials.mqtt_username);await page.getByLabel('密码',{exact:true}).fill(credentials.mqtt_password);await page.getByRole('button',{name:'登录',exact:true}).click();
 await expect(page.getByText('MQTT 已连接',{exact:true})).toBeVisible({timeout:15000});
 const deviceEvent=page.waitForResponse(r=>r.url().endsWith('/api/analytics/event')&&r.request().postDataJSON()?.event==='device_view'&&r.request().postDataJSON()?.model==='tc1'&&r.status()===204);
 await page.locator('.device-select').filter({hasText:'TC1-E12C'}).click();await deviceEvent;
 await expect(page.frameLocator('iframe').locator('#homePage')).toContainText('冰箱',{timeout:20000});
 expect((await page.request.get('/api/admin/analytics')).status()).toBe(403);
 for(const p of payloads){expect(Object.keys(p).sort()).toEqual(['event','model','page','referrer','screen','session','visitor']);expect(JSON.stringify(p)).not.toContain(credentials.mqtt_password);expect(JSON.stringify(p)).not.toContain('D0BAE463E12C');}
 const adminContext=await browser.newContext();const admin=await adminContext.newPage();admin.on('pageerror',e=>errors.push(e.message));
 try{
  await admin.goto(baseURL+'/login');await admin.getByLabel('用户名',{exact:true}).fill(credentials.admin_username);await admin.getByLabel('密码',{exact:true}).fill(credentials.admin_password);await admin.getByRole('button',{name:'登录',exact:true}).click();
  await expect(admin).toHaveURL(baseURL+'/');await admin.getByRole('link',{name:'平台管理',exact:true}).click();await expect(admin).toHaveURL(baseURL+'/admin');
  const stats=admin.getByRole('region',{name:'访问统计'});
  await expect(stats.locator('.analytics-metrics')).toBeVisible();
  await expect(stats).toContainText('设备型号访问');await expect(stats).toContainText('TC1');
  const response=await admin.request.get('/api/admin/analytics?days=7');expect(response.status()).toBe(200);const data=await response.json();expect(data.pageviews).toBeGreaterThan(0);expect(data.visitors).toBeGreaterThan(0);expect(data.models.some(x=>x.name==='TC1'&&x.count>0)).toBe(true);
  fs.writeFileSync(testInfo.outputPath('analytics-summary.json'),JSON.stringify(data,null,2));
  await stats.screenshot({path:testInfo.outputPath('05-admin-analytics-desktop.png')});
  for(const days of ['1','30','90']){const updated=admin.waitForResponse(r=>r.url().includes('/api/admin/analytics?days='+days)&&r.status()===200);await stats.getByLabel('统计范围').selectOption(days);await updated;}
  await admin.setViewportSize({width:390,height:844});await stats.screenshot({path:testInfo.outputPath('06-admin-analytics-mobile.png')});
  expect(await admin.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);expect(errors).toEqual([]);
 }finally{await adminContext.close();}
});
