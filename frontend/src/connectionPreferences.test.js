import {test} from 'node:test';
import assert from 'node:assert/strict';
import {legacyCredentials,autoConnect,setAutoConnect,rememberLogin,savedPassword,forgetLogin} from './connectionPreferences.js';
const storage=()=>{const data=new Map();return {getItem:k=>data.get(k)??null,setItem:(k,v)=>data.set(k,v),removeItem:k=>data.delete(k)};};
test('original tc1_mqtt credential format is read without mutating legacy data',()=>{
 const s=storage(),raw=JSON.stringify({host:'wss://mqtt.fjrcn.cn/mqtt',username:'old-user',password:'saved-password',clientId:'old'});s.setItem('mqtt_config',raw);
 assert.deepEqual(legacyCredentials(s),{username:'old-user',password:'saved-password'});assert.equal(s.getItem('mqtt_config'),raw);
 for(const value of ['broken','null','{}','{"username":[],"password":"x"}']){s.setItem('mqtt_config',value);assert.equal(legacyCredentials(s),null);}
});
test('automatic connection defaults on, honors manual disconnect and isolates accounts',()=>{
 const s=storage();assert.equal(autoConnect('alice',s),true);setAutoConnect('alice',false,s);assert.equal(autoConnect('alice',s),false);assert.equal(autoConnect('bob',s),true);setAutoConnect('alice',true,s);assert.equal(autoConnect('alice',s),true);
 s.setItem('mqtt_auto_connect','false');assert.equal(autoConnect('legacy',s),false);assert.equal(autoConnect('alice',s),true);
});
test('browser credentials are scoped to the login owner and removed on logout',()=>{
 const s=storage();rememberLogin('alice','secret',s);assert.equal(savedPassword('alice',s),'secret');assert.equal(savedPassword('bob',s),'');forgetLogin(s);assert.equal(savedPassword('alice',s),'');
});
