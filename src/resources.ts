import { readFile, writeFile, readdir, realpath, mkdir, stat, unlink, rename } from "node:fs/promises";
import { resolve, join, dirname, isAbsolute, basename, extname } from "node:path";
import { readFileSync, writeFileSync, renameSync, unlinkSync, mkdirSync } from "node:fs";
import { randomUUID, createHash } from "node:crypto";
import { pathToFileURL, fileURLToPath } from "node:url";
import { Compile } from "typebox/compile";
import { DefaultPackageManager, SettingsManager, getPackageDir, loadSkills, createCodemodeExtension, createToolSearchExtension, createMcpExtension, type InlineExtension } from "@earendil-works/pi-coding-agent";
import { agentDir, inside, safeError } from "./storage.ts";
import type { McpServerConfig } from "@earendil-works/pi-coding-agent";
import type { McpCatalog, McpDefinition, McpDebugResult, PluginSummary } from "./types.ts";
import { defaultAgentFiles, parseAgent } from "./subagents.ts";
import { parse, stringify } from "yaml";
import type { AgentForm } from "./types.ts";

const MASK = "[已保存]";
async function readJson(path: string) { try { return JSON.parse((await readFile(path, "utf8")).replace(/^\uFEFF/,"")); } catch (error) { if ((error as NodeJS.ErrnoException).code === "ENOENT") return {}; throw error; } }
function protect(value: any, old?: any, secret = false, hide = true): any {
  if (!hide && value === MASK && old !== undefined) return old;
  if (!hide && value === MASK && secret && old === undefined) throw new Error("MCP 密钥占位符没有原值，请填写新值");
  if (typeof value === "string") return secret && hide ? MASK : value;
  if (Array.isArray(value)) return value.map((v, i) => protect(v, old?.[i], secret, hide));
  if (value && typeof value === "object") return Object.fromEntries(Object.entries(value).map(([k,v]) => [k,protect(v,old?.[k],secret || /^(env|headers|authorization|token|password|apiKey|clientSecret|accessToken|refreshToken)$/i.test(k),hide)]));
  return value;
}
export async function resourceRoot(cwd: string, scope: string) {
  if (!["user", "project"].includes(scope)) throw new Error("无效资源作用域");
  const base = scope === "user" ? agentDir : join(cwd, ".pi"); await mkdir(base, { recursive: true });
  const root = await realpath(base); if (scope === "project" && !inside(await realpath(cwd),root)) throw new Error("项目 .pi 目录指向工作区外部"); return root;
}
async function resourcePath(root: string, name: string) {
  if (typeof name !== "string" || isAbsolute(name) || name.split(/[/\\]/).includes("..") || !/^(prompts|extensions|agents)[/\\][\w.\-/\\\u4e00-\u9fff ]+\.(md|ts|js|mjs)$/i.test(name)) throw new Error("资源路径无效，必须位于 prompts / extensions / agents 内");
  const target = resolve(root, name); if (!inside(root,target)) throw new Error("资源路径越界");
  let parent = target;
  while (true) { try { const canonical = await realpath(parent); if (!inside(root,canonical)) throw new Error("资源符号链接越界"); break; } catch (e) { if ((e as NodeJS.ErrnoException).code !== "ENOENT") throw e; parent=dirname(parent); } }
  return target;
}
export async function resources(cwd: string, scope: string) {
  const root = await resourceRoot(cwd,scope); const files: string[]=[];
  const walk = async (folder: string, prefix: string) => { for (const entry of await readdir(folder,{withFileTypes:true}).catch(()=>[])) {
    if (entry.isSymbolicLink() || entry.name === "node_modules" || files.length >= 2000) continue;
    const name=prefix+entry.name; if(entry.isDirectory()) await walk(join(folder,entry.name),name+"/"); else if(/\.(md|ts|js|mjs)$/i.test(name)) files.push(name);
  } };
  for(const kind of ["prompts","extensions","agents"]) await walk(join(root,kind),kind+"/");
  const settings=SettingsManager.create(cwd,agentDir); settings.setProjectTrusted(true);
  const manager=new DefaultPackageManager({cwd,agentDir,settingsManager:settings});
  await resourceConfigPath(root,join(root,"mcp.json"));
  return {root,files,agentDefaults:defaultAgentFiles,packages:manager.listConfiguredPackages().filter(p=>p.scope===scope),mcp:protect(await readJson(join(root,"mcp.json")))};
}
export async function skillCatalog(cwd: string, path?: string, disabledSkills: readonly string[] = []) {
  const settings=SettingsManager.create(cwd,agentDir); settings.setProjectTrusted(true);
  const manager=new DefaultPackageManager({cwd,agentDir,settingsManager:settings});
  const resolved=await manager.resolve(async()=>"skip");
  const {skills}=loadSkills({cwd,agentDir,skillPaths:resolved.skills.filter(s=>s.enabled).map(s=>s.path),includeDefaults:false});
  if(path===undefined)return skills.map(s=>({name:s.name,description:s.description,path:s.filePath,enabled:!disabledSkills.includes(s.name)}));
  const skill=skills.find(s=>s.filePath===path); if(!skill)throw new Error("Skill 不存在或未启用，请重新打开列表");
  if((await stat(skill.filePath)).size>1024*1024)throw new Error("Skill 内容预览仅支持 1 MiB 以内的文件");
  return {path:skill.filePath,text:await readFile(skill.filePath,"utf8")};
}
export const embeddedPlugins:{id:string;name:string;type:string;path:string;description:string;factory?:InlineExtension}[]=[
  ...[{name:"codemode",description:"通过代码组合调用工具",factory:createCodemodeExtension()},{name:"tool-search",description:"按需搜索工具定义",factory:createToolSearchExtension()},{name:"mcp",description:"连接并管理 MCP 服务",factory:createMcpExtension()}].map(({name,description,factory})=>({id:`builtin:${name}`,name,type:"Pi 内置扩展",path:join(getPackageDir(),"dist","extensions",name,"index.js"),description,factory:{name,factory,builtin:true,replaceable:true}})),
  ...[["todo","待办事项工具"],["goal","目标管理工具"]].map(([name,description])=>{const path=fileURLToPath(new URL(`../internal/appservice/resources/pi-desk-${name}.ts`,import.meta.url));return {id:path,name:`pi-desk-${name}`,type:"Pi Desk 扩展",path,description};}),
];
const pluginId=(path:string)=>path.startsWith("builtin:")?path:resolve(path);
const legacyPlugins=new Set(["todo","goal","subagents","browser","computer-use","remote"].map(name=>resolve(agentDir,"extensions",`pi-desk-${name}.ts`)));
export async function pluginCatalog(cwd:string,overrides:Record<string,boolean>={}):Promise<PluginSummary[]> {
  const settings=SettingsManager.create(cwd,agentDir);settings.setProjectTrusted(true);
  const manager=new DefaultPackageManager({cwd,agentDir,settingsManager:settings,builtinExtensions:embeddedPlugins.filter(p=>p.factory).map(p=>p.name)});
  const [resolved,additional]=await Promise.all([manager.resolve(async()=>"skip"),manager.resolveExtensionSources(embeddedPlugins.filter(p=>!p.factory).map(p=>p.path),{temporary:true})]);
  const rows=new Map<string,PluginSummary>();
  for(const resource of [...resolved.extensions,...additional.extensions]){
    const id=pluginId(resource.path);if(legacyPlugins.has(id))continue;
    const embedded=embeddedPlugins.find(p=>p.id===id),type=embedded?.type||(resource.metadata.origin==="package"?"资源包插件":resource.metadata.scope==="project"?"项目插件":"用户插件");
    const name=basename(resource.path,extname(resource.path));
    rows.set(id,{id,name:embedded?.name||(name==="index"&&resource.metadata.packageRoot?basename(resource.metadata.packageRoot):name),type,path:embedded?.path||resource.path,source:resource.metadata.source,description:embedded?.description||`${type} · ${resource.metadata.source==="auto"?"自动发现":resource.metadata.source}`,enabled:overrides[id]??resource.enabled});
  }
  rows.set("builtin:subagent",{id:"builtin:subagent",name:"subagent",type:"Pi Desk 内置工具",path:fileURLToPath(new URL(`./subagents${extname(fileURLToPath(import.meta.url))}`,import.meta.url)),source:"pi-desk",description:"委派子 Agent；在 agent 设置中编辑角色",enabled:overrides["builtin:subagent"]!==false});
  return [...rows.values()].sort((a,b)=>a.name.localeCompare(b.name));
}
export async function readPlugin(cwd:string,id:string,overrides:Record<string,boolean>={}) {
  const plugin=(await pluginCatalog(cwd,overrides)).find(p=>p.id===id);if(!plugin)throw Error("插件不存在，请重新打开列表");
  if((await stat(plugin.path)).size>1024*1024)throw Error("插件内容预览仅支持 1 MiB 以内的文件");
  return {...plugin,text:await readFile(plugin.path,"utf8")};
}
export async function readResource(cwd: string, scope: string, name: string) {
  const path=await resourcePath(await resourceRoot(cwd,scope),name);
  let text:string,builtin=false;
  try {if((await stat(path)).size>1024*1024) throw new Error("资源编辑仅支持 1 MiB 以内的文件");text=await readFile(path,"utf8");}
  catch(error){if((error as NodeJS.ErrnoException).code!=="ENOENT"||!Object.hasOwn(defaultAgentFiles,name))throw error;text=defaultAgentFiles[name];builtin=true;}
  const definition=name.replaceAll("\\","/").startsWith("agents/")?parseAgent(text,path):undefined;
  const agent:AgentForm|undefined=definition&&{name:definition.name,description:definition.description,model:definition.model||"",instructions:definition.instructions};
  return {path:name,text,builtin,agent};
}
export async function mutateResource(cwd: string, scope: string, data: Record<string,any>) {
  const root=await resourceRoot(cwd,scope);
  if(data.operation==="package_install" || data.operation==="package_remove") {
    if(typeof data.source!=="string" || !data.source.trim() || data.source.length>2000) throw new Error("请填写包来源");
    const settings=SettingsManager.create(cwd,agentDir); settings.setProjectTrusted(true); const manager=new DefaultPackageManager({cwd,agentDir,settingsManager:settings});
    if(data.operation==="package_install") await manager.installAndPersist(data.source,{local:scope==="project"}); else await manager.removeAndPersist(data.source,{local:scope==="project"});
    await settings.flush(); return;
  }
  if(data.operation==="mcp") {
    if(!data.config || typeof data.config!=="object" || Array.isArray(data.config) || data.config.mcpServers!==undefined && (typeof data.config.mcpServers!=="object" || !data.config.mcpServers || Array.isArray(data.config.mcpServers))) throw new Error("MCP 配置必须是 JSON 对象，mcpServers 必须是对象");
    const path=join(root,"mcp.json"); await resourceConfigPath(root,path);
    const config=protect(data.config,await readJson(path),false,false); const serialized=JSON.stringify(config,null,2); if(serialized.length>1024*1024) throw new Error("MCP 配置不能超过 1 MiB");
    const temp=path+"."+randomUUID()+".tmp";try{await writeFile(temp,serialized+"\n",{mode:0o600});await rename(temp,path);}finally{await unlink(temp).catch(()=>{});}return;
  }
  const path=await resourcePath(root,data.path);
  if(data.operation==="delete") { await unlink(path); return; }
  if(data.operation==="save"&&data.agent!==undefined){
    if(!data.path.replaceAll("\\","/").startsWith("agents/")||!data.agent||typeof data.agent!=="object"||Array.isArray(data.agent)||["name","description","model","instructions"].some(key=>typeof data.agent[key]!=="string"))throw Error("角色的名称、说明、模型和提示词必须是文本");
    const agent=data.agent as AgentForm;if(!agent.name.trim()||!agent.instructions.trim())throw Error("角色名称和提示词不能为空");
    const original=await readResource(cwd,scope,data.path).catch(error=>{if((error as NodeJS.ErrnoException).code!=="ENOENT")throw error;return {text:""};});
    const match=/^---\r?\n([\s\S]*?)\r?\n---\r?\n/.exec(original.text),meta=match?parse(match[1])||{}:{};
    Object.assign(meta,{name:agent.name.trim(),description:agent.description});if(agent.model.trim())meta.model=agent.model.trim();else delete meta.model;
    data.text=`---\n${stringify(meta)}---\n${agent.instructions}`;
  }
  if(data.operation!=="save" || typeof data.text!=="string" || data.text.length>1024*1024) throw new Error("资源内容无效或超过 1 MiB");
  if(data.path.replaceAll("\\","/").startsWith("agents/")){const agent=parseAgent(data.text,path);if(!agent.name.trim()||!agent.instructions.trim())throw Error("角色名称和指令不能为空");}
  if(!data.overwrite) { try { await stat(path); throw new Error("文件已存在，请明确选择覆盖"); } catch(e) { if((e as NodeJS.ErrnoException).code!=="ENOENT") throw e; } }
  await mkdir(dirname(path),{recursive:true}); await writeFile(path,data.text,{mode:0o600});
}
async function resourceConfigPath(root: string,path: string) { try { if(!inside(root,await realpath(path))) throw new Error("MCP 配置符号链接越界"); } catch(e) { if((e as NodeJS.ErrnoException).code!=="ENOENT") throw e; } }

