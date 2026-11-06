import {spawn,execFile,type ChildProcessWithoutNullStreams} from "node:child_process";
import {readFile,mkdir} from "node:fs/promises";
import {existsSync} from "node:fs";
import {join,resolve,posix} from "node:path";
import {homedir} from "node:os";
import {fileURLToPath} from "node:url";
import {promisify} from "node:util";
import {createHash,randomUUID} from "node:crypto";
import {gzipSync} from "node:zlib";
import {StringDecoder} from "node:string_decoder";
import type {WebSocket} from "ws";
import type {Runtime} from "./runtime.ts";
import type {Workspace} from "./types.ts";
import {deskDir,safeError} from "./storage.ts";
import {importRemoteHistory} from "./remote-history.ts";

export const quote=(value:string)=>"'"+value.replaceAll("'","'\\''")+"'";
const exec=promisify(execFile);
const repo=resolve(fileURLToPath(new URL("../",import.meta.url)));
export function sshArgs(target:string,port?:number){if(!target||target.length>250||/^[-]/.test(target)||/[\s\0]/.test(target))throw new Error("SSH 别名无效");if(port!==undefined&&(!Number.isInteger(port)||port<1||port>65535))throw new Error("SSH 端口无效");return ["-T","-o","BatchMode=yes","-o","StrictHostKeyChecking=yes","-o","ConnectTimeout=10","-o","ServerAliveInterval=10","-o","ServerAliveCountMax=2",...(port?["-p",String(port)]:[]),"--",target];}
export async function sshIdentity(target:string,port?:number){
  const {stdout}=await exec("ssh",["-G",...sshArgs(target,port)]);const lines=stdout.split(/\r?\n/);const field=(name:string)=>lines.find(line=>line.startsWith(name+" "))?.slice(name.length+1)||"";const host=field("hostkeyalias")&&field("hostkeyalias")!=="none"?field("hostkeyalias"):field("hostname");const lookup=field("port")==="22"?host:`[${host}]:${field("port")}`;const keys:string[]=[];
  for(const file of (field("userknownhostsfile")+" "+field("globalknownhostsfile")).split(/\s+/)){const path=file.replace(/^~(?=[/\\]|$)/,homedir());if(!existsSync(path))continue;try{const found=await exec("ssh-keygen",["-F",lookup,"-f",path]);for(const line of found.stdout.split(/\r?\n/)){if(!line||line.startsWith("#"))continue;const values=line.split(/\s+/);const offset=values[0].startsWith("@")?2:1;if(values[offset]&&values[offset+1])keys.push(values[offset]+" "+values[offset+1]);}}catch{}}
  const security=lines.filter(line=>/^(hostname|port|user|hostkeyalias|proxycommand|proxyjump|identityfile|identitiesonly|certificatefile|hostkeyalgorithms|canonicalizehostname|userknownhostsfile|globalknownhostsfile) /.test(line));const publicKeys=[...new Set(keys)].sort();return {hostname:field("hostname"),user:field("user"),port:Number(field("port")),fingerprint:createHash("sha256").update(JSON.stringify({security,publicKeys})).digest("hex"),hostFingerprints:publicKeys.map(key=>"SHA256:"+createHash("sha256").update(Buffer.from(key.split(" ")[1],"base64")).digest("base64").replace(/=+$/,""))};
}

