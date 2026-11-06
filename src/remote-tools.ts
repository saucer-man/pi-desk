import {createReadTool,createWriteTool,createEditTool,createBashTool,createFindTool,createGrepTool,createLsTool,type ToolDefinition,type ExtensionFactory} from "@earendil-works/pi-coding-agent";
import {createHash} from "node:crypto";
import {relative,isAbsolute,dirname,extname,posix} from "node:path";
import {realpath} from "node:fs/promises";
import {inside} from "./storage.ts";
import type {Runtime} from "./runtime.ts";
import type {SessionRecord,Workspace} from "./types.ts";

export function remoteTools(runtime:Runtime,w:Workspace,record:SessionRecord,anchor:string,skills:()=>{filePath:string}[]){
  const manager=runtime.remote,cwd=record.cwd;
  const logical=(path:string)=>manager.logical(cwd,path);
  const remotePath=(path:string)=>{const value=relative(anchor,path);if(!inside(anchor,path))throw Error("远端工具虚拟路径越界");return value.replaceAll("\\","/")||".";};
  const fileOps=(signal?:AbortSignal)=>{
    const hashes=new Map<string,string>();
    const call=(method:string,path:string,data:Record<string,unknown>={},blob?:Buffer)=>manager.operation(w,method,{...data,path:remotePath(path)},{signal,blob},cwd);
    return {
      access:async(path:string)=>{await call("file.stat",path);},
      readFile:async(path:string)=>{const bytes=await manager.content(w,remotePath(path),cwd);hashes.set(path,createHash("sha256").update(bytes).digest("hex"));return bytes;},
      writeFile:async(path:string,content:string)=>{
        let hash=hashes.get(path),absent=false;
        if(!hash)try{hash=(await call("file.hash",path)).value.sha256;}catch(error){if(!String(error).includes("REMOTE_FILE_NOT_FOUND"))throw error;absent=true;}
        await call("file.write",path,{expectedSha256:hash,expectedAbsent:absent},Buffer.from(content));
      },
      mkdir:async(path:string)=>{if(remotePath(path)!==".")await call("file.mkdir",path);},
      detectImageMimeType:async(path:string)=>({".png":"image/png",".jpg":"image/jpeg",".jpeg":"image/jpeg",".gif":"image/gif",".webp":"image/webp"} as Record<string,string>)[extname(path).toLowerCase()],
    };
  };
  const bashOps={exec:async(command:string,_cwd:string,options:{signal?:AbortSignal;timeout?:number;onData:(data:Buffer)=>void})=>{
    const result=await manager.shell(w,command,{cwd,signal:options.signal,timeout:options.timeout===undefined?86400000:options.timeout*1000,onData:options.onData});return {exitCode:result.exitCode};
  }};
  const localRead=createReadTool(anchor);
  const factory={read:createReadTool,write:createWriteTool,edit:createEditTool};
  const tools:ToolDefinition[]=Object.entries(factory).map(([name,create])=>{
    const base=create(anchor);
    return {...base,execute:async(id,params:any,signal,onUpdate)=>{
      if(name==="read"&&isAbsolute(params.path)){
        for(const skill of skills()){
          const root=await realpath(dirname(skill.filePath)).catch(()=>undefined);if(!root||!inside(root,params.path))continue;
          const path=await realpath(params.path);if(!inside(root,path))throw Error("Skill 引用越出本机已启用目录");return localRead.execute(id,{...params,path},signal,onUpdate);
        }
      }
      const path=logical(params.path),tool=create(anchor,{operations:fileOps(signal)} as any);
      return tool.execute(id,{...params,path},signal,onUpdate);
    }} as ToolDefinition;
  });
  const bash=createBashTool(anchor,{operations:bashOps});tools.push({...bash,execute:(id,params:any,signal,onUpdate)=>bash.execute(id,params,signal,onUpdate)} as ToolDefinition);
  const text=(value:string)=>({content:[{type:"text" as const,text:value}],details:{}});
  const find=createFindTool(anchor),grep=createGrepTool(anchor),ls=createLsTool(anchor);
  tools.push({...find,execute:async(_id,params:any,signal)=>{
    const path=logical(params.path||"."),reply=await manager.operation(w,"search.find",{path,pattern:params.pattern,limit:Math.min(2000,Math.max(1,params.limit||1000))},{signal},cwd);
    return text(reply.value.paths.join("\n")+(reply.value.budgetReached?"\n[Search budget reached; refine the query.]":""));
  }} as ToolDefinition);
  tools.push({...grep,execute:async(_id,params:any,signal)=>{
    const path=logical(params.path||"."),pattern=(params.ignoreCase?"(?i)":"")+(params.literal?String(params.pattern).replace(/[.*+?^${}()|[\]\\]/g,"\\$&"):params.pattern);
    const reply=await manager.operation(w,"search.grep",{path,pattern,glob:params.glob||"",limit:Math.min(2000,Math.max(1,params.limit||100))},{signal},cwd);
    const lines:string[]=[];
    for(const match of reply.value.matches){
      lines.push(`${match.path}:${match.line}: ${match.text}`);
      const context=Math.min(20,Math.max(0,params.context||0));if(context){const result=await manager.operation(w,"file.read",{path:match.path,startLine:Math.max(1,match.line-context),maxLines:context*2+1},{signal},cwd);lines.push(result.value.content);}
    }
    return text(lines.join("\n")+(reply.value.budgetReached?"\n[Search budget reached; refine the query.]":""));
  }} as ToolDefinition);
  tools.push({...ls,execute:async(_id,params:any,signal)=>{const reply=await manager.operation(w,"file.list",{path:logical(params.path||".")},{signal},cwd);return text(reply.value.entries.slice(0,Math.max(1,params.limit||500)).map((entry:any)=>posix.basename(entry.path)+(entry.kind==="directory"?"/":"")).join("\n")+(reply.value.truncated?"\n[Directory listing truncated.]":""));}} as ToolDefinition);
  const extension:ExtensionFactory=pi=>{
    pi.on("before_agent_start",async event=>({systemPrompt:event.systemPrompt+`\n\nRemote workspace: ${cwd}\nPi local anchor: ${anchor}\nUse the registered remote tools for project files and commands. Local enabled skills can be read, but shell commands always run remotely. Local plugins and MCP servers run on the host; they are not automatically remote.\n\n`+await manager.context(w,cwd)}));
    pi.on("user_bash",()=>({operations:bashOps}));
  };
  return {tools,extension};
}