// These helpers are shipped with the pinned SDK but are not re-exported by its package entry.
const mcpConfig=import(pathToFileURL(join(getPackageDir(),"dist/extensions/mcp/config.js")).href) as Promise<typeof import("../node_modules/@earendil-works/pi-coding-agent/dist/extensions/mcp/config.js")>;
const mcpCore=import(pathToFileURL(join(getPackageDir(),"dist/core/mcp-servers.js")).href) as Promise<typeof import("../node_modules/@earendil-works/pi-coding-agent/dist/core/mcp-servers.js")>;
const mcpWrites=new Map<string,Promise<unknown>>();
function mcpName(value:unknown){if(typeof value!=="string"||value.length>256||! /^[a-zA-Z0-9_-]+$/.test(value))throw new Error("MCP 名称仅支持字母、数字、下划线和连字符，最多 256 字符");return value;}
async function mcpPath(cwd:string,scope:unknown,create=false){if(scope!=="global"&&scope!=="project")throw new Error("无效 MCP 作用域");const base=scope==="global"?agentDir:join(cwd,".pi");let root:string;try{root=await realpath(base);}catch(error){if((error as NodeJS.ErrnoException).code!=="ENOENT")throw error;if(!create)return join(base,"mcp.json");root=await resourceRoot(cwd,scope==="global"?"user":"project");}if(scope==="project"&&!inside(await realpath(cwd),root))throw new Error("项目 .pi 目录指向工作区外部");const path=join(root,"mcp.json");await resourceConfigPath(root,path);return path;}
async function mcpDocument(path:string){const raw=await readFile(path,"utf8").catch(e=>{if(e.code==="ENOENT")return "{}";throw e;});const doc=JSON.parse(raw.replace(/^\uFEFF/,""));if(!doc||typeof doc!=="object"||Array.isArray(doc)||doc.mcpServers!==undefined&&(!doc.mcpServers||typeof doc.mcpServers!=="object"||Array.isArray(doc.mcpServers)))throw new Error("mcp.json 必须包含 mcpServers 对象");return {doc,revision:createHash("sha256").update(raw).digest("hex")};}
export async function mcpCatalog(cwd:string,scope="effective"):Promise<McpCatalog>{
  if(scope==="all"){
    const [project,global]=await Promise.all([mcpCatalog(cwd,"project"),mcpCatalog(cwd,"global")]);
    return {paths:project.paths,errors:[...new Set([...project.errors,...global.errors])],servers:[...project.servers,...global.servers]};
  }
  if(!["effective","global","project"].includes(scope))throw new Error("无效 MCP 列表范围");
  const paths={global:await mcpPath(cwd,"global"),project:join(cwd,".pi","mcp.json")};if(scope!=="global")await mcpPath(cwd,"project");const loaded=(await mcpConfig).loadMcpConfig({agentDir,cwd,projectTrusted:scope!=="global"});
  return {paths,errors:loaded.errors.map(safeError),servers:loaded.servers.filter(s=>scope!=="project"||s.scope==="project"||s.override).map(s=>({name:s.name,scope:s.override?"project":s.scope==="project"?"project":"global",source:s.override||s.source,description:s.config.description||("url" in s.config?s.config.url:[s.config.command,...s.config.args||[]].join(" ")),enabled:s.config.enabled!==false,overridden:Boolean(s.override)}))};
}
export async function mcpDefinition(cwd:string,scope:unknown,name:string):Promise<McpDefinition>{const path=await mcpPath(cwd,scope),{doc,revision}=await mcpDocument(path);if(!Object.hasOwn(doc.mcpServers||{},name))throw new Error("MCP 已不存在，请刷新列表");return {name,scope:scope as "global"|"project",source:path,definition:protect(doc.mcpServers[name]),revision};}
async function prepareMcp(cwd:string,data:Record<string,any>){
  const path=await mcpPath(cwd,data.scope),{doc,revision}=await mcpDocument(path);if(data.revision&&data.revision!==revision)throw new Error("配置文件已被修改，请重新打开详情后再保存或调试");
  const name=mcpName(data.name),original=data.original===undefined?undefined:mcpName(data.original);if(original&&!Object.hasOwn(doc.mcpServers||{},original))throw new Error("MCP 已不存在，请刷新列表");
  const definition=protect(configObject(data.definition,"MCP 配置"),original?doc.mcpServers[original]:undefined,false,false);const {validateMcpServerConfig,mcpNamespace}=await mcpCore;let effective=definition;
  if(data.scope==="project"&&definition.command===undefined&&definition.url===undefined&&definition.type===undefined){const base=(await mcpConfig).loadMcpConfig({agentDir,cwd,projectTrusted:false}).servers.find(s=>s.name===name);if(!base)throw new Error("项目覆盖需要同名用户 MCP");if(Object.keys(definition).some(k=>!["enabled","exposure","toolExposure"].includes(k)))throw new Error("项目覆盖只允许 enabled、exposure、toolExposure");effective={...base.config,...definition};}
  if(data.scope==="project"&&definition.auth!==undefined)throw new Error("auth 只能保存在用户级 mcp.json");const config=validateMcpServerConfig(name,effective);if(typeof config==="string")throw new Error(config);
  const others=(await mcpCatalog(cwd)).servers;if(others.some(s=>s.name!==name&&s.name!==original&&mcpNamespace(s.name)===mcpNamespace(name)))throw new Error("MCP 名称命名空间冲突");
  if(name!==original&&Object.hasOwn(doc.mcpServers||{},name))throw new Error("同名 MCP 已存在");return {path,doc,name,original,definition,config};
}
export async function mutateMcp(cwd:string,data:Record<string,any>){
  const path=await mcpPath(cwd,data.scope,true);const job=(mcpWrites.get(path)||Promise.resolve()).catch(()=>{}).then(async()=>{
    let doc:Record<string,any>;const name=mcpName(data.name);
    if(data.operation==="save"){const prepared=await prepareMcp(cwd,data);doc=prepared.doc;doc.mcpServers??={};if(prepared.original&&prepared.original!==name)delete doc.mcpServers[prepared.original];doc.mcpServers={...doc.mcpServers,[name]:prepared.definition};}
    else {doc=(await mcpDocument(path)).doc;if(!Object.hasOwn(doc.mcpServers||{},name))throw new Error("MCP 已不存在，请刷新列表");if(data.operation==="delete")delete doc.mcpServers[name];else if(data.operation==="toggle"){if(typeof data.enabled!=="boolean")throw new Error("启用状态必须是布尔值");if(!doc.mcpServers[name]||typeof doc.mcpServers[name]!=="object"||Array.isArray(doc.mcpServers[name]))throw new Error("请先修复 MCP 配置");doc.mcpServers[name].enabled=data.enabled;}else throw new Error("未知 MCP 操作");}
    await mutateResource(cwd,data.scope==="global"?"user":"project",{operation:"mcp",config:doc});
  });mcpWrites.set(path,job);try{await job;}finally{if(mcpWrites.get(path)===job)mcpWrites.delete(path);}
}
export async function debugMcp(cwd:string,data:Record<string,any>,providerToken:(provider:string)=>Promise<string|undefined>,signal?:AbortSignal):Promise<McpDebugResult>{
  const prepared=await prepareMcp(cwd,data);const {McpServerConnection,createDefaultTransport,McpOAuthCredentialStore}=await import(pathToFileURL(join(getPackageDir(),"dist/extensions/mcp/runtime.js")).href) as typeof import("../node_modules/@earendil-works/pi-coding-agent/dist/extensions/mcp/runtime.js");
  const secrets=new Set<string>();const connection=new McpServerConnection({entry:{name:prepared.name,config:prepared.config as McpServerConfig,source:prepared.path},cwd,credentials:new McpOAuthCredentialStore(),providerToken:async provider=>{const token=await providerToken(provider);if(token)secrets.add(token);return token;},createTransport:(entry,cwd,auth)=>{const transport=createDefaultTransport(entry,cwd,auth);const options=(transport as {options?:{env?:Record<string,string>;headers?:Record<string,string>}}).options;for(const value of Object.values({...options?.env,...options?.headers}))if(value)secrets.add(value);return transport;},onTools(){}});
  let timedOut=false;const close=()=>void connection.close();const timer=setTimeout(()=>{timedOut=true;close();},60000);timer.unref();signal?.addEventListener("abort",close,{once:true});const started=Date.now();
  try {if(signal?.aborted)throw new Error("调试已取消");const client=await connection.getClient();return {state:connection.state,durationMs:Date.now()-started,serverInfo:client.serverInfo,instructions:connection.instructions||prepared.config.description,tools:connection.tools.map(t=>({name:t.name,description:t.description,inputSchema:t.inputSchema}))};}
  catch(error){let message=timedOut?"MCP 调试超时（60 秒）":signal?.aborted?"调试已取消":connection.state==="needs-auth"?"MCP 需要认证，请先通过 Pi 的 /mcp 或 /login 完成登录":safeError(error);for(const value of secrets)message=message.replaceAll(value,"[已隐藏]");return {state:connection.state,error:message,durationMs:Date.now()-started,tools:[]};}
  finally{clearTimeout(timer);signal?.removeEventListener("abort",close);await connection.close();}
}

