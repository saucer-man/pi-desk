import type { IncomingMessage, ServerResponse } from "node:http";
import { readFile, readdir, realpath, stat } from "node:fs/promises";
import { homedir } from "node:os";
import { resolve, join, basename, relative, extname } from "node:path";
import { randomUUID } from "node:crypto";
import { pathToFileURL } from "node:url";
import { execFile, spawn } from "node:child_process";
import { promisify } from "node:util";
import { delimiter, isAbsolute, dirname } from "node:path";
import { statSync } from "node:fs";
import { Runtime } from "./runtime.ts";
import { openExternal } from "./browser.ts";
import { versions, latestRelease, diagnostics } from "./maintenance.ts";
import { agentDir, deskDir, directory, inside, safeError } from "./storage.ts";
import { git, agentDefinitions, delegate } from "./subagents.ts";
import { skillCatalog, pluginCatalog, readPlugin, resources, readResource, mutateResource, modelConfig, mutateModelConfig, mcpCatalog, mcpDefinition, mutateMcp, debugMcp } from "./resources.ts";
import { sshIdentity } from "./remote.ts";
import { SettingsManager } from "@earendil-works/pi-coding-agent";

export function json(res: ServerResponse, data: unknown, status = 200) { res.writeHead(status, { "Content-Type": "application/json; charset=utf-8", "Cache-Control": "no-store" }); res.end(JSON.stringify(data, (key, value) => key === "cost" ? undefined : value)); }
async function body(req: IncomingMessage): Promise<Record<string, any>> {
  if (!req.headers["content-type"]?.startsWith("application/json")) throw new Error("请求必须使用 application/json");
  const chunks: Buffer[] = []; let size = 0;
  for await (const chunk of req) { size += chunk.length; if (size > 32 * 1024 * 1024) throw new Error("请求不能超过 32 MiB"); chunks.push(Buffer.from(chunk)); }
  const data = JSON.parse(Buffer.concat(chunks).toString("utf8")); if (!data || typeof data !== "object" || Array.isArray(data)) throw new Error("请求数据必须是对象"); return data;
}
function text(value: unknown, max = 10000) { if (typeof value !== "string" || value.length > max) throw new Error("文本字段缺失或超过长度限制"); return value; }
export function workspace(runtime: Runtime, id: string) { const w = runtime.store.state.workspaces.find(w => w.id === id); if (!w) throw new Error("工作区不存在"); return w; }
async function fileIn(root: string, path: string) { const canonical = await realpath(resolve(root, path)); if (!inside(root, canonical)) throw new Error("路径超出工作区"); return canonical; }
async function findFiles(root: string, query: string) {
  const matches: string[] = []; let scanned = 0; const words = query.toLowerCase().split(/\s+/).filter(Boolean);
  const match = (path: string) => words.every(word => { let i = 0; for (const c of path.toLowerCase()) if (c === word[i]) i++; return i === word.length; });
  const walk = async (folder: string) => { for (const entry of await readdir(folder, { withFileTypes: true }).catch(() => [])) {
    if (scanned >= 20000) return;
    if (entry.isSymbolicLink() || [".git", "node_modules", "bak", "dist", ".cache"].includes(entry.name)) continue;
    const path = join(folder, entry.name); if (entry.isDirectory()) await walk(path); else { scanned++; const name = relative(root, path); if (matches.length < 100 && match(name)) matches.push(name); }
  } };
  await walk(root); return { matches, scanned, truncated: scanned >= 20000 };
}

