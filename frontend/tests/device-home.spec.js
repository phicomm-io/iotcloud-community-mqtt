import {test,expect} from '@playwright/test';
test('product home components share platform design without device navigation',async({page})=>{
 const errors=[];page.on('pageerror',e=>errors.push(e.message));
 await page.addInitScript(()=>localStorage.setItem('phicomm_devices',JSON.stringify(['tc1','m1','a1'].map((model,i)=>({model,mac:'aabbccddee0'+i,version:'v2'})))));
 await page.goto('/guest');
 for(const model of ['TC1','M1','A1']){
  await page.locator('.device-select').filter({hasText:model}).click();
  const home=page.frameLocator('iframe');
  await expect(home.locator('p[role=status]')).toContainText('请先连接 MQTT');
  await expect(home.locator('.topbar')).toHaveCount(0);
  await expect(home.locator('#settingsPage')).toBeHidden();
  await expect(home.locator('#homePage')).toBeVisible();
  const buttons=home.locator('#homePage button');
  for(let i=0;i<await buttons.count();i++)await expect(buttons.nth(i)).toBeDisabled();
  expect(await home.locator('body').evaluate(()=>document.documentElement.outerHTML.includes('@include'))).toBe(false);
  const shared=await page.locator('body').evaluate(()=>getComputedStyle(document.documentElement).getPropertyValue('--accent'));
  expect(await home.locator('body').evaluate(()=>getComputedStyle(document.documentElement).getPropertyValue('--accent'))).toBe(shared);
 }
 await page.setViewportSize({width:390,height:844});
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
 const home=page.frameLocator('iframe');
 expect(await home.locator('body').evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
 await page.getByRole('button',{name:'设置',exact:true}).click();
 await expect(page.getByRole('heading',{name:'MQTT 连接',exact:true})).toBeVisible();
 expect(errors).toEqual([]);
});
test('daily list hides protocol details and legacy names can be changed and restored',async({page})=>{
 await page.addInitScript(()=>{if(!localStorage.getItem('phicomm_devices'))localStorage.setItem('phicomm_devices',JSON.stringify([{model:'tc1',mac:'aabbccddee01',version:'v1'}]));});
 await page.goto('/guest');
 await expect(page.locator('.device-list')).not.toContainText(/Legacy|TMCP|aabbccddee01/);
 await page.getByRole('button',{name:/修改.*的名称/}).click();
 await page.getByLabel('设备名称',{exact:true}).fill('书房插排');
 await page.getByRole('button',{name:'保存名称'}).click();
 await expect(page.locator('.device-select')).toContainText('书房插排');
 await page.reload();await expect(page.locator('.device-select')).toContainText('书房插排');
 await page.getByRole('button',{name:'设置',exact:true}).click();
 await page.getByRole('navigation',{name:'设置分类'}).getByRole('button',{name:/设备管理/}).click();
 await expect(page.locator('.managed-device')).toContainText('书房插排');
 await page.locator('.device-details summary').click();
 await expect(page.locator('.device-details')).toContainText('Legacy');
});
