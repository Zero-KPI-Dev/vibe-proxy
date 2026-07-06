import{c as s,b as e,t,v as i}from"./index-BZEdHrPu.js";/**
 * @license lucide-react v0.460.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const u=s("Activity",[["path",{d:"M22 12h-2.48a2 2 0 0 0-1.93 1.46l-2.35 8.36a.25.25 0 0 1-.48 0L9.24 2.18a.25.25 0 0 0-.48 0l-2.35 8.36A2 2 0 0 1 4.49 12H2",key:"169zse"}]]);function y(){return e({queryKey:["metrics-summary"],queryFn:t.summary,refetchInterval:3e4})}function c(r){return e({queryKey:["metrics-history",r],queryFn:()=>t.history(r),refetchInterval:3e4})}function n(){return e({queryKey:["provider-health"],queryFn:i.list,refetchInterval:3e4})}export{u as A,n as a,c as b,y as u};
