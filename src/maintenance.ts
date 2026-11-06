import { readFileSync } from "node:fs";
import { platform, arch } from "node:os";
import { getPackageDir } from "@earendil-works/pi-coding-agent";
import { join } from "node:path";
import type { Runtime } from "./runtime.ts";

export const appVersion = JSON.parse(readFileSync(new URL("../package.json", import.meta.url),"utf8")).version as string;
export const sdkVersion = JSON.parse(readFileSync(join(getPackageDir(),"package.json"),"utf8")).version as string;
export const versions = () => ({version:appVersion,sdkVersion,node:process.version,platform:platform(),arch:arch(),timezone:Intl.DateTimeFormat().resolvedOptions().timeZone});
let release: {at:number;value:{tag:string;url:string}} | undefined;
export async function latestRelease() {
  if(release&&Date.now()-release.at<300000)return release.value;
  const response=await fetch("https://api.github.com/repos/saucer-man/pi-desk/releases/latest",{headers:{Accept:"application/vnd.github+json","User-Agent":"pi-desk/"+appVersion},signal:AbortSignal.timeout(5000)});
  if(response.status===404)return {tag:"",url:"https://github.com/saucer-man/pi-desk/releases"};
  if(!response.ok)throw new Error("版本检查失败，请稍后重试");const value=await response.json() as {tag_name?:string;html_url?:string};
  if(typeof value.tag_name!=="string"||!value.html_url?.startsWith("https://github.com/saucer-man/pi-desk/releases/"))throw new Error("版本响应无效");
  release={at:Date.now(),value:{tag:value.tag_name,url:value.html_url}};return release.value;
}
export async function diagnostics(runtime:Runtime) {
  const state=runtime.store.state;let models:Awaited<ReturnType<Runtime["models"]>>|undefined;try{models=await runtime.models();}catch{}
  // An explicit whitelist prevents prompts, file paths, credentials and tool output entering the export.
  return {generatedAt:new Date().toISOString(),application:versions(),uptimeSeconds:Math.floor(process.uptime()),memory:process.memoryUsage(),counts:{workspaces:state.workspaces.length,sessions:state.sessions.length,liveSessions:runtime.live.size,schedules:state.schedules?.length||0,providers:models?.providers.length,availableModels:models?.models.length},modelCatalogAvailable:Boolean(models),sessions:state.sessions.map(r=>({id:r.id,status:r.status,depth:r.depth,unread:Boolean(r.unread),hasError:Boolean(r.error),modelRequests:r.modelRequests,pending:r.pending?.length||0})),schedules:state.schedules?.map(t=>({id:t.id,frequency:t.frequency,enabled:t.enabled,status:t.lastStatus,hasError:Boolean(t.lastError)}))};
}
