/* Apply the saved theme before first paint so the page does not flash light on
 * a dark display. The control itself lives in the shared header (ui.js). */
(()=>{
'use strict';
 const key='phicomm.appearance',choices=['system','light','dark'];
 let selected='system';try{const saved=localStorage.getItem(key);if(choices.includes(saved))selected=saved;}catch{}
 if(selected==='system')document.documentElement.removeAttribute('data-theme');
 else document.documentElement.dataset.theme=selected;
})();
