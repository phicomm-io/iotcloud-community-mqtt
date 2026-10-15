<script setup>
import {computed} from 'vue';
import AppIcon from './AppIcon.vue';
import {deviceType,deviceSummary} from '../devicePresentation';
const props=defineProps({device:Object,state:Object,name:String,connected:Boolean,now:Number});
const summary=computed(()=>deviceSummary(props.device,props.state,props.connected,props.now));
</script>
<template><span class="device-summary"><span class="summary-icon"><AppIcon :name="device.model==='m1'?'air':device.model==='a1'?'fan':'plug'" :size="22"/></span><span class="summary-copy"><b>{{name}}</b><small class="summary-type">{{deviceType(device)}}</small><span class="summary-reading" :class="{muted:!connected||state?.available===false}">{{summary}}</span></span></span></template>
<style scoped>
.device-summary{display:flex;align-items:flex-start;gap:12px;min-width:0;width:100%;text-align:left}.summary-icon{width:38px;height:38px;display:grid;place-items:center;flex-shrink:0;border-radius:var(--radius-s);background:var(--surface-muted);color:var(--muted)}.summary-copy{display:grid;gap:5px;min-width:0;flex:1}.summary-copy b{font-size:14px;line-height:1.4;overflow-wrap:anywhere;color:var(--text)}.summary-type{font-size:11px;font-weight:400;color:var(--muted)}.summary-reading{font-size:12px;font-weight:500;color:var(--text);line-height:1.6}.summary-reading.muted{color:var(--muted)}
</style>