interface Envelope {version:number;kind:string;id:string;generation:number;method?:string;payload?:any;blobLength?:number;timeoutMillis?:number;error?:{code:string;message:string;outcomeUnknown?:boolean}}
export interface HelperReply {value:any;blob:Buffer}
type Pending={process:boolean;resolve:(reply:HelperReply)=>void;reject:(error:Error)=>void;event?:(method:string,value:any,blob:Buffer)=>void;timer:ReturnType<typeof setTimeout>;cleanup:()=>void;mutation:boolean;sequence:number};
export class HelperConnection {
  readonly generation=Date.now();
  readonly pending=new Map<string,Pending>();
  readonly roots=new Map<string,Promise<any>>();
  closed=false;
  private input:Buffer=Buffer.alloc(0);
  private stderr="";
  private waiters=new Set<()=>void>();
  private rootUsers=new Map<string,number>();
  private releasing=new Map<string,Promise<void>>();
  readonly child:ChildProcessWithoutNullStreams;
  readonly onClose:(error:Error)=>void;
  constructor(child:ChildProcessWithoutNullStreams,onClose:(error:Error)=>void=()=>{}){
    this.child=child;this.onClose=onClose;
    child.stdin.on("error",error=>this.fail(error));child.on("error",error=>this.fail(error));
    child.stderr.on("data",chunk=>this.stderr=(this.stderr+chunk).slice(-2000));
    child.stdout.on("data",chunk=>{try{this.parse(Buffer.from(chunk));}catch(error){this.fail(new Error(safeError(error)));}});
    child.on("exit",()=>this.fail(new Error("SSH 连接已断开，未重发未确认的请求。"+safeError(this.stderr))));
  }
  private parse(chunk:Buffer){
    this.input=Buffer.concat([this.input,chunk]);
    while(this.input.length>=4){
      const length=this.input.readUInt32BE();if(!length||length>1024*1024)throw Error("helper 控制帧长度无效");if(this.input.length<4+length)return;
      const frame=JSON.parse(this.input.subarray(4,4+length).toString()) as Envelope;
      const size=frame.blobLength||0;if(frame.version!==1||frame.generation!==this.generation||!Number.isInteger(size)||size<0||size>16*1024*1024)throw Error("helper 协议或连接代次无效");
      if(this.input.length<4+length+size)return;
      const blob=this.input.subarray(4+length,4+length+size);this.input=this.input.subarray(4+length+size);
      const pending=this.pending.get(frame.id);if(!pending)continue;
      if(frame.kind==="event"){
        if(frame.method==="process.accepted")this.send({kind:"event",id:frame.id,method:"stream.credit",payload:{bytes:256*1024}});
        if(frame.method==="stream.data"){
          if(frame.payload?.sequence!==++pending.sequence||blob.length>256*1024)throw Error("helper 输出序列无效");
          pending.event?.(frame.method,frame.payload,blob);this.send({kind:"event",id:frame.id,method:"stream.credit",payload:{bytes:blob.length}});
        }else pending.event?.(frame.method!,frame.payload,blob);
      }else if(frame.kind==="response"||frame.kind==="error"){
        this.pending.delete(frame.id);clearTimeout(pending.timer);pending.cleanup();this.wake();
        if(frame.kind==="error")pending.reject(Object.assign(new Error(`${frame.error?.code}: ${frame.error?.message}${frame.error?.outcomeUnknown?"；修改结果不确定，请检查后再操作":""}`),{code:frame.error?.code,outcomeUnknown:frame.error?.outcomeUnknown}));
        else pending.resolve({value:frame.payload,blob});
      }else throw Error("helper 消息类型无效");
    }
  }
  send(frame:Partial<Envelope>,blob:Buffer=Buffer.alloc(0)){
    if(this.closed)throw Error("SSH 连接已断开");
    const data=Buffer.from(JSON.stringify({version:1,generation:this.generation,...frame,blobLength:blob.length||undefined}));
    if(data.length>1024*1024||blob.length>16*1024*1024)throw Error("helper 请求超过大小上限");
    const header=Buffer.alloc(4);header.writeUInt32BE(data.length);this.child.stdin.write(Buffer.concat([header,data,blob]));
  }
  request(method:string,payload?:unknown,options:{blob?:Buffer;signal?:AbortSignal;timeout?:number;event?:Pending["event"]}={}){
    const id=randomUUID(),mutation=["file.write","file.mkdir","bash.run","terminal.run"].includes(method),timeout=options.timeout??30000;
    let cancel=()=>{};
    const promise=new Promise<HelperReply>((resolve,reject)=>{
      options.signal?.throwIfAborted();
      const abort=()=>{if(!this.pending.has(id))return;this.send({kind:"cancel",id});};cancel=abort;
      const cleanup=()=>options.signal?.removeEventListener("abort",abort);
      const timer=setTimeout(()=>{abort();this.fail(new Error(mutation?"SSH 操作超时，修改结果不确定；未自动重发":"SSH 请求超时"));},timeout+3000);
      const pending:Pending={process:method==="bash.run"||method==="terminal.run",resolve,reject,timer,cleanup,event:options.event,mutation,sequence:0};this.pending.set(id,pending);
      options.signal?.addEventListener("abort",abort,{once:true});
      try{this.send({kind:"request",id,method,payload,timeoutMillis:timeout},options.blob);}catch(error){clearTimeout(timer);cleanup();this.pending.delete(id);reject(error);}
    });
    return {id,promise,cancel};
  }
  private wake(){for(const resolve of this.waiters)resolve();this.waiters.clear();}
  private async wait(signal?:AbortSignal){signal?.throwIfAborted();await new Promise<void>((resolve,reject)=>{const done=()=>{signal?.removeEventListener("abort",abort);this.waiters.delete(done);resolve();},abort=()=>{this.waiters.delete(done);reject(signal?.reason||Error("Operation aborted"));};this.waiters.add(done);signal?.addEventListener("abort",abort,{once:true});});}
  async start(method:string,payload?:unknown,options:Parameters<HelperConnection["request"]>[2]={}){
    const process=method==="bash.run"||method==="terminal.run";
    while(!this.closed&&!method.startsWith("system.")&&(this.pending.size>=60||process&&[...this.pending.values()].filter(p=>p.process).length>=16))await this.wait(options.signal);
    if(this.closed)throw Error("SSH 连接已断开");options.signal?.throwIfAborted();
    return this.request(method,payload,options);
  }
  async call(method:string,payload?:unknown,options:Parameters<HelperConnection["request"]>[2]={}){return (await this.start(method,payload,options)).promise;}
  async usingRoot<T>(path:string,operation:(root:any)=>Promise<T>,signal?:AbortSignal):Promise<T>{
    while(this.releasing.has(path))await this.releasing.get(path);
    while(!this.rootUsers.has(path)&&this.rootUsers.size+this.releasing.size>=24)await this.wait(signal);
    signal?.throwIfAborted();this.rootUsers.set(path,(this.rootUsers.get(path)||0)+1);
    try{return await operation(await this.root(path));}
    finally{
      const remaining=this.rootUsers.get(path)!-1;
      if(remaining)this.rootUsers.set(path,remaining);
      else{this.rootUsers.delete(path);const root=this.roots.get(path);this.roots.delete(path);if(root&&!this.closed){const release=root.then(value=>this.call("root.close",{rootHandle:value.handle})).then(()=>{});this.releasing.set(path,release);try{await release;}finally{this.releasing.delete(path);this.wake();}}else this.wake();}
    }
  }
  root(path:string){let pending=this.roots.get(path);if(!pending){pending=this.call("root.open",{path}).then(r=>r.value);this.roots.set(path,pending);void pending.catch(()=>this.roots.delete(path));}return pending;}
  fail(error:Error){if(this.closed)return;this.closed=true;for(const p of this.pending.values()){clearTimeout(p.timer);p.cleanup();p.reject(p.mutation?Object.assign(new Error(error.message+"；远端修改结果不确定，请先检查，未自动重发"),{outcomeUnknown:true}):error);}this.pending.clear();this.wake();this.child.kill();this.onClose(error);}
  async close(){if(this.closed)return;try{await this.call("system.shutdown",undefined,{timeout:5000});}finally{this.fail(new Error("SSH 已断开"));}}
}

