<script setup>
import {ref} from 'vue';
const emit=defineEmits(['save']);
const dialog=ref(),device=ref(),name=ref(''),error=ref(''),busy=ref(false);
function open(d,current){device.value=d;name.value=current;error.value='';dialog.value.showModal();}
function save(value=name.value.trim()){error.value='';if(value&&new TextEncoder().encode(value).length>63){error.value='名称不能超过 63 字节。';return;}busy.value=true;emit('save',device.value,value,message=>{busy.value=false;if(message)error.value=message;else dialog.value.close();});}
defineExpose({open});
</script>
<template><dialog ref="dialog" class="rename-dialog" @cancel="busy&&$event.preventDefault()"><form @submit.prevent="save()"><h2>修改设备名称</h2><p class="help">使用容易辨认的名称，例如“客厅净化器”。</p><label>设备名称<input v-model="name" maxlength="63" :disabled="busy" autofocus/></label><p v-if="device?.version==='v1'" class="help">旧版设备的名称保存在当前浏览器中。</p><p v-if="error" role="alert" class="error">{{error}}</p><footer><button class="btn ghost" type="button" :disabled="busy" @click="save('')">恢复默认名称</button><button class="btn" type="button" :disabled="busy" @click="dialog.close()">取消</button><button class="btn primary" :disabled="busy">{{busy?'保存中…':'保存名称'}}</button></footer></form></dialog></template>
<style scoped>
.rename-dialog{width:min(440px,calc(100vw - 32px));padding:24px}.rename-dialog form{display:grid;gap:18px}.rename-dialog label{gap:8px}.rename-dialog footer{display:flex;justify-content:flex-end;gap:8px}
</style>
