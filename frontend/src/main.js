import { createApp } from 'vue'
import Root from './Root.vue'
import router from './router.js'
import {track} from './analytics'
router.afterEach((to,from,failure)=>{if(!failure)track('pageview',to.path);});
import './style.css'


createApp(Root).use(router).mount('#app')