async function prepare(target:string,port?:number){
  const args=sshArgs(target,port);
  const {stdout}=await exec("ssh",["-n",...args,"sh -c "+quote(`printf '%s\\n' "$(uname -s)" "$(uname -m)" "$HOME"`)],{timeout:20000,maxBuffer:10000,windowsHide:true});
  const [system,machine,home]=stdout.trimEnd().split(/\r?\n/),os=system==="Linux"?"linux":system==="Darwin"?"darwin":"",architecture=machine==="x86_64"?"amd64":["arm64","aarch64"].includes(machine)?"arm64":"";
  if(!os||!architecture||!home?.startsWith("/")||/[\r\n\0]/.test(home))throw Error("远端仅支持 Linux/macOS x64、ARM64");
  const manifest=JSON.parse(await readFile(join(repo,"build/remote-helper/artifacts/manifest.json"),"utf8"));
  const artifact=manifest.artifacts?.find((a:any)=>a.os===os&&a.architecture===architecture);
  if(!artifact||artifact.protocol!==1||!/^[a-f0-9]{64}$/.test(artifact.sha256)||!/^[a-f0-9]{64}$/.test(artifact.buildHash))throw Error("helper 产物缺失，请先执行 npm run build");
  const bytes=await readFile(join(repo,"build/remote-helper/artifacts",`helper-${os}-${architecture}`));
  if(bytes.length!==artifact.size||createHash("sha256").update(bytes).digest("hex")!==artifact.sha256)throw Error("本机 helper 产物校验失败");
  const directory=posix.join(home,".pi/desk/remote-helper/1",artifact.sha256),path=posix.join(directory,"helper");
  const cached=await exec("ssh",["-n",...args,"sh -c "+quote(`if test -f ${quote(path)} && test -x ${quote(path)} && test ! -L ${quote(path)}; then if command -v sha256sum >/dev/null 2>&1; then sum=$(sha256sum ${quote(path)}); else sum=$(shasum -a 256 ${quote(path)}); fi; test "\${sum%% *}" = ${quote(artifact.sha256)} && printf cached; fi`)],{timeout:30000,windowsHide:true,maxBuffer:10000}).then(r=>r.stdout==="cached",()=>false);
  if(cached)return {path,home,artifact};
  const script=`set -eu
umask 077
for dir in "$HOME/.pi" "$HOME/.pi/desk" "$HOME/.pi/desk/remote-helper" "$HOME/.pi/desk/remote-helper/1" ${quote(directory)}; do test ! -L "$dir"; mkdir -p "$dir"; done
path=${quote(path)}; tmp="$path.upload-$$"
trap 'rm -f "$tmp"' EXIT HUP INT TERM
gzip -dc > "$tmp"
if command -v sha256sum >/dev/null 2>&1; then sum=$(sha256sum "$tmp"); else sum=$(shasum -a 256 "$tmp"); fi
test "\${sum%% *}" = ${quote(artifact.sha256)}
chmod 700 "$tmp"; mv -f "$tmp" "$path"
`;
  await new Promise<void>((resolve,reject)=>{const child=spawn("ssh",[...args,"sh -c "+quote(script)],{windowsHide:true});let error="";const timer=setTimeout(()=>{child.kill();reject(Error("helper 上传超时"));},300000);child.stderr.on("data",d=>error=(error+d).slice(-2000));child.stdin.on("error",()=>{});child.on("error",reject);child.on("exit",code=>{clearTimeout(timer);code===0?resolve():reject(Error("helper 安装失败："+safeError(error)));});child.stdin.end(gzipSync(bytes,{level:9}));});
  return {path,home,artifact};
}