// Fixed application IDs and argv only; callers never supply a command or shell text.
async function workspaceApplications() {
  const win=process.platform==="win32", mac=process.platform==="darwin";
  const first=async(paths:string[])=>{for(const path of paths.filter(isAbsolute))try{if((await stat(path)).isFile())return path;}catch{} return undefined;};
  const fromPath=(name:string)=>(process.env.PATH||"").split(delimiter).filter(Boolean).map(dir=>join(dir.replace(/^"|"$/g,""),name));
  const registered=async(name:string)=>win?(await Promise.all(["HKCU","HKLM"].map(async hive=>{try{const {stdout}=await promisify(execFile)("reg.exe",["query",`${hive}\\Software\\Microsoft\\Windows\\CurrentVersion\\App Paths\\${name}`,"/ve"],{windowsHide:true,timeout:2000});return /REG_(?:EXPAND_)?SZ\s+(.+)/.exec(stdout)?.[1].trim().replace(/^"|"$/g,"");}catch{return undefined;}}))).filter((p):p is string=>Boolean(p)):[];
  const roots=[process.env.LOCALAPPDATA,process.env.ProgramFiles,process.env["ProgramFiles(x86)"]].filter((p):p is string=>Boolean(p));
  const definitions=[
    {id:"file-manager",name:win?"文件资源管理器":mac?"Finder":"文件管理器",command:win?await first([join(process.env.SystemRoot||"C:/Windows","explorer.exe")]):await first(mac?["/usr/bin/open"]:fromPath("xdg-open")),icon:`file-manager-${win?"windows":process.platform}.png`},
    ...await Promise.all([["cursor","Cursor","Cursor.exe","Cursor.app","cursor.ico"],["vscode","VS Code","Code.exe","Visual Studio Code.app","vscode.ico"]].map(async([id,name,exe,bundle,icon])=>{
      const paths=win?[...await registered(exe),...roots.flatMap(root=>id==="cursor"?[join(root,"Programs/cursor",exe),join(root,"Cursor",exe)]:[join(root,"Programs/Microsoft VS Code",exe),join(root,"Microsoft VS Code",exe)]),...fromPath(exe)]:mac?[join("/Applications",bundle),join(homedir(),"Applications",bundle)]:fromPath(id==="vscode"?"code":"cursor");
      const app=mac?paths.find(path=>{try{return statSync(path).isDirectory();}catch{return false;}}):await first(paths);
      return {id,name,command:mac&&app?"/usr/bin/open":app,prefix:mac&&app?["-a",app]:[],icon};
    })),
    ...(win?[
      {id:"gitbash",name:"Git Bash",command:await first([...await registered("git-bash.exe"),...roots.map(root=>join(root,"Git/git-bash.exe")),...fromPath("git.exe").map(path=>resolve(path,"../../git-bash.exe"))]),icon:""},
      {id:"powershell",name:"PowerShell",command:await first([...roots.map(root=>join(root,"PowerShell/7/pwsh.exe")),...fromPath("pwsh.exe"),join(process.env.SystemRoot||"C:/Windows","System32/WindowsPowerShell/v1.0/powershell.exe")]),prefix:["-NoLogo","-NoExit","-WindowStyle","Normal"],icon:""},
    ]:[]),
  ];
  return await Promise.all(definitions.filter((a):a is typeof a&{command:string}=>Boolean(a.command)).map(async app=>{
    const iconPath=app.id==="gitbash"?join(dirname(app.command),"mingw64/share/git/git-for-windows.ico"):new URL(`../internal/workspaceapp/assets/${app.icon}`,import.meta.url);
    const icon=await readFile(iconPath).then(bytes=>`data:image/${app.id==="gitbash"||app.icon.endsWith("ico")?"x-icon":"png"};base64,${bytes.toString("base64")}`).catch(()=>undefined);
    return {...app,icon};
  }));
}