// Use the schema shipped with the pinned SDK, including compatibility and image options.
const modelSchema = Compile(JSON.parse(readFileSync(join(getPackageDir(), "schemas/models.schema.json"), "utf8")));
const modelsPath = join(agentDir, "models.json"), settingsPath = join(agentDir, "settings.json");
function configJson(path: string): Record<string, any> {
  let value; try { value = JSON.parse(readFileSync(path, "utf8").replace(/^\uFEFF/, "").replace(/"(?:\\.|[^"\\])*"|\/\/[^\n]*/g, m => m[0] === '"' ? m : "").replace(/"(?:\\.|[^"\\])*"|,(\s*[}\]])/g, (m, tail) => tail ?? m)); }
  catch (error) { if ((error as NodeJS.ErrnoException).code === "ENOENT") return {}; throw new Error(`${path} 无法解析，请先修复配置文件`); }
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error(`${path} 必须是 JSON 对象`);
  return value;
}
function modelDocument() {
  const root = configJson(modelsPath); if (root.providers === undefined) root.providers = {};
  if (!modelSchema.Check(root)) throw new Error("models.json 不符合 Pi SDK 配置格式，请先修复配置文件");
  return root;
}
export function modelConfig() {
  const root = modelDocument(), settings = configJson(settingsPath);
  return { path: modelsPath, providers: Object.entries(root.providers).map(([id, provider]: [string, any]) => ({ ...protect(provider), id, hasApiKey: Boolean(provider.apiKey), models: (provider.models || []).map((model: any) => ({ ...protect(model), compaction: settings.compaction?.modelOverrides?.[`${id}/${model.id}`] || {} })) })) };
}
function identifier(value: unknown, label: string): string {
  if (typeof value !== "string" || !value.trim() || value.length > 256 || /[\x00-\x1f\x7f]/.test(value) || ["__proto__", "constructor", "prototype"].includes(value)) throw new Error(`${label} 无效`);
  return value.trim();
}
function configObject(value: any, label: string) {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error(`${label} 必须是 JSON 对象`);
  return value;
}
function validateEndpoint(value: unknown) {
  if (value === undefined || value === "") return;
  try { const url = new URL(String(value)); if (!["http:", "https:"].includes(url.protocol) || url.username || url.password) throw 0; }
  catch { throw new Error("API 地址必须是 HTTP / HTTPS URL，且不能包含用户名或密码"); }
}
// Synchronous read/validate/rename keeps concurrent HTTP edits from losing each other's changes.
// Restore both documents if the second rename fails; never overwrite a malformed source file.
export function mutateModelConfig(data: Record<string, any>, knownProviders: readonly string[]) {
  const root = modelDocument(), settings = configJson(settingsPath);
  if (settings.compaction !== undefined) { configObject(settings.compaction, "compaction"); if (settings.compaction.modelOverrides !== undefined) configObject(settings.compaction.modelOverrides, "compaction.modelOverrides"); }
  const oldProvider = data.originalProvider ? identifier(data.originalProvider, "原提供商 ID") : "";
  const oldModel = data.originalModel ? identifier(data.originalModel, "原模型 ID") : "";
  let from = "", to = "";
  const removed = new Set<string>();
  if (data.operation === "default") {
    settings.defaultProvider = identifier(data.provider, "提供商 ID"); settings.defaultModel = identifier(data.model, "模型 ID");
  } else if (data.operation === "save") {
    const input = configObject(data.provider, "提供商"), modelInput = configObject(data.model, "模型");
    const id = identifier(input.id, "提供商 ID"), modelId = identifier(modelInput.id, "模型 ID");
    if (data.createProvider && (Object.hasOwn(root.providers, id) || knownProviders.includes(id))) throw new Error("提供商 ID 已存在，请使用其他 ID 或在已有提供商下添加模型");
    const original = oldProvider ? root.providers[oldProvider] : root.providers[id];
    if (oldProvider && !original) throw new Error("原提供商不存在，请刷新后重试");
    if (oldProvider && oldProvider !== id && Object.hasOwn(root.providers, id)) throw new Error("目标提供商已存在");
    const models = original?.models || [], index = oldModel ? models.findIndex((m: any) => m.id === oldModel) : -1;
    if (oldModel && index < 0) throw new Error("原模型不存在，请刷新后重试");
    if (models.some((m: any, i: number) => i !== index && m.id === modelId)) throw new Error("模型 ID 已存在");
    const { id: _id, hasApiKey: _has, models: _models, ...providerFields } = input;
    const provider = protect(providerFields, original, false, false);
    if (!Object.hasOwn(provider, "apiKey")) provider.apiKey = original?.apiKey;
    if (data.clearApiKey) delete provider.apiKey;
    const { compaction: _compaction, ...modelFields } = modelInput;
    const model = protect(modelFields, models[index], false, false); model.id = modelId;
    if (model.cost === undefined && models[index]?.cost !== undefined) model.cost = models[index].cost;
    for (const field of ["contextWindow", "maxTokens"]) if (!Number.isSafeInteger(model[field]) || model[field] <= 0 || model[field] > 10_000_000) throw new Error(`${field} 必须是 1–10000000 的整数`);
    if (model.maxTokens > model.contextWindow) throw new Error("最大输出不能超过上下文窗口");
    validateEndpoint(provider.baseUrl); validateEndpoint(model.baseUrl);
    if (!knownProviders.includes(id) || oldProvider && oldProvider !== id) {
      if (!(provider.baseUrl || model.baseUrl) || !(provider.api || model.api)) throw new Error("自定义提供商必须填写 API 地址与协议");
    }
    if (index < 0) models.push(model); else models[index] = model;
    provider.models = models;
    if (oldProvider && oldProvider !== id) delete root.providers[oldProvider];
    root.providers[id] = provider;
    const overrides = settings.compaction?.modelOverrides;
    if (oldProvider && oldProvider !== id && overrides) for (const key of Object.keys(overrides)) if (key.startsWith(oldProvider + "/")) { overrides[id + key.slice(oldProvider.length)] = overrides[key]; delete overrides[key]; }
    if (settings.defaultProvider === oldProvider && oldProvider !== id) settings.defaultProvider = id;
    from = `${id}/${oldModel || modelId}`; to = `${id}/${modelId}`;
    if (settings.defaultProvider === id && settings.defaultModel === oldModel) settings.defaultModel = modelId;
    const compaction = configObject(data.compaction || {}, "压缩参数");
    for (const [key, value] of Object.entries(compaction)) if (!["reserveTokens", "keepRecentTokens"].includes(key) || !Number.isSafeInteger(value) || Number(value) < 0 || Number(value) > 10_000_000) throw new Error("压缩参数必须是 0–10000000 的整数");
    if (overrides && from !== to) delete overrides[from];
    if (Object.keys(compaction).length) { settings.compaction ??= {}; settings.compaction.modelOverrides ??= {}; settings.compaction.modelOverrides[to] = compaction; }
    else if (overrides) delete overrides[to];
  } else if (data.operation === "delete_model" || data.operation === "delete_provider") {
    const id = identifier(data.provider, "提供商 ID"), provider = root.providers[id];
    if (!provider) throw new Error("提供商配置不存在");
    if (data.operation === "delete_provider") {
      for (const key of Object.keys(settings.compaction?.modelOverrides || {})) if (key.startsWith(id + "/")) removed.add(key);
      if (settings.defaultProvider === id) removed.add(`${id}/${settings.defaultModel}`);
      delete root.providers[id];
    } else {
      const modelId = identifier(data.model, "模型 ID"), models = provider.models || [];
      if (!models.some((m: any) => m.id === modelId)) throw new Error("模型配置不存在");
      provider.models = models.filter((m: any) => m.id !== modelId); removed.add(`${id}/${modelId}`);
    }
    for (const key of removed) if (settings.compaction?.modelOverrides) delete settings.compaction.modelOverrides[key];
    if (removed.has(`${settings.defaultProvider}/${settings.defaultModel}`)) { delete settings.defaultProvider; delete settings.defaultModel; }
  } else throw new Error("未知模型配置操作");
  if (!modelSchema.Check(root)) throw new Error("模型配置无效：" + modelSchema.Errors(root).map(e => `${e.instancePath}: ${e.message}`).slice(0, 4).join("；"));
  const files = data.operation === "default" ? [[settingsPath, settings]] as const : [[modelsPath, root], [settingsPath, settings]] as const;
  const backups = files.map(([path]) => { try { return readFileSync(path); } catch (e) { if ((e as NodeJS.ErrnoException).code === "ENOENT") return undefined; throw e; } });
  const temp = files.map(([path]) => path + "." + randomUUID() + ".tmp"); let committed = 0;
  mkdirSync(agentDir, { recursive: true, mode: 0o700 });
  try {
    files.forEach(([, value], i) => { const json = JSON.stringify(value, null, 2); if (json.length > 4 * 1024 * 1024) throw new Error("模型配置不能超过 4 MiB"); writeFileSync(temp[i], json + "\n", { mode: 0o600 }); });
    files.forEach(([path], i) => { renameSync(temp[i], path); committed++; });
  } catch (error) {
    for (let i = 0; i < committed; i++) { if (backups[i]) { writeFileSync(temp[i], backups[i]!, { mode: 0o600 }); renameSync(temp[i], files[i][0]); } else unlinkSync(files[i][0]); }
    throw error;
  } finally { for (const path of temp) try { unlinkSync(path); } catch {} }
}