export class RemoteManager {
  private connected=new Map<string,HelperConnection>();
  private starting=new Map<string,Promise<HelperConnection>>();
  private prepared=new Map<string,ReturnType<typeof prepare>>();
  private closing=false;
  private homes=new Map<string,string>();
  private terminalStarting=new Map<string,Promise<void>>();
  private terminals=new Map<string,{operation:ReturnType<HelperConnection["request"]>;buffer:string;clients:Set<WebSocket>;connection:HelperConnection}>();
  private runtime:Runtime;
  constructor(runtime:Runtime){this.runtime=runtime;for(const w of runtime.store.state.workspaces)if(w.remote)w.remote.connected=false;}
  workspaceFor(id?:string){if(!id)return;const record=this.runtime.store.state.sessions.find(r=>r.id===id);return this.runtime.store.state.workspaces.find(w=>w.remote&&(w.id===id||w.id===record?.workspaceId));}
  async anchor(w:Workspace){const path=join(deskDir,"ssh-workspaces",w.id);await mkdir(path,{recursive:true,mode:0o700});return path;}
  async connect(w:Workspace):Promise<HelperConnection>{
    const current=this.connected.get(w.id);if(current&&!current.closed)return current;
    const pending=this.starting.get(w.id);if(pending)return pending;
    if(this.closing||!w.remote)throw Error("不是 SSH 工作区或服务正在退出");
    const job=(async()=>{
      const config=w.remote!,identity=await sshIdentity(config.target,config.port);
      if(config.configFingerprint&&config.configFingerprint!==identity.fingerprint||config.hostFingerprint&&!identity.hostFingerprints.includes(config.hostFingerprint))throw Error("SSH 主机身份或连接配置已改变，请核对配置");
      config.configFingerprint=identity.fingerprint;
      const key=JSON.stringify([config.target,config.port,identity.fingerprint]);let preparation=this.prepared.get(key);
      if(!preparation){preparation=prepare(config.target,config.port);this.prepared.set(key,preparation);void preparation.catch(()=>this.prepared.delete(key));}
      const ready=await preparation;if(this.closing)throw Error("服务正在退出");
      const child=spawn("ssh",[...sshArgs(config.target,config.port),quote(ready.path)+" serve-stdio"],{windowsHide:true});
      const connection=new HelperConnection(child,error=>{if(this.connected.get(w.id)===connection){this.connected.delete(w.id);config.connected=false;config.error=error.message;this.runtime.changed();}});
      try{
        const nonce=randomUUID(),hello=(await connection.call("system.hello",{nonce,clientVersion:"2.0.0"})).value;
        if(hello.nonce!==nonce||hello.protocolVersion!==1||hello.buildHash!==ready.artifact.buildHash||hello.os!==ready.artifact.os||hello.architecture!==ready.artifact.architecture||!["root.open","root.close","file.content","file.write","bash.run","git.read"].every(method=>hello.capabilities?.includes(method)))throw Error("helper 握手校验失败");
        const root=await connection.root(w.path),binding=createHash("sha256").update(`${root.canonicalPath}/${root.device}/${root.inode}`).digest("hex");
        if(config.identity&&config.identity!==binding)throw Error("远端根目录身份已改变");
        w.path=root.canonicalPath;config.identity=binding;
        if(!config.historyImported)await importRemoteHistory(this.runtime,w,connection,ready.home);
        this.homes.set(w.id,ready.home);config.execution="local-sdk";config.connected=true;config.error=undefined;this.connected.set(w.id,connection);await this.anchor(w);this.runtime.changed();return connection;
      }catch(error){this.prepared.delete(key);connection.fail(new Error(safeError(error)));throw error;}
    })();this.starting.set(w.id,job);
    try{return await job;}catch(error){w.remote.error=safeError(error);w.remote.connected=false;this.runtime.changed();throw error;}finally{this.starting.delete(w.id);}
  }
  async add(data:Record<string,unknown>){
    const target=String(data.target||""),path=String(data.path||""),port=data.port===undefined?undefined:Number(data.port);sshArgs(target,port);
    if(!path.startsWith("/")||path.length>2000||/[\r\n\0]/.test(path)||posix.normalize(path)!==path)throw Error("请提供规范的远端项目绝对路径");
    const existing=this.runtime.store.state.workspaces.find(w=>w.remote?.target===target&&w.remote.port===port&&w.path===path);if(existing){existing.trusted=true;await this.connect(existing);existing.hidden=false;this.runtime.changed();return existing;}
    const w:Workspace={id:randomUUID(),name:typeof data.name==="string"&&data.name.trim()?data.name.trim():`${target}:${posix.basename(path)}`,path,trusted:true,remote:{target,port}};
    this.runtime.store.state.workspaces.push(w);try{await this.connect(w);return w;}catch(error){this.runtime.store.state.workspaces=this.runtime.store.state.workspaces.filter(item=>item!==w);this.runtime.changed();throw error;}
  }
  async root(w:Workspace,cwd=w.path){const connection=await this.connect(w);return {connection,root:await connection.root(cwd)};}
  logical(root:string,path="."){
    if(!path||path.includes("\\")||/[\p{Cc}\p{Cf}]/u.test(path))throw Error("远端路径无效");
    const value=posix.isAbsolute(path)?posix.relative(root,path):path;
    if(value.split("/").includes(".."))throw Error("路径超出远端工作区");
    const result=posix.normalize(value||".");if(posix.isAbsolute(result)||result===".."||result.startsWith("../"))throw Error("路径超出远端工作区");return result;
  }
  async operation(w:Workspace,method:string,data:Record<string,unknown>,options:Parameters<HelperConnection["request"]>[2]={},cwd=w.path){const connection=await this.connect(w);const run=(root:any)=>connection.call(method,{...data,rootHandle:root.handle},options);return cwd===w.path?run(await connection.root(cwd)):connection.usingRoot(cwd,run,options.signal);}
  async content(w:Workspace,path:string,cwd=w.path){const reply=await this.operation(w,"file.content",{path:this.logical(cwd,path)},{},cwd);if(reply.value.sha256!==createHash("sha256").update(reply.blob).digest("hex")||reply.value.size!==reply.blob.length)throw Error("远端文件内容校验失败");return reply.blob;}
  async context(w:Workspace,cwd=w.path){
    const directories=[w.path];if(cwd!==w.path)directories.push(cwd);
    const parts=[];for(const dir of directories){for(const name of ["AGENTS.override.md","AGENTS.md"]){try{const bytes=await this.content(w,name,dir);if(bytes.length>256*1024)throw Error("远端项目指令超过 256 KiB");parts.push(`Instructions from ${posix.join(dir,name)}:\n${bytes.toString()}`);break;}catch(error){if(!String(error).includes("REMOTE_FILE_NOT_FOUND"))throw error;}}}
    return parts.join("\n\n");
  }
  async shell(w:Workspace,command:string,options:{cwd?:string;signal?:AbortSignal;timeout?:number;onData?:(data:Buffer)=>void}={}){
    const connection=await this.connect(w),cwd=options.cwd||w.path,decoder=new StringDecoder("utf8");let output="";
    const run=(root:any)=>connection.call("bash.run",{rootHandle:root.handle,command},{signal:options.signal,timeout:options.timeout||120000,event:(method,_data,blob)=>{if(method==="stream.data"){output+=decoder.write(blob);options.onData?.(blob);}}});
    const result=cwd===w.path?await run(await connection.root(cwd)):await connection.usingRoot(cwd,run,options.signal);output+=decoder.end();return {exitCode:result.value.exitCode as number,output};
  }