export async function api(runtime: Runtime, req: IncomingMessage, res: ServerResponse, url: URL) {
  const path = url.pathname; const method = req.method; const [, , resource, id, action] = path.split("/");
  if(path==="/api/open-url"&&method==="POST"){const data=await body(req);await openExternal(text(data.url,10000));json(res,{opened:true});return;}
  if(path==="/api/version"&&method==="GET"){json(res,{...versions(),...(url.searchParams.get("check")==="1"?{latest:await latestRelease()}:{})});return;}
  if(path==="/api/diagnostics"&&method==="GET"){res.setHeader("Content-Disposition","attachment; filename=pi-desk-diagnostics.json");json(res,await diagnostics(runtime));return;}
  if (path === "/api/health") { json(res, { name: "Pi Desk", runtime: "Node.js", sdk: "1.4.0", agentDir, deskDir }); return; }
  if (path === "/api/events" && method === "GET") {
    res.writeHead(200, { "Content-Type": "text/event-stream", "Cache-Control": "no-cache, no-transform", Connection: "keep-alive", "X-Accel-Buffering": "no" });
    let closed=false;
    const cleanup=()=>{closed=true;clearInterval(heartbeat);runtime.listeners.delete(send);};
    const write=(data:string)=>{
      if(closed||res.destroyed||res.writableEnded){cleanup();return;}
      if(res.writableLength>2*1024*1024){cleanup();res.end();return;}
      res.write(data);
    };
    const sessions=url.searchParams.has("sessionId")?new Set(url.searchParams.getAll("sessionId")):undefined;
    const send=Object.assign((event:import("./types.ts").ServerEvent)=>{
      if(sessions&&event.sessionId&&["event","snapshot","widget","status"].includes(event.type)&&!sessions.has(event.sessionId))return;
      write(`data: ${JSON.stringify(event,(key,value)=>key==="cost"?undefined:value)}\n\n`);
    },{sessions});
    const heartbeat=setInterval(()=>write(": heartbeat\n\n"),15000);
    res.on("close",cleanup);res.on("finish",cleanup);res.on("error",()=>{cleanup();res.destroy();});
    runtime.listeners.add(send);
    send({ type: "state", data: runtime.store.state }); for (const { request } of runtime.requests.values()) send({ type: "ui", data: request }); return;
  }
  if(path==="/api/applications" && method==="GET") { json(res,(await workspaceApplications()).map(({id,name,icon})=>({id,name,icon}))); return; }
  if(resource==="workspaces" && id && action==="open" && method==="POST") {
    const w=workspace(runtime,id);if(w.remote)throw new Error("SSH 工作区请使用右侧文件面板；本地打开方式不适用于远端目录"); const data=await body(req); const app=(await workspaceApplications()).find(a=>a.id===data.applicationId); if(!app)throw new Error("应用未安装或不可用");
    const record=data.sessionId?runtime.store.record(text(data.sessionId,100)):undefined;if(record && record.workspaceId!==w.id)throw new Error("会话不属于当前工作区");
    const cwd=await directory(record?.cwd||w.path); const args=[...("prefix" in app?app.prefix||[]:[]),...(app.id==="powershell"?[]:[app.id==="gitbash"?`--cd=${cwd}`:cwd])];
    // Interactive applications must not inherit Electron's Node mode or a hidden window.
    const env={...process.env};for(const key of Object.keys(env))if(["ELECTRON_RUN_AS_NODE","NODE_OPTIONS","VSCODE_IPC_HOOK_CLI"].includes(key.toUpperCase()))delete env[key];
    if(process.platform==="win32"&&app.id==="file-manager") {
      // Match DSH's shell handoff: a file URI avoids Explorer's raw-path parsing.
      const target=pathToFileURL(cwd).href.replace(/(?:%[89A-F][0-9A-F])+/gi,value=>decodeURIComponent(value)).replaceAll(",","%2C").replaceAll("=","%3D");
      await promisify(execFile)(app.command,[target],{cwd,env,windowsHide:false,timeout:10000}).catch(error=>{if(error.code!==1)throw new Error(`无法打开文件资源管理器：${safeError(error)}`);});
    } else await new Promise<void>((resolve,reject)=>{
      const child=spawn(app.command,args,{cwd,env,detached:true,stdio:"ignore",windowsHide:false});
      const finish=(error?:Error)=>{clearTimeout(timer);child.unref();error?reject(error):resolve();};
      const timer=setTimeout(()=>finish(),1000);
      child.once("error",()=>finish(new Error(`无法启动 ${app.name}，请检查安装后重试`)));
      child.once("exit",code=>finish(code!==0?new Error(`${app.name} 启动失败（退出码 ${code}），请检查应用安装`):undefined));
    });json(res,{opened:true});return;
  }
  if(path==="/api/browser"&&method==="GET"){json(res,await runtime.browser.status());return;}
  if(path==="/api/browser"&&method==="POST"){const data=await body(req);json(res,await runtime.browser.command(data as unknown as import("./browser.ts").BrowserCommand));return;}
  if(resource==="schedules") {
    if(!id&&method==="GET"){json(res,{tasks:runtime.store.state.schedules,timezone:Intl.DateTimeFormat().resolvedOptions().timeZone});return;}
    if(!id&&method==="POST"){json(res,await runtime.scheduler.save(await body(req)));return;}
    if(id&&method==="PATCH"){json(res,await runtime.scheduler.save(await body(req),id));return;}
    if(id&&method==="DELETE"){runtime.scheduler.remove(id);json(res,{ok:true});return;}
    if(id&&action==="run"&&method==="POST"){void runtime.scheduler.run(id).catch(()=>{});json(res,{accepted:true},202);return;}
    if(id&&action==="stop"&&method==="POST"){const task=runtime.scheduler.find(id);if(task.lastSessionId&&task.lastStatus==="running"){await runtime.stop(task.lastSessionId);}json(res,{ok:true});return;}
  }
  if (path === "/api/state" && method === "GET") { json(res, runtime.store.state); return; }
  if (path === "/api/proxy" && method === "POST") {
    const data=await body(req),previous=runtime.store.state.preferences.proxy;
    if(data.proxy!==null && (typeof data.proxy!=="string" || data.proxy.length>2048 || /[\x00-\x1f\x7f]/.test(data.proxy)))throw new Error("代理地址无效");
    const proxy=typeof data.proxy==="string"?data.proxy.trim():null;
    if(proxy)try{const url=new URL(proxy);if(!["http:","https:"].includes(url.protocol)||url.pathname!=="/"||url.search||url.hash)throw 0;}catch{throw new Error("代理地址必须是 HTTP / HTTPS URL，例如 http://127.0.0.1:7890");}
    const config={...previous,models:{...previous?.models},providers:{...previous?.providers}};
    if(data.provider!==undefined){const provider=text(data.provider,256).trim();if(!provider||/[\x00-\x1f\x7f]/.test(provider)||["__proto__","constructor","prototype"].includes(provider)||data.model!==undefined)throw new Error("供应商 ID 无效");if(proxy==="")delete config.providers[provider];else config.providers[provider]=proxy;}
    else if(data.model===undefined){if(proxy===null)throw new Error("全局代理请留空以清除");config.global=proxy;}
    else {const model=text(data.model,600);if(!model.includes("/")||/[\x00-\x1f\x7f]/.test(model))throw new Error("模型 ID 无效");if(proxy==="")delete config.models[model];else config.models[model]=proxy;}
    runtime.store.state.preferences.proxy=config;
    try{runtime.changed();}catch(error){runtime.store.state.preferences.proxy=previous;throw error;}
    json(res,config);return;
  }
  if (path === "/api/discover" && method === "POST") { await runtime.discover(); json(res, runtime.store.state); return; }
  if (path === "/api/model-config" && method === "GET") { json(res, modelConfig()); return; }
  if (path === "/api/model-config" && method === "POST") {
    const data = await body(req); const models = await runtime.modelRuntime(); await models.refresh({ allowNetwork: false });
    if (data.operation === "default" && !(await runtime.models()).models.some(m => m.provider === data.provider && m.id === data.model)) throw new Error("默认模型必须是已导入且已认证的可用模型");
    if(data.operation==="import")runtime.assertModelSelection(data.provider?.id,data.models?.map((m:any)=>m.id));
    mutateModelConfig(data, models.getProviders().map(p => p.id));
    const selection=runtime.store.state.preferences.modelSelection;
    if(data.operation==="save"&&selection?.[data.originalProvider||data.provider.id]){
      const previous=data.originalProvider||data.provider.id,ids=selection[previous].map(id=>id===data.originalModel?data.model.id:id);
      if(previous!==data.provider.id)delete selection[previous];selection[data.provider.id]=ids;runtime.changed();
    }
    if(data.operation==="import")await runtime.selectModels(data.provider.id,data.models.map((m:any)=>m.id));
    if(data.operation==="delete_provider"&&selection){delete selection[data.provider];runtime.changed();}
    if(data.operation==="delete_model"&&selection?.[data.provider]){selection[data.provider]=selection[data.provider].filter(id=>id!==data.model);runtime.changed();}
    const providerProxies=runtime.store.state.preferences.proxy?.providers;
    if(data.operation==="save"&&data.originalProvider&&data.originalProvider!==data.provider.id&&providerProxies&&Object.hasOwn(providerProxies,data.originalProvider)){providerProxies[data.provider.id]=providerProxies[data.originalProvider];delete providerProxies[data.originalProvider];runtime.changed();}
    if(data.operation==="delete_provider"&&providerProxies){delete providerProxies[data.provider];runtime.changed();}
    const catalog = await runtime.models(); runtime.emit({ type: "models", data: catalog }); json(res, { ...modelConfig(), catalog }); return;
  }
  if (path === "/api/models" && method === "GET") { json(res, await runtime.models(url.searchParams.get("sessionId") || undefined, url.searchParams.get("refresh")==="1")); return; }
  if (path === "/api/models/selection" && method === "POST") {const data=await body(req);json(res,await runtime.selectModels(text(data.provider,256),data.models));return;}
  if (path === "/api/models/discover" && method === "POST") {const data=await body(req),controller=new AbortController(),cancel=()=>controller.abort();res.on("close",cancel);try{json(res,await runtime.discoverModels(data,controller.signal));}finally{res.off("close",cancel);}return;}
  if (path === "/api/models/test" && method === "POST") {
    const data=await body(req),provider=text(data.provider,256),model=text(data.model,500),prompt=data.prompt===undefined?undefined:text(data.prompt,10000);
    if(url.searchParams.get("stream")!=="1"){json(res,await runtime.testModel(provider,model,url.searchParams.get("sessionId")||undefined,{prompt,draft:data.draft}));return;}
    res.writeHead(200,{"Content-Type":"application/x-ndjson; charset=utf-8","Cache-Control":"no-store"});res.flushHeaders();
    const controller=new AbortController(),cancel=()=>controller.abort(),log=(event:import("./types.ts").ModelProbeLog)=>{if(!res.destroyed&&!res.writableEnded)res.write(JSON.stringify(event)+"\n");};res.on("close",cancel);
    let loggedError=false;
    try{await runtime.testModel(provider,model,undefined,{prompt,draft:data.draft,signal:controller.signal,log:event=>{if(event.kind==="error")loggedError=true;log(event);}});}catch(error){if(!loggedError)log({at:new Date().toISOString(),kind:"error",data:{message:safeError(error)}});}finally{res.off("close",cancel);res.end();}return;
  }
  if (path === "/api/models" && method === "POST") {
    const data = await body(req);const provider = text(data.provider, 150); const models = await runtime.modelRuntime();
    if (data.operation === "cancel_oauth") { runtime.authControllers.get(provider)?.abort(); json(res,{ok:true}); return; }
    if (data.operation === "logout") await models.logout(provider);
    else if (data.operation === "api_key") { const key = text(data.key, 20000); if (!key.trim()) throw new Error("API Key 不能为空"); await models.login(provider, "api_key", { prompt: async () => key, notify() {} }); }
    else if (data.operation === "oauth") {
      const settings=SettingsManager.create(agentDir,agentDir),deviceId=settings.getOrCreateDeviceId();await settings.flush();
      runtime.authControllers.get(provider)?.abort();
      const controller = new AbortController(); runtime.authControllers.set(provider,controller);
      let auth:import("./types.ts").OAuthState={provider,status:"waiting"};
      const update=(data:Partial<typeof auth>)=>{auth={...auth,...data};if(runtime.authControllers.get(provider)===controller)runtime.emit({type:"auth",data:auth});};
      void runtime.withProxy({provider,id:""},()=>models.login(provider, "oauth", {
        signal: controller.signal,
        prompt: async prompt => { const result = await runtime.ask({ type: prompt.type === "select" ? "select" : prompt.type === "secret" ? "secret" : "input", title: prompt.message, url:auth.url, options: prompt.type === "select" ? prompt.options.map(o => o.id) : undefined, oauth:{provider,kind:prompt.type,placeholder:"placeholder" in prompt?prompt.placeholder:undefined,labels:prompt.type==="select"?prompt.options.map(o=>o.label):undefined} }, AbortSignal.any([controller.signal,...(prompt.signal?[prompt.signal]:[])])); if (result === undefined) throw new Error("登录已取消"); return String(result); },
        notify: event => { if (event.type === "auth_url") update({url:event.url,message:event.instructions}); else if (event.type === "device_code") update({url:event.verificationUri,userCode:event.userCode}); else update({message:event.message}); },
      },{getDeviceId:()=>deviceId,agentName:"Pi Desk"})).then(async () => {if(data.selectModels===true&&runtime.store.state.preferences.modelSelection?.[provider]===undefined){const catalog=await runtime.models();(runtime.store.state.preferences.modelSelection??={})[provider]=catalog.defaultProvider===provider&&catalog.defaultModel?[catalog.defaultModel]:[];runtime.changed();}runtime.emit({ type: "models", data: await runtime.models() });update({status:"complete"});}).catch(error => {if(runtime.authControllers.get(provider)!==controller)return;const message=controller.signal.aborted?"登录已取消":safeError(error);update({status:controller.signal.aborted?"cancelled":"error",message});}).finally(() => {if(runtime.authControllers.get(provider)===controller)runtime.authControllers.delete(provider);});
      json(res, { started: true }); return;
    } else throw new Error("未知认证操作");
    json(res, await runtime.models()); return;
  }
  if (resource === "ui" && id && method === "POST") { const data = await body(req); const pending = runtime.requests.get(id); if (!pending) throw new Error("请求已结束"); pending.resolve(data.value); json(res, { ok: true }); return; }
  if (path === "/api/preferences" && method === "POST") { const data = await body(req); if (!["light", "dark", "system"].includes(data.theme)) throw new Error("无效主题"); runtime.store.state.preferences.theme = data.theme; runtime.changed(); json(res, runtime.store.state.preferences); return; }
  if (path === "/api/browse" && method === "GET") {
    const dir = await directory(url.searchParams.get("path") || homedir()); const entries = await readdir(dir, { withFileTypes: true });
    json(res, { path: dir, parent: resolve(dir, ".."), entries: entries.filter(e => e.isDirectory() && !e.name.startsWith(".") && e.name !== "node_modules").map(e => ({ name: e.name, path: join(dir, e.name) })) }); return;
  }
  if (resource === "workspaces") {
    if (!id && method === "POST") {
      const data = await body(req);if(data.remote===true){json(res,await runtime.remote.add(data));return;} const cwd = await directory(text(data.path)); let w = runtime.store.state.workspaces.find(w => w.path.toLowerCase() === cwd.toLowerCase());
      if (!w) { w = { id: randomUUID(), name: typeof data.name === "string" && data.name.trim() ? data.name.slice(0, 100) : basename(cwd), path: cwd, trusted: true }; runtime.store.state.workspaces.push(w); } else w.trusted = true;
      w.hidden=false; runtime.changed(); json(res, w); return;
    }
    if(id&&action==="reconnect"&&method==="POST"){const w=workspace(runtime,id);if(!w.remote)throw new Error("不是 SSH 工作区");await runtime.remote.connect(w);json(res,w);return;}
    if(id&&action==="ssh-info"&&method==="GET"){const w=runtime.store.state.workspaces.find(w=>w.id===id);if(!w?.remote)throw new Error("不是 SSH 工作区");const identity=await sshIdentity(w.remote.target,w.remote.port);json(res,identity);return;}
    if(id&&action==="disconnect"&&method==="POST"){const w=runtime.store.state.workspaces.find(w=>w.id===id);if(!w?.remote)throw new Error("不是 SSH 工作区");if(runtime.store.state.sessions.some(r=>r.workspaceId===id&&(r.status==="running"||runtime.creating.has(r.id)))||runtime.store.state.schedules?.some(task=>task.workspaceId===id&&task.lastStatus==="running"))throw new Error("工作区仍有运行中的任务或正在加载资源");await runtime.remote.disconnect(id);json(res,w);return;}
    if (id && !action && method === "PATCH") { const w = runtime.store.state.workspaces.find(w => w.id === id); if (!w) throw new Error("工作区不存在"); const data = await body(req); if (data.pinned !== undefined && typeof data.pinned !== "boolean") throw new Error("置顶状态必须是布尔值"); if (data.name !== undefined) {const name=text(data.name,100).trim();if(!name)throw new Error("工作区名称不能为空");if(runtime.store.state.workspaces.some(item=>item.id!==id&&!item.hidden&&item.name===name))throw new Error("已有同名工作区");w.name=name;} if (data.pinned !== undefined) w.pinned = data.pinned; if (data.trusted === true) w.trusted = true; runtime.changed(); json(res, w); return; }
    if (id && (!action&&method==="DELETE"||action==="remove"&&method==="POST")) { const w=runtime.store.state.workspaces.find(w=>w.id===id);if(!w)throw new Error("工作区不存在");if(runtime.store.state.sessions.some(r => r.workspaceId === id && (r.status === "running"||runtime.creating.has(r.id)))||runtime.store.state.schedules?.some(task=>task.workspaceId===id&&task.lastStatus==="running"))throw new Error("工作区仍有运行中的任务或正在加载资源");for(const task of runtime.store.state.schedules||[])if(task.workspaceId===id){task.enabled=false;task.nextRunAt=undefined;}await runtime.remote.disconnect(id);await runtime.remove(runtime.store.state.sessions.filter(r=>r.workspaceId===id).map(r=>r.id),false);w.hidden=true;runtime.changed(); json(res, { ok: true }); return; }
  }
  if (resource === "skills" && id) {
    const w=workspace(runtime,id),cwd=w.remote?await runtime.remote.anchor(w):w.path;
    if(method==="GET"){json(res,await skillCatalog(cwd,url.searchParams.has("path")?text(url.searchParams.get("path")):undefined,runtime.store.state.preferences.disabledSkills));return;}
    if(method==="POST"){await runtime.changeSkill(cwd,await body(req));json(res,{skills:await skillCatalog(cwd,undefined,runtime.store.state.preferences.disabledSkills),disabledSkills:runtime.store.state.preferences.disabledSkills||[]});return;}
  }
  if(resource==="plugins"&&id){
    const w=workspace(runtime,id),cwd=w.remote?await runtime.remote.anchor(w):w.path;
    if(method==="GET"){json(res,url.searchParams.has("id")?await readPlugin(cwd,text(url.searchParams.get("id")),runtime.store.state.preferences.pluginOverrides):await pluginCatalog(cwd,runtime.store.state.preferences.pluginOverrides));return;}
    if(method==="POST"){const warnings=await runtime.changePlugin(cwd,await body(req));json(res,{plugins:await pluginCatalog(cwd,runtime.store.state.preferences.pluginOverrides),overrides:runtime.store.state.preferences.pluginOverrides,warnings});return;}
  }
  if(resource==="mcp"&&id){
    const w=workspace(runtime,id),cwd=w.remote?await runtime.remote.anchor(w):w.path,scope=url.searchParams.get("scope")||"effective";
    if(method==="GET"){json(res,url.searchParams.has("name")?await mcpDefinition(cwd,scope,text(url.searchParams.get("name"),256)):await mcpCatalog(cwd,scope));return;}
    if(method==="POST"){const data=await body(req);if(data.operation==="debug"){const controller=new AbortController(),close=()=>controller.abort();res.once("close",close);try{const result=await debugMcp(cwd,data,async provider=>{try{return(await(await runtime.modelRuntime()).getAuth(provider))?.auth.apiKey;}catch{return undefined;}},controller.signal);if(!res.destroyed)json(res,result);}finally{res.off("close",close);}return;}await mutateMcp(cwd,data);json(res,await mcpCatalog(cwd,scope));return;}
  }
  if (resource === "resources" && id) {
    const w=workspace(runtime,id),cwd=w.remote?await runtime.remote.anchor(w):w.path; const scope=url.searchParams.get("scope") || "project";
    if(method==="GET") { json(res,url.searchParams.has("path") ? await readResource(cwd,scope,text(url.searchParams.get("path"))) : await resources(cwd,scope)); return; }
    if(method==="POST") { await mutateResource(cwd,scope,await body(req)); json(res,await resources(cwd,scope)); return; }
  }
  if (resource === "sessions") {
    if (!id && method === "POST") { const data = await body(req);if (!["standard", "minimal"].includes(data.mode)) throw new Error("无效模式"); json(res, runtime.create(text(data.workspaceId), data.mode, typeof data.model === "string" && data.model ? { model: data.model } : {})); return; }
    if (!id) throw new Error("缺少会话 id"); const record = runtime.store.record(id);
    if (!action && method === "DELETE") { json(res, await runtime.remove(id, url.searchParams.get("children") === "1")); return; }
    if (action === "read" && method === "POST") { record.unread=false;runtime.changed(record);json(res,{ok:true});return; }
    if (action === "composer" && method === "GET") { json(res, { text: record.draft, images: runtime.store.images(record.draftImages), pending: (record.pending || []).map(p => ({ ...p, images: runtime.store.images(p.images) })) }); return; }
    if (!action && method === "GET") { json(res, await runtime.snapshot(id)); return; }
    if (!action && method === "PATCH") {
      const data = await body(req);
      if (data.pinned !== undefined && typeof data.pinned !== "boolean") throw new Error("置顶状态必须是布尔值");
      if (data.mode || data.model || data.thinking) { if (data.mode && !["standard", "minimal"].includes(data.mode)) throw new Error("无效模式"); if (data.thinking && !["off", "minimal", "low", "medium", "high", "xhigh", "max"].includes(data.thinking)) throw new Error("无效思考强度"); await runtime.change(id, data); }
      if (data.title !== undefined) { record.title = text(data.title, 200); runtime.live.get(id)?.session.setSessionName(record.title); }
      if (data.draft !== undefined) record.draft = text(data.draft, 1024 * 1024);
      if (data.draftImages !== undefined) record.draftImages = runtime.store.cacheImages(data.draftImages);
      if (data.pinned !== undefined) record.pinned = data.pinned;
      if (data.archived !== undefined) { if (record.status === "running") throw new Error("运行中的会话不能归档"); record.archived = Boolean(data.archived); }
      runtime.changed(record); json(res, record); return;
    }
    if (action === "prompt" && method === "POST") {
      const data = await body(req); const value = text(data.text, 1024 * 1024); const images = data.images;
      if (images !== undefined && (!Array.isArray(images) || images.length > 20 || images.some((i: any) => i.type !== "image" || !["image/png", "image/jpeg", "image/webp", "image/gif"].includes(i.mimeType) || typeof i.data !== "string" || !/^[A-Za-z0-9+/]*={0,2}$/.test(i.data)))) throw new Error("图片附件格式不受支持");
      const live = await runtime.preparePrompt(id); if (!value.trim() && !images?.length) throw new Error("请输入消息或添加图片"); if (record.archived) throw new Error("请先恢复归档会话");
      const running = live.session.isStreaming || record.status === "running";
      if (running) await runtime.prompt(id, value, images, data.behavior === "steer" ? "steer" : "followUp");
      else if(url.searchParams.get("wait")==="1")await runtime.prompt(id,value,images);
      else void runtime.prompt(id, value, images).catch(error => runtime.emit({ type: "notice", sessionId: id, data: { type: "error", message: safeError(error) } }));
      json(res, { accepted: true, queued: running }, 202); return;
    }
    if (action === "stop" && method === "POST") { await runtime.stop(id); json(res, { ok: true }); return; }
    if (action === "traces" && method === "GET") { json(res, runtime.traces(id)); return; }
    if (action === "agents" && method === "GET") { json(res, await agentDefinitions(runtime.remote.workspaceFor(id)?await runtime.remote.anchor(runtime.remote.workspaceFor(id)!):record.cwd)); return; }
    if (action === "reload" && method === "POST") { json(res, await runtime.reload(id)); return; }
    if (action === "delegate" && method === "POST") { json(res, await delegate(runtime, record, await body(req))); return; }
    if (action === "message" && method === "POST") { const data=await body(req); if(!["edit","resend","delete","exclude"].includes(data.operation)) throw new Error("无效消息操作"); json(res,await runtime.mutateMessage(id,text(data.entryId,100),data.operation,data.text===undefined?undefined:text(data.text,1024*1024))); return; }
    if (action === "fork" && method === "POST") { const data = await body(req); json(res, await runtime.fork(id, data.entryId)); return; }
    if (action === "queue" && method === "POST") {
      const data = await body(req);
      if (data.operation === "pause") { json(res, await runtime.pauseQueue(id)); return; }
      if (data.operation === "replace") { runtime.updateQueue(id, data.pending); json(res, { ok: true }); return; }
      if (data.operation === "edit") { json(res, runtime.editQueued(id, text(data.id, 100), text(data.draft ?? "", 1024 * 1024), data.images || [])); return; }
      if (data.operation === "send") {
        await runtime.sendQueued(id, text(data.id, 100)); json(res, { accepted: true }, 202); return;
      }
      throw new Error("未知队列操作");
    }
  }
  if (resource === "files" && id && (method === "GET"&&!action || method === "POST"&&action==="preview")) {
    const w = workspace(runtime, id); if(w.remote){json(res,await runtime.remote.files(w,method==="POST"?text((await body(req)).path):url.searchParams.get("path")||".",url.searchParams.get("search")));return;} if (url.searchParams.has("search")) { json(res, await findFiles(w.path, text(url.searchParams.get("search"), 200))); return; }
    const requested=method==="POST"?text((await body(req)).path):url.searchParams.get("path")||".";
    const path=method==="POST"?await realpath(resolve(w.path,requested.replace(/^~(?=[/\\]|$)/,homedir()))):await fileIn(w.path,requested);const info=await stat(path);
    if (info.isDirectory()) {if(!inside(w.path,path))throw new Error("工作区外仅支持文件预览，不支持目录浏览");const entries = await readdir(path, { withFileTypes: true }); json(res, { path: relative(w.path, path), entries: entries.filter(e => !e.isSymbolicLink()).map(e => ({ name: e.name, directory: e.isDirectory(), path: relative(w.path, join(path, e.name)) })).sort((a,b) => Number(b.directory) - Number(a.directory) || a.name.localeCompare(b.name)) }); return; }
    if(!info.isFile())throw new Error("仅支持普通文件预览");
    const metadata={path:inside(w.path,path)?relative(w.path,path):path,name:basename(path),size:info.size};
    if (info.size > 2 * 1024 * 1024) {json(res,{...metadata,unsupported:"文件超过 2 MiB 预览上限"});return;}
    const content = await readFile(path); const imageType = ({ ".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".webp": "image/webp", ".gif": "image/gif" } as Record<string,string>)[extname(path).toLowerCase()];
    if(imageType){json(res,{...metadata,image:`data:${imageType};base64,${content.toString("base64")}`});return;}
    if(content.subarray(0,5).toString()==="%PDF-"){json(res,{...metadata,pdf:content.toString("base64")});return;}
    try{const text=new TextDecoder("utf-8",{fatal:true}).decode(content);if(text.includes("\0"))throw new Error();json(res,{...metadata,text});}catch{json(res,{...metadata,unsupported:"此文件格式暂不支持预览"});}return;
  }
  if (resource === "git" && id && method === "GET") {
    const w = workspace(runtime, id);
    const run=(args:string[])=>w.remote?runtime.remote.git(w,w.path,args):git(w.path,args);
    const [branch, status, diff, unstaged, staged, untracked] = await Promise.all([["branch", "--show-current"], ["status", "--porcelain=v1"], ["diff", "--stat"], ["diff", "--no-ext-diff", "--no-textconv", "--"], ["diff", "--cached", "--no-ext-diff", "--no-textconv", "--"], ["ls-files", "--others", "--exclude-standard", "-z"]].map(run));
    json(res, { branch, status, diff, unstaged, staged, untracked: untracked.split("\0").filter(Boolean) }); return;
  }
  json(res, { error: "接口不存在" }, 404);
}
