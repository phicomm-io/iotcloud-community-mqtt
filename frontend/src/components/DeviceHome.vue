<script setup>
import {ref,onMounted,onBeforeUnmount} from 'vue';
import pages from 'virtual:device-pages';
const props=defineProps({device:Object});
const emit=defineEmits(['command','settings']);
const frame=ref(),height=ref(700);
function receive(event){
 if(event.source!==frame.value?.contentWindow||event.origin!==location.origin)return;
 if(event.data?.type==='device-height'){height.value=Math.max(200,Math.min(12000,Number(event.data.height)||700));return;}
 if(event.data?.type==='device-settings'){emit('settings');return;}
 if(event.data?.type!=='device-command')return;
 const {id,request}=event.data,target=event.source;
 emit('command',request,reply=>{if(frame.value?.contentWindow===target)target.postMessage({type:'device-result',id,reply:JSON.parse(JSON.stringify(reply))},location.origin);});
}
defineExpose({refresh:()=>frame.value?.contentWindow?.UI?.reload()});
onMounted(()=>window.addEventListener('message',receive));
onBeforeUnmount(()=>window.removeEventListener('message',receive));
</script>
<template><iframe ref="frame" class="device-home" :style="{height:height+'px'}" :srcdoc="pages[props.device.model]" :title="`${props.device.model.toUpperCase()} 首页`" sandbox="allow-scripts allow-same-origin allow-modals" /></template>
<style scoped>
.device-home{display:block;width:100%;border:0;border-radius:0;background:var(--bg);}
</style>