  async git(w:Workspace,cwd:string,args:string[],signal?:AbortSignal){const result=await this.shell(w,"git "+args.map(quote).join(" "),{cwd,signal});if(result.exitCode!==0)throw Error(result.output||"远端 Git 执行失败");return result.output.trim();}
  async worktreePath(w:Workspace,id:string){await this.connect(w);if(!/^[a-f0-9-]{36}$/.test(id))throw Error("worktree ID 无效");return posix.join(this.homes.get(w.id)!,".pi/desk/worktrees",id);}
  async files(w:Workspace,requested:string,query:string|null=null){
    if(query!==null){
      if(query.length>200)throw Error("搜索内容过长");const result=await this.operation(w,"search.find",{path:".",pattern:"**/*",limit:2000});
      const words=query.toLowerCase().split(/\s+/).filter(Boolean),matches=result.value.paths.filter((path:string)=>words.every(word=>{let index=0;for(const char of path.toLowerCase())if(char===word[index])index++;return index===word.length;}));
      return {matches:matches.slice(0,100),scanned:result.value.paths.length,truncated:result.value.budgetReached||matches.length>100};
    }
    const path=this.logical(w.path,requested),info=(await this.operation(w,"file.stat",{path})).value;
    if(info.kind==="directory"){
      const listing=(await this.operation(w,"file.list",{path})).value;
      return {path:path==="."?"":path,entries:listing.entries.filter((entry:any)=>entry.kind!=="symlink").map((entry:any)=>({name:posix.basename(entry.path),directory:entry.kind==="directory",path:entry.path})).sort((a:any,b:any)=>Number(b.directory)-Number(a.directory)||a.name.localeCompare(b.name)),truncated:listing.truncated};
    }
    if(info.kind!=="file")throw Error("仅支持远端普通文件预览");
    const metadata={path,name:posix.basename(path),size:info.size};if(info.size>2*1024*1024)return {...metadata,unsupported:"文件超过 2 MiB 预览上限"};
    const bytes=await this.content(w,path),imageType=({".png":"image/png",".jpg":"image/jpeg",".jpeg":"image/jpeg",".webp":"image/webp",".gif":"image/gif"} as Record<string,string>)[posix.extname(path).toLowerCase()];
    if(imageType)return {...metadata,image:`data:${imageType};base64,${bytes.toString("base64")}`};
    if(bytes.subarray(0,5).toString()==="%PDF-")return {...metadata,pdf:bytes.toString("base64")};
    try{const text=new TextDecoder("utf-8",{fatal:true}).decode(bytes);if(text.includes("\0"))throw Error();return {...metadata,text};}catch{return {...metadata,unsupported:"此文件格式暂不支持预览"};}
  }
  async terminal(w:Workspace,socket:WebSocket){
    let terminal=this.terminals.get(w.id);
    if(!terminal){
      let pending=this.terminalStarting.get(w.id);if(!pending){pending=(async()=>{
      const {connection,root}=await this.root(w);let buffer="";const clients=new Set<WebSocket>(),decoder=new StringDecoder("utf8");
      const operation=await connection.start("terminal.run",{rootHandle:root.handle,columns:100,rows:24},{timeout:86400000,event:(method,_data,blob)=>{if(method!=="stream.data")return;const text=decoder.write(blob);buffer=(buffer+text).slice(-200000);for(const client of clients)if(client.readyState===1){if(client.bufferedAmount>=2*1024*1024)client.close(1013);else client.send(JSON.stringify({type:"data",data:text}));}}});
      terminal={operation,get buffer(){return buffer;},clients,connection};this.terminals.set(w.id,terminal);
      void operation.promise.then(result=>{for(const client of clients)if(client.readyState===1)client.send(JSON.stringify({type:"exit",code:result.value.exitCode}));},error=>{for(const client of clients)if(client.readyState===1)client.send(JSON.stringify({type:"error",message:safeError(error)}));}).finally(()=>{for(const client of clients)client.close();this.terminals.delete(w.id);});
      })();this.terminalStarting.set(w.id,pending);}
      try{await pending;}finally{this.terminalStarting.delete(w.id);}
      terminal=this.terminals.get(w.id);
    }
    if(!terminal||socket.readyState!==1)return;
    const active=terminal;active.clients.add(socket);socket.send(JSON.stringify({type:"data",data:active.buffer}));
    socket.on("message",raw=>{try{const data=JSON.parse(raw.toString());if(data.type==="input"&&typeof data.data==="string"&&data.data.length>0&&Buffer.byteLength(data.data)<=64*1024)active.connection.send({kind:"event",id:active.operation.id,method:"terminal.input"},Buffer.from(data.data));if(data.type==="resize"&&Number.isInteger(data.cols)&&Number.isInteger(data.rows))active.connection.send({kind:"event",id:active.operation.id,method:"terminal.resize",payload:{columns:Math.max(2,Math.min(500,data.cols)),rows:Math.max(2,Math.min(300,data.rows))}});if(data.type==="close")active.operation.cancel();}catch{socket.close(1008);}});socket.on("close",()=>active.clients.delete(socket));
  }
  async disconnect(id:string){
    await this.starting.get(id)?.catch(()=>undefined);
    try{await this.connected.get(id)?.close();}finally{this.connected.delete(id);const w=this.workspaceFor(id);if(w?.remote){w.remote.connected=false;w.remote.error=undefined;this.runtime.changed();}}
  }
  async close(){this.closing=true;await Promise.allSettled([...this.starting.values()]);await Promise.allSettled([...this.connected.values()].map(connection=>connection.close()));}
}
