import { ref, watch } from 'vue';
const saved = (localStorage.getItem('phicomm.appearance')==='system'?'auto':localStorage.getItem('phicomm.appearance'));
export const theme = ref(['light', 'dark', 'auto'].includes(saved) ? saved : 'auto');
watch(theme, value => {
 document.documentElement.classList.remove('light', 'dark');
 if(value==='auto') delete document.documentElement.dataset.theme; else document.documentElement.dataset.theme=value;
 if (value !== 'auto') document.documentElement.classList.add(value);
 localStorage.setItem('phicomm.appearance', value==='auto'?'system':value);
}, { immediate: true });
export function toggleTheme() { theme.value = { auto: 'light', light: 'dark', dark: 'auto' }[theme.value]; }
