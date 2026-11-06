import {createHash,randomUUID} from "node:crypto";
import {readFile,mkdir,writeFile,rename,copyFile} from "node:fs/promises";
import {existsSync} from "node:fs";
import {join,posix} from "node:path";
import type {Runtime} from "./runtime.ts";
import type {Workspace,SessionRecord} from "./types.ts";
import type {HelperConnection} from "./remote.ts";
import {deskDir,sessionRoot} from "./storage.ts";

const digest=(bytes:Buffer|string)=>createHash("sha256").update(bytes).digest("hex");
const missing=(error:unknown)=>/REMOTE_FILE_NOT_FOUND/.test(String(error));
function jsonl(bytes:Buffer){return new TextDecoder("utf-8",{fatal:true}).decode(bytes).split(/\r?\n/).filter(Boolean).map(line=>JSON.parse(line));}
async function preserve(path:string,bytes:Buffer){
  await mkdir(join(path,".."),{recursive:true,mode:0o700});
  if(existsSync(path)){if(digest(await readFile(path))!==digest(bytes))throw Error("已有本机导入文件与源不一致，已保留双方文件："+path);return;}
  const temp=path+"."+randomUUID()+".tmp";await writeFile(temp,bytes,{mode:0o600,flag:"wx"});await rename(temp,path);
}

