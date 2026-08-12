import{c as o,a2 as a}from"./index-gQTkySAn.js";/**
 * @license lucide-react v0.460.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const s=o("Copy",[["rect",{width:"14",height:"14",x:"8",y:"8",rx:"2",ry:"2",key:"17jyea"}],["path",{d:"M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2",key:"zix9uf"}]]);async function y(t){var n;if(!t)return!1;if(typeof navigator<"u"&&((n=navigator.clipboard)!=null&&n.writeText))try{return await navigator.clipboard.writeText(t),!0}catch{}try{if((await a.copyText(t)).copied)return!0}catch{}if(typeof document>"u"||!document.body)return!1;const e=document.createElement("textarea");e.value=t,e.setAttribute("readonly",""),e.style.position="fixed",e.style.left="-9999px",e.style.top="0",e.style.opacity="0";const c=document.activeElement instanceof HTMLElement?document.activeElement:null;document.body.appendChild(e),e.focus(),e.select(),e.setSelectionRange(0,e.value.length);try{return document.execCommand("copy")}catch{return!1}finally{e.remove(),c==null||c.focus()}}export{s as C,y as c};
