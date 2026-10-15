<script setup>
const props=defineProps({device:Object,connected:Boolean});const emit=defineEmits(['send-cmd']);
function toggle(index){const p=props.device[`plug_${index}`];if(p)emit('send-cmd',{mac:props.device.mac,[`plug_${index}`]:{on:p.on?0:1}});}
</script>
<template><section class="surface platform-core"><h3>插座</h3><div class="outlet-grid"><button v-for="n in 6" :key="n" :disabled="!connected||device[`plug_${n-1}`]?.on===undefined" @click="toggle(n-1)">{{device[`plug_${n-1}`]?.setting?.name||`插座 ${n}`}} · {{device[`plug_${n-1}`]?.on===undefined?'状态未知':device[`plug_${n-1}`].on?'开':'关'}}</button></div><dl class="sensor-grid"><div><dt>功率</dt><dd>{{device.power_watts??device.power??'—'}} W</dd></div><div><dt>累计电量</dt><dd>{{device.energy_kwh??device.powerConsumption??'—'}} kWh</dd></div></dl></section></template>