// Import only Pi history and referenced images. Never read remote auth, models or executable extensions.
export async function importRemoteHistory(runtime:Runtime,w:Workspace,connection:HelperConnection,home:string){
  const pi=await connection.root(posix.join(home,".pi"));
  const folder="desk/remote/"+digest(w.path);
  const call=async(method:string,path:string,data:Record<string,unknown>={})=>{
    const base=path==="agent/sessions"||path.startsWith("agent/sessions/")?"agent/sessions":path===folder||path.startsWith(folder+"/")?folder:undefined;
    if(!base)throw Error("历史读取范围无效");
    const expected=posix.join(home,".pi",base);await connection.call("file.stat",{rootHandle:pi.handle,path:base});
    const root=await connection.root(expected);if(root.canonicalPath!==expected)throw Error("历史目录不能通过符号链接替换");
    const reply=await connection.call(method,{rootHandle:root.handle,path:posix.relative(base,path)||".",...data});
    if(method==="file.list")reply.value.entries=reply.value.entries.map((entry:any)=>({...entry,path:posix.join(base,entry.path)}));
    return reply;
  };
  const read=async(path:string)=>{
    if(path.startsWith("/")||path.split("/").includes("..")||path.includes("\\"))throw Error("历史源路径越界");
    const info=(await call("file.stat",path)).value;if(info.kind!=="file")throw Error("历史源必须是普通文件");
    const reply=await call("file.content",path);
    if(reply.value.size!==reply.blob.length||reply.value.sha256!==digest(reply.blob))throw Error("远端历史校验失败");return reply.blob;
  };
  const optional=async(path:string)=>{try{return await read(path);}catch(error){if(!missing(error))throw error;return undefined;}};
  const metadata=await optional(folder+"/state.json");
  const state=metadata?JSON.parse(metadata.toString()):undefined;
  if(state&&(!Array.isArray(state.workspaces)||!Array.isArray(state.sessions)))throw Error("旧远端状态格式无效，未替换本机会话");
  const remoteWorkspace=state?.workspaces.find((item:any)=>item.path===w.path);
  const sourceRecords:SessionRecord[]=state?.sessions.filter((item:any)=>item.workspaceId===remoteWorkspace?.id)||[];
  const local=runtime.store.state.sessions.filter(record=>record.workspaceId===w.id);
  const candidates=new Map<string,{source?:SessionRecord;existing?:SessionRecord;path?:string}>();
  const validId=(id:string)=>/^[a-zA-Z0-9_.-]{1,220}$/.test(id);
  const mapped=(id:string)=>{if(!validId(id))throw Error("远端历史 ID 无效");return w.id+"."+id;};
  for(const source of sourceRecords){const id=mapped(source.id);candidates.set(id,{source,existing:local.find(record=>record.id===id),path:source.sessionPath});}
  for(const existing of local){
    const item=candidates.get(existing.id)||{existing};
    if(existing.sessionPath&&!existsSync(existing.sessionPath))item.path=existing.remoteHistory?.sourcePath||existing.sessionPath;
    candidates.set(existing.id,item);
  }
  // Native Pi sessions without Desk metadata remain discoverable, including empty sessions.
  const scan=async(path:string,depth=0)=>{
    let list;try{list=(await call("file.list",path)).value;}catch(error){if(missing(error))return;throw error;}
    if(list.truncated)throw Error("远端历史目录超过列表上限，未标记迁移完成");
    for(const entry of list.entries){
      if(entry.kind==="directory"&&depth<3)await scan(entry.path,depth+1);
      else if(entry.kind==="file"&&entry.path.endsWith(".jsonl")){
        const full=posix.join(home,".pi",entry.path);if([...candidates.values()].some(item=>item.path===full))continue;
        const first=(await call("file.read",entry.path,{startLine:1,maxLines:1})).value;
        const header=JSON.parse(first.content);if(header?.type!=="session"||header.cwd!==w.path)continue;
        const id=mapped(header.id);if(!candidates.has(id))candidates.set(id,{path:full,source:{id:header.id,cwd:w.path,workspaceId:w.id,title:"历史会话",mode:"standard",status:"idle",depth:0,draft:"",createdAt:header.timestamp,updatedAt:header.timestamp}});
      }
    }
  };
  await scan("agent/sessions");
  const staged:SessionRecord[]=[];
  for(const [id,item] of candidates){
    const source=item.source,existing=item.existing;
    const record=structuredClone({...source,...existing,id,workspaceId:w.id}) as SessionRecord;
    if(!record.cwd||!record.cwd.startsWith("/"))record.cwd=w.path;
    if(!["standard","minimal"].includes(record.mode))record.mode="standard";
    record.status=record.status==="running"?"paused":record.status||"idle";
    if(source?.parentId&&!existing?.parentId)record.parentId=mapped(source.parentId);
    if(item.path&&!(existing?.sessionPath&&existsSync(existing.sessionPath))){
      const prefix=posix.join(home,".pi/agent/sessions")+"/";
      if(!item.path.startsWith(prefix)||!item.path.endsWith(".jsonl"))throw Error("旧会话路径不在默认远端 Pi 历史目录内，已保留原记录："+item.path);
      const path=posix.relative(posix.join(home,".pi"),item.path),bytes=await read(path),entries=jsonl(bytes),header=entries[0];
      if(header?.type!=="session"||typeof header.id!=="string"||header.cwd!==(source?.cwd||record.cwd))throw Error("远端会话头与工作区元数据不一致，未创建空历史");
      const ids=new Set<string>();for(const entry of entries.slice(1)){if(typeof entry.id!=="string"||ids.has(entry.id))throw Error("远端 JSONL 条目损坏或重复");ids.add(entry.id);if(entry.parentId&&!ids.has(entry.parentId))throw Error("远端 JSONL 分支引用不完整");}
      const check=await call("file.hash",path);if(check.value.sha256!==digest(bytes))throw Error("远端历史正在变化，请停止远端任务后重试");
      const destination=join(sessionRoot,"--ssh-"+w.id+"--",digest(item.path)+".jsonl");await preserve(destination,bytes);
      record.sessionPath=destination;record.remoteHistory={sourcePath:item.path,sha256:digest(bytes),importedAt:new Date().toISOString()};
    }
    if(existing?.sessionPath&&!existsSync(existing.sessionPath)&&!record.remoteHistory)throw Error("原会话历史缺失，已保留元数据，未创建替代会话");
    if(source){
      for(const image of [...(record.draftImages||[]),...(record.pending||[]).flatMap(prompt=>prompt.images||[])]){
        if(!image.cacheKey||!/^[a-f0-9]{64}$/.test(image.cacheKey))throw Error("旧图片引用无效");
        const localImage=join(deskDir,"composer-images",image.cacheKey);if(existsSync(localImage)){if(digest(await readFile(localImage))!==image.cacheKey)throw Error("本机图片校验失败");continue;}
        const bytes=await optional(folder+"/composer-images/"+image.cacheKey);
        if(!bytes){image.missing=true;continue;}if(digest(bytes)!==image.cacheKey)throw Error("旧图片完整性校验失败");await preserve(join(deskDir,"composer-images",image.cacheKey),bytes);
      }
    }
    if(record.pending)record.pending=record.pending.map(prompt=>({...prompt,paused:true}));
    staged.push(record);
  }
  if(existsSync(runtime.store.path)){
    const path=join(deskDir,"ssh-imports",w.id);await mkdir(path,{recursive:true,mode:0o700});
    const backup=join(path,"state-before-import.json");if(!existsSync(backup))await copyFile(runtime.store.path,backup);
  }
  runtime.store.state.sessions=[...runtime.store.state.sessions.filter(record=>record.workspaceId!==w.id),...staged];
  w.remote!.historyImported=true;
  runtime.changed();
}
