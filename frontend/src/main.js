import { createApp } from 'vue'
import Root from './Root.vue'
import router from './router.js'
import {track} from './analytics'
router.afterEach((to,from,failure)=>{if(!failure)track('pageview',to.path);});
import './style.css'



async function start(){
 if(import.meta.env.VITE_DEMO === '1') await import('./demo.js');
 createApp(Root).use(router).mount('#app');
}
start();
