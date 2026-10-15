// Device service.js retains its HTTP-shaped API; transport is supplied by host.
(()=>{
 const pending=new Map();
 window.addEventListener('message',event=>{
  if(event.source!==parent||event.data?.type!=='device-result')return;
  const entry=pending.get(event.data.id);if(!entry)return;
  pending.delete(event.data.id);clearTimeout(entry.timer);entry.cleanup();
  entry.resolve(new Response(JSON.stringify(event.data.reply),{status:200,headers:{'Content-Type':'application/json'}}));
 });
 window.fetch=(url,options={})=>{
  if(url!=='/api/v1/command')return Promise.reject(Error('不支持的操作'));
  return new Promise((resolve,reject)=>{
   const id=Array.from(crypto.getRandomValues(new Uint8Array(16)),b=>b.toString(16).padStart(2,'0')).join('');
   const abort=()=>{clearTimeout(timer);pending.delete(id);cleanup();reject(Error('暂未收到操作回执，请等待状态更新'));};
   const cleanup=()=>options.signal?.removeEventListener('abort',abort);
   const timer=setTimeout(abort,24000);
   if(options.signal?.aborted){abort();return;}
   options.signal?.addEventListener('abort',abort,{once:true});
   pending.set(id,{resolve,timer,cleanup});
   parent.postMessage({type:'device-command',id,request:JSON.parse(options.body)},parent.location.origin);
  });
 };
})();
window.addEventListener('storage',event=>{
 if(event.key!=='phicomm.appearance')return;
 if(event.newValue==='system'||!event.newValue)delete document.documentElement.dataset.theme;
 else document.documentElement.dataset.theme=event.newValue;
});
