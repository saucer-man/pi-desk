import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, realpath, mkdir, writeFile, readFile, access } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { spawn, execFile } from "node:child_process";
import { once } from "node:events";
import { promisify } from "node:util";
import { createServer, request as httpRequest, IncomingMessage, ServerResponse } from "node:http";
import { WebSocket } from "ws";
import { connect, Socket } from "node:net";

async function stopChild(child:ReturnType<typeof spawn>){
  if(child.exitCode!==null||child.signalCode!==null)return;
  const exited=once(child,"exit"),timer=setTimeout(()=>child.kill("SIGKILL"),5000);
  child.kill();try{await exited;}finally{clearTimeout(timer);}
}

test("CLI: help, version and invalid commands do not create user state",async()=>{
  const temp=await realpath(await mkdtemp(join(tmpdir(),"pi-desk-cli-"))),desk=join(temp,"desk"),run=promisify(execFile);
  const options={env:{...process.env,PI_DESK_DIR:desk,PI_CODING_AGENT_DIR:join(temp,"agent")},timeout:30000,windowsHide:true};
  const pkg=JSON.parse(await readFile("package.json","utf8"));
  assert.equal((await run(process.execPath,[resolve("dist/server.js"),"--version"],options)).stdout.trim(),pkg.version);
  assert.match((await run(process.execPath,[resolve("dist/server.js"),"web","--help"],options)).stdout,/Usage: pi-desk \[web\]/);
  for(const args of [["unknown"],["web","--unknown"],["web","--port"],["web","--port=bad"],["web","--port=65536"]])await assert.rejects(run(process.execPath,[resolve("dist/server.js"),...args],options));
  await assert.rejects(access(desk));
});

test("SSE: ended, disconnected and slow responses stop events and heartbeats",async t=>{
  const {api}=await import("./api.ts");
  for(const reason of ["overflow","heartbeat overflow","ended","destroyed","close","finish","error","initial overflow"])await t.test(reason,async t=>{
    t.mock.timers.enable({apis:["setInterval"]});
    const req=new IncomingMessage(new Socket());req.method="GET";const res=new ServerResponse(req);
    let buffered=reason==="initial overflow"?2*1024*1024+1:0;
    Object.defineProperty(res,"writableLength",{get:()=>buffered});
    const runtime={store:{state:{}},requests:new Map(),listeners:new Set<(event:unknown)=>void>()} as unknown as import("./runtime.ts").Runtime;
    t.after(()=>{req.emit("close");res.destroy();});
    const writes=t.mock.method(res,"write");
    await api(runtime,req,res,new URL("http://127.0.0.1/api/events"));
    const send=[...runtime.listeners][0];
    if(send){const count=writes.mock.callCount();t.mock.timers.tick(15000);assert.equal(writes.mock.callCount(),count+1,"open response sends heartbeats");}
    if(reason==="overflow"){buffered=2*1024*1024+1;send({type:"event"});assert.equal(res.writableEnded,true);}
    else if(reason==="heartbeat overflow"){buffered=2*1024*1024+1;t.mock.timers.tick(15000);assert.equal(res.writableEnded,true);}
    else if(reason==="ended")res.end();
    else if(reason==="destroyed")res.destroy();
    else if(reason==="close")res.emit("close");
    else if(reason==="finish")res.emit("finish");
    else if(reason==="error"){assert.doesNotThrow(()=>res.emit("error",Error("connection reset")));assert.equal(res.destroyed,true);}
    if(reason==="ended"||reason==="destroyed")send({type:"event"});
    assert.equal(runtime.listeners.size,0,"finished response must unsubscribe immediately");
    buffered=0;const count=writes.mock.callCount();send?.({type:"event"});t.mock.timers.tick(15000);
    await new Promise(resolve=>setImmediate(resolve));
    assert.equal(writes.mock.callCount(),count,"late events and heartbeat must not write after cleanup");
  });
});

test("SSE: only subscribed conversations send bodies; background status and notifications remain global",async t=>{
  const {api}=await import("./api.ts");
  for(const query of ["","?sessionId=active&sessionId=child","?sessionId="])await t.test(query||"legacy all sessions",async t=>{
    const req=new IncomingMessage(new Socket());req.method="GET";const res=new ServerResponse(req);
    const runtime={store:{state:{}},requests:new Map(),listeners:new Set<(event:import("./types.ts").ServerEvent)=>void>()} as unknown as import("./runtime.ts").Runtime;
    t.after(()=>res.emit("close"));const writes=t.mock.method(res,"write");
    await api(runtime,req,res,new URL("http://127.0.0.1/api/events"+query));
    const send=[...runtime.listeners][0];
    for(const id of ["active","child","background"])for(const type of ["event","snapshot","widget","status"]){
      const before=writes.mock.callCount();let serialized=false;
      send({type,sessionId:id,data:{toJSON(){serialized=true;return {type:"message_update"};}}});
      const expected=!query||query.includes("active")&&id!=="background";
      assert.equal(writes.mock.callCount()-before,expected?1:0);assert.equal(serialized,expected,"discard background bodies before JSON serialization");
    }
    for(const type of ["state","record","trace","notice","ui","ui_done"]){
      const before=writes.mock.callCount();send({type,sessionId:"background",data:{type:"settled"}});
      assert.equal(writes.mock.callCount(),before+1,`${type} remains available for background tasks`);
    }
  });
});

test("web subscriptions: switching sessions, opening children, reconnecting and cleanup",async()=>{
  const saved=Object.fromEntries(["localStorage","EventSource"].map(key=>[key,Object.getOwnPropertyDescriptor(globalThis,key)]));
  const sources:any[]=[];
  Object.defineProperty(globalThis,"localStorage",{configurable:true,value:{getItem:()=>null,setItem(){},removeItem(){}}});
  Object.defineProperty(globalThis,"EventSource",{configurable:true,value:class {closed=false;url:string;constructor(url:string){this.url=url;sources.push(this);}close(){this.closed=true;}}});
  const {useApp,connect}=await import("../frontend/src/web/store.ts"),original=useApp.getState();let disconnect=()=>{};
  try{
    useApp.setState({activeId:"active",subagentId:"",loadModels:async()=>{}});disconnect=connect();
    const ids=(source:any)=>new URL(source.url,"http://localhost").searchParams.getAll("sessionId").filter(Boolean);
    assert.deepEqual(ids(sources[0]),["active"]);
    useApp.setState({error:"unrelated update"});assert.equal(sources.length,1);
    useApp.setState({subagentId:"child"});assert.equal(sources[0].closed,true);assert.deepEqual(ids(sources[1]),["active","child"]);
    useApp.setState({activeId:"next",subagentId:""});assert.equal(sources[1].closed,true);assert.deepEqual(ids(sources[2]),["next"]);
    sources[2].onopen();assert.equal(useApp.getState().connected,true);
    sources[0].onerror();sources[0].onmessage({data:JSON.stringify({type:"notice",data:{message:"stale"}})});
    assert.equal(useApp.getState().connected,true);assert.equal(useApp.getState().error,"unrelated update");
    sources[2].onerror();assert.equal(useApp.getState().connected,false);sources[2].onopen();assert.equal(useApp.getState().connected,true);
    sources[2].onmessage({data:JSON.stringify({type:"notice",sessionId:"background",data:{message:"background notice"}})});
    assert.equal(useApp.getState().error,"background notice");
    disconnect();assert.equal(sources[2].closed,true);useApp.setState({activeId:"after cleanup"});assert.equal(sources.length,3);
    useApp.setState({browserRequest:undefined});useApp.getState().receive({type:"browser",sessionId:"after cleanup",data:{id:"managed-tab"}});assert.deepEqual(useApp.getState().browserRequest,{id:"managed-tab",version:1});
    useApp.getState().receive({type:"browser",sessionId:"background",data:{id:"other-tab"}});assert.equal(useApp.getState().browserRequest?.id,"managed-tab");
    useApp.getState().receive({type:"browser",sessionId:"after cleanup",data:{id:"managed-tab"}});assert.equal(useApp.getState().browserRequest?.version,2);
  }finally{
    disconnect();useApp.setState(original);
    for(const key of Object.keys(saved)){if(saved[key])Object.defineProperty(globalThis,key,saved[key]!);else Reflect.deleteProperty(globalThis,key);}
  }
});

test("web streaming: coalesce updates, flush final messages and discard pending refreshes on switch",async t=>{
  const saved=Object.getOwnPropertyDescriptor(globalThis,"localStorage");
  Object.defineProperty(globalThis,"localStorage",{configurable:true,value:{getItem:()=>null,setItem(){},removeItem(){}}});
  const {useApp}=await import("../frontend/src/web/store.ts"),original=useApp.getState();
  t.mock.timers.enable({apis:["setTimeout"]});let notifications=0;
  useApp.setState({activeId:"stream",snapshot:{messages:[],execution:{runningTools:[]}} as any});
  const unsubscribe=useApp.subscribe(()=>notifications++);
  try{
    const update=(type:string,text:string)=>useApp.getState().receive({type:"event",sessionId:"stream",data:{type,message:{role:"assistant",timestamp:1,content:text}}});
    for(let i=0;i<100;i++)update("message_update",String(i));
    assert.equal(notifications,0);assert.equal(useApp.getState().snapshot!.messages.at(-1)!.content,"99");
    t.mock.timers.tick(31);assert.equal(notifications,0);t.mock.timers.tick(1);assert.equal(notifications,1);
    update("message_update","pending");update("message_end","complete");assert.equal(notifications,2);
    t.mock.timers.tick(32);assert.equal(notifications,2,"message end cancels delayed duplicate notifications");
    assert.equal(useApp.getState().snapshot!.messages.at(-1)!.content,"complete");assert.equal(useApp.getState().snapshot!.execution!.streaming,undefined);
    update("message_update","old session");useApp.setState({activeId:"next",snapshot:undefined});assert.equal(notifications,3);
    t.mock.timers.tick(32);assert.equal(notifications,3);assert.equal(useApp.getState().snapshot,undefined);
  }finally{unsubscribe();useApp.setState(original);if(saved)Object.defineProperty(globalThis,"localStorage",saved);else Reflect.deleteProperty(globalThis,"localStorage");}
});

test("web retries: traces update parent and child status independently and clear on recovery or stop",async()=>{
  const saved=Object.getOwnPropertyDescriptor(globalThis,"localStorage");
  Object.defineProperty(globalThis,"localStorage",{configurable:true,value:{getItem:()=>null,setItem(){},removeItem(){}}});
  const {useApp}=await import("../frontend/src/web/store.ts"),original=useApp.getState();
  useApp.setState({activeId:"parent",subagentId:"child",snapshot:{messages:[],execution:{runningTools:[]}} as any,subagentSnapshot:{messages:[],execution:{runningTools:[]}} as any});
  const trace=(sessionId:string,type:string,data:unknown={})=>{const entry={id:`${sessionId}/${type}`,sessionId,runId:"run",type,label:type,at:new Date().toISOString(),data};useApp.getState().receive({type:"trace",sessionId,data:entry});return entry;};
  try{
    const retry=trace("parent","auto_retry_start",{attempt:1,maxAttempts:3,delayMs:2000,errorMessage:"fetch failed"});
    assert.deepEqual(useApp.getState().snapshot?.execution?.retry,retry);
    const childRetry=trace("child","auto_retry_start",{attempt:2,maxAttempts:3,delayMs:4000,errorMessage:"503 unavailable"});
    trace("background","auto_retry_start");assert.deepEqual(useApp.getState().snapshot?.execution?.retry,retry);assert.deepEqual(useApp.getState().subagentSnapshot?.execution?.retry,childRetry);
    trace("parent","auto_retry_end",{success:true});assert.equal(useApp.getState().snapshot?.execution?.retry,undefined);assert.deepEqual(useApp.getState().subagentSnapshot?.execution?.retry,childRetry);
    useApp.getState().receive({type:"event",sessionId:"child",data:{type:"agent_settled"}});assert.equal(useApp.getState().subagentSnapshot?.execution?.retry,undefined);
    trace("parent","auto_retry_start",retry.data);trace("parent","auto_retry_end",{success:false});assert.equal(useApp.getState().snapshot?.execution?.retry,undefined);
    trace("parent","auto_retry_start",retry.data);trace("parent","run_start");assert.equal(useApp.getState().snapshot?.execution?.retry,undefined);
  }finally{useApp.setState(original);if(saved)Object.defineProperty(globalThis,"localStorage",saved);else Reflect.deleteProperty(globalThis,"localStorage");}
});

test("settings errors: refresh failures reach the caller and OAuth errors stay in their dialog",async t=>{
  const saved=Object.getOwnPropertyDescriptor(globalThis,"localStorage");
  Object.defineProperty(globalThis,"localStorage",{configurable:true,value:{getItem:()=>null,setItem(){},removeItem(){}}});
  const {useApp}=await import("../frontend/src/web/store.ts"),original=useApp.getState();
  t.mock.method(globalThis,"fetch",async()=>new Response(JSON.stringify({error:"settings operation failed"}),{status:500}));
  try{
    useApp.setState({error:"existing conversation error",auth:undefined});
    await assert.rejects(useApp.getState().loadModels(true),/settings operation failed/);
    await assert.rejects(useApp.getState().refresh(),/settings operation failed/);
    useApp.getState().receive({type:"auth",data:{provider:"openai",status:"error",message:"OAuth state mismatch"}});
    assert.equal(useApp.getState().auth?.message,"OAuth state mismatch");
    assert.equal(useApp.getState().error,"existing conversation error","settings must not overwrite unrelated conversation feedback");
  }finally{useApp.setState(original);if(saved)Object.defineProperty(globalThis,"localStorage",saved);else Reflect.deleteProperty(globalThis,"localStorage");}
});

test("progress snapshots: skip unwatched sessions, retain watched parents and recheck disconnections",async t=>{
  const {Runtime}=await import("./runtime.ts");t.mock.timers.enable({apis:["setTimeout"]});
  const records={parent:{},child:{parentId:"parent"},background:{}};
  const watched=Object.assign(()=>{}, {sessions:new Set(["parent"])}),live=new Map(Object.keys(records).map(id=>[id,{}]));
  const runtime=Object.assign(Object.create(Runtime.prototype),{live,listeners:new Set([watched]),store:{record:(id:keyof typeof records)=>records[id]},closing:false,snapshot:async(id:string)=>({id}),emit:()=>{}});
  const snapshots=t.mock.method(runtime,"snapshot");
  runtime.progress("background");assert.equal((live.get("background") as any).progressTimer,undefined);
  runtime.progress("child");runtime.progress("child");t.mock.timers.tick(100);await Promise.resolve();
  assert.deepEqual(snapshots.mock.calls.map(call=>call.arguments[0]),["parent"],"child progress refreshes its watched parent's aggregate once");
  runtime.progress("parent");runtime.listeners.delete(watched);t.mock.timers.tick(100);await Promise.resolve();assert.equal(snapshots.mock.callCount(),1);
  runtime.listeners.add(()=>{});runtime.progress("background");t.mock.timers.tick(100);await Promise.resolve();assert.equal(snapshots.mock.callCount(),2,"legacy unfiltered clients retain full progress");
});

test("OpenAI OAuth: production login persists and reuses the installation UUID",{timeout:45000},async t=>{
  const temp=await realpath(await mkdtemp(join(tmpdir(),"pi-desk-oauth-device-"))),agent=join(temp,"agent");await mkdir(agent);
  await writeFile(join(agent,"settings.json"),JSON.stringify({retry:{enabled:false}}));
  let child:ReturnType<typeof spawn>|undefined,streamAbort:AbortController|undefined,deviceId="";
  t.after(async()=>{streamAbort?.abort();if(child)await stopChild(child);});
  for(let attempt=0;attempt<2;attempt++){
    child=spawn(process.execPath,[resolve("dist/server.js"),"--port","0","--no-open"],{windowsHide:true,env:{...process.env,PI_CODING_AGENT_DIR:agent,PI_DESK_DIR:join(temp,"desk"),PI_OAUTH_CALLBACK_HOST:"127.0.0.1"},stdio:["ignore","pipe","pipe"]});
    const server=child;let output="",errors="";server.stderr!.on("data",chunk=>errors+=chunk);
    const origin=await new Promise<string>((ok,fail)=>{const timer=setTimeout(()=>fail(Error(errors||"OAuth fixture startup timed out")),15000);server.stdout!.on("data",chunk=>{output+=chunk;const match=/http:\/\/127\.0\.0\.1:\d+/.exec(output);if(match){clearTimeout(timer);ok(match[0]);}});server.once("exit",code=>{clearTimeout(timer);fail(Error(`server exit ${code}: ${errors}`));});});
    streamAbort=new AbortController();const response=await fetch(origin+"/api/events",{signal:AbortSignal.any([streamAbort.signal,AbortSignal.timeout(15000)])}),reader=response.body!.getReader(),decoder=new TextDecoder();let buffer="";
    const next=async(type:string):Promise<any>=>{for(;;){const end=buffer.indexOf("\n\n");if(end>=0){const block=buffer.slice(0,end);buffer=buffer.slice(end+2);if(!block.startsWith("data: "))continue;const event=JSON.parse(block.slice(6));if(event.type===type)return event.data;if(event.type==="notice"&&event.data.type==="error")throw Error(event.data.message);continue;}const chunk=await reader.read();assert.equal(chunk.done,false,"OAuth event stream ended early");buffer+=decoder.decode(chunk.value,{stream:true});}};
    const post=async(path:string,data:unknown)=>{const response=await fetch(origin+"/api"+path,{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify(data)});assert.equal(response.status,200);return response.json();};
    assert.equal((await post("/models",{provider:"openai",operation:"oauth"})).started,true);
    const auth=await next("auth"),url=new URL(auth.url),settings=JSON.parse(await readFile(join(agent,"settings.json"),"utf8"));
    assert.match(settings.deviceId,/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i);
    assert.equal(url.hostname,"auth.openai.com");assert.equal(url.searchParams.get("ext_agent_host_id"),`urn:uuid:${settings.deviceId}`);assert.equal(url.searchParams.get("agent_name_hint"),"Pi Desk");assert.equal(settings.retry.enabled,false);
    if(deviceId)assert.equal(settings.deviceId,deviceId,"restarting and retrying keeps the same installation identity");deviceId=settings.deviceId;
    const prompt=await next("ui");assert.equal(prompt.url,auth.url);assert.deepEqual(prompt.oauth,{provider:"openai",kind:"manual_code",placeholder:"http://127.0.0.1:1455/auth/callback"});
    if(attempt===0)await post(`/ui/${prompt.id}`,{});else await post("/models",{provider:"openai",operation:"cancel_oauth"});
    const ended=await next("auth");assert.equal(ended.status,attempt===0?"error":"cancelled");assert.match(ended.message,/取消|cancel/i);
    await post("/preferences",{theme:"light"});await next("state"); // Drain through a marker; any duplicate global error notice fails next().
    streamAbort.abort();await reader.cancel().catch(()=>{});await stopChild(server);child=undefined;
  }
});

test("production persistence: acknowledged data survives SIGKILL and an interrupted temporary write",{timeout:60000},async t=>{
  const temp=await realpath(await mkdtemp(join(tmpdir(),"pi-desk-durability-"))),project=join(temp,"project"),agent=join(temp,"agent"),desk=join(temp,"desk");
  await mkdir(project);await mkdir(agent);
  let requests=0;
  const model=createServer(async(req,res)=>{for await(const _ of req){}requests++;res.writeHead(200,{"Content-Type":"text/event-stream"});
    for(const chunk of [{choices:[{index:0,delta:{role:"assistant",content:"persisted reply"},finish_reason:null}]},{choices:[{index:0,delta:{},finish_reason:"stop"}]},{choices:[],usage:{prompt_tokens:10,completion_tokens:3,total_tokens:13}}])res.write(`data: ${JSON.stringify({id:"durable",object:"chat.completion.chunk",created:1,model:"mock",...chunk})}\n\n`);
    res.end("data: [DONE]\n\n");});
  model.listen(0,"127.0.0.1");await once(model,"listening");
  await writeFile(join(agent,"models.json"),JSON.stringify({providers:{mock:{api:"openai-completions",baseUrl:`http://127.0.0.1:${(model.address() as {port:number}).port}/v1`,apiKey:"local-fixture",models:[{id:"mock",contextWindow:100000,maxTokens:1000}]}}}));
  await writeFile(join(agent,"settings.json"),JSON.stringify({defaultProvider:"mock",defaultModel:"mock",retry:{enabled:false},compaction:{enabled:false}}));
  const {pathToFileURL}=await import("node:url"),crashPreload=join(temp,"crash-write.mjs");
  await writeFile(crashPreload,`import fs from 'node:fs';import {resolve} from 'node:path';import {syncBuiltinESMExports} from 'node:module';const write=fs.writeFileSync;fs.writeFileSync=function(path,contents,options){if(String(path)===resolve(process.env.PI_DESK_DIR,'state.json.tmp')&&String(contents).includes('interrupted draft fixture')){write(path,String(contents).slice(0,20),options);process.kill(process.pid,'SIGKILL');}return write(path,contents,options);};syncBuiltinESMExports();`);
  let child:ReturnType<typeof spawn>|undefined,origin="";
  t.after(async()=>{if(child)await stopChild(child);model.close();model.closeAllConnections();});
  const start=async(crash=false)=>{
    child=spawn(process.execPath,[...(crash?["--import",pathToFileURL(crashPreload).href]:[]),resolve("dist/server.js"),"--port","0","--no-open"],{windowsHide:true,env:{...process.env,PI_DESK_DIR:desk,PI_CODING_AGENT_DIR:agent,PI_CODING_AGENT_SESSION_DIR:join(temp,"sessions")},stdio:["ignore","pipe","pipe"]});
    origin=await new Promise<string>((ok,fail)=>{let output="",errors="";const timer=setTimeout(()=>fail(Error(errors||"startup timeout")),20000);child!.stderr!.on("data",chunk=>errors+=chunk);child!.stdout!.on("data",chunk=>{output+=chunk;const match=/http:\/\/127\.0\.0\.1:\d+/.exec(output);if(match){clearTimeout(timer);ok(match[0]);}});child!.once("exit",code=>{clearTimeout(timer);fail(Error(`server exit ${code}: ${errors}`));});});
  };
  const request=async(path:string,data?:unknown,method=data===undefined?"GET":"POST")=>{
    const response=await fetch(origin+"/api"+path,{method,headers:data===undefined?undefined:{"Content-Type":"application/json"},body:data===undefined?undefined:JSON.stringify(data)});
    const value=await response.json();assert.ok(response.ok,JSON.stringify(value));return value;
  };
  await start();assert.equal((await request("/state")).preferences.maxRetries??3,3);await request("/preferences",{maxRetries:2});
  const workspace=await request("/workspaces",{path:project}),session=await request("/sessions",{workspaceId:workspace.id,mode:"minimal"});
  await request(`/sessions/${session.id}/prompt?wait=1`,{text:"original durable prompt"});
  const reply=(await request(`/sessions/${session.id}`)).messages.find((message:any)=>message.role==="assistant");
  await request(`/sessions/${session.id}/message`,{operation:"edit",entryId:reply.entryId,text:"edited durable reply"});
  const image=Buffer.from("durable attachment fixture").toString("base64");
  await request(`/sessions/${session.id}`,{title:"Durable title",pinned:true,draft:"durable unsent draft",draftImages:[{id:"image",name:"draft.png",type:"image",mimeType:"image/png",data:image}]},"PATCH");
  await request(`/workspaces/${workspace.id}`,{pinned:true},"PATCH");
  await request(`/sessions/${session.id}/queue`,{operation:"replace",pending:[{id:"held",text:"durable paused queue",behavior:"followUp",images:[]}]});
  await request("/preferences",{theme:"dark"});
  const exited=once(child!,"exit");child!.kill("SIGKILL");await exited;
  const saved=JSON.parse(await readFile(join(desk,"state.json"),"utf8")),record=saved.sessions.find((item:any)=>item.id===session.id);
  assert.equal(record.draft,"durable unsent draft");assert.equal(record.lastStats.tokens.output,3);
  await writeFile(join(desk,"state.json.tmp"),'{"interrupted temporary file":');
  await start();const state=await request("/state"),restored=state.sessions.find((item:any)=>item.id===session.id),composer=await request(`/sessions/${session.id}/composer`),snapshot=await request(`/sessions/${session.id}`);
  assert.equal(state.preferences.maxRetries,2,"retry limit survives a production restart");
  assert.equal(state.preferences.theme,"dark");assert.equal(restored.title,"Durable title");assert.equal(restored.pinned,true);assert.equal(restored.status,"idle");
  assert.equal(state.workspaces.find((item:any)=>item.id===workspace.id).pinned,true);
  assert.equal(composer.text,"durable unsent draft");assert.equal(composer.images[0].data,image);assert.equal(composer.pending[0].text,"durable paused queue");assert.equal(composer.pending[0].paused,true);
  assert.equal(snapshot.stats.tokens.output,3);assert.ok(snapshot.messages.some((message:any)=>JSON.stringify(message.content).includes("edited durable reply")));assert.equal(requests,1,"restart does not replay held prompts");
  await request(`/sessions/${session.id}`,{draft:"saved after crash recovery"},"PATCH");
  assert.equal(JSON.parse(await readFile(join(desk,"state.json"),"utf8")).sessions.find((item:any)=>item.id===session.id).draft,"saved after crash recovery");
  const stopped=once(child!,"exit");child!.kill("SIGKILL");await stopped;await start(true);
  const interrupted=once(child!,"exit");await assert.rejects(request(`/sessions/${session.id}`,{draft:"interrupted draft fixture"},"PATCH"));await interrupted;
  assert.equal(JSON.parse(await readFile(join(desk,"state.json"),"utf8")).sessions.find((item:any)=>item.id===session.id).draft,"saved after crash recovery","a partial temporary write must not replace acknowledged data");
  const temporary=await readFile(join(desk,"state.json.tmp"),"utf8");assert.throws(()=>JSON.parse(temporary),SyntaxError);
  await start();const recovered=await request(`/sessions/${session.id}/composer`);
  assert.equal(recovered.text,"saved after crash recovery");assert.equal(recovered.images[0].data,image);assert.equal(recovered.pending[0].text,"durable paused queue");
});

test("production HTTP: local origin boundary, default-enabled workspace, SSE reconnect, background task, PTY", { timeout: 120000 }, async t => {
  const temp = await realpath(await mkdtemp(join(tmpdir(), "pi-desk-web-"))); const cwd = join(temp, "project & space 'quoted'"); const agent = join(temp, "agent");
  await mkdir(cwd); await mkdir(agent); await mkdir(join(temp,"older")); await writeFile(join(cwd, "hello.txt"), "workspace preview"); await writeFile(join(temp, "outside.txt"), "outside-workspace");
  const proxyHits:string[][]=[[],[]],proxyAuth:string[]=[],proxyPorts=new Map<number,number>();
  const mock = createServer(async (req, res) => { if(req.method==="GET"&&req.url==="/redirect"){res.writeHead(302,{Location:"/sidebar"}).end();return;}if(req.method==="GET"&&req.url==="/sidebar"){res.writeHead(200,{"Content-Type":"text/html"});res.end("<title>Sidebar fixture</title><h1>Embedded link fixture</h1>");return;}if(req.method==="GET"&&req.url==="/v1/models"){const route=proxyPorts.get(req.socket.remotePort!);if(route!==undefined)proxyHits[route].push(req.url);res.writeHead(200,{"Content-Type":"application/json"});res.end(JSON.stringify({data:[{id:"discovered-one"},{id:"discovered-two",contextWindow:64000,maxTokens:1000}]}));return;} let body="";for await (const chunk of req)body+=chunk; res.writeHead(200, { "Content-Type": "text/event-stream" }); await new Promise(resolve => setTimeout(resolve, body.includes("workspace live order fixture")?1500:250));
    const route=proxyPorts.get(req.socket.remotePort!);if(route!==undefined)proxyHits[route].push(req.url!);
    if(req.url?.includes("streamGenerateContent")){res.end(`data: ${JSON.stringify({candidates:[{content:{role:"model",parts:[{text:"Google proxy verified"}]},finishReason:"STOP"}],usageMetadata:{promptTokenCount:10,candidatesTokenCount:3,totalTokenCount:13}})}\n\n`);return;}
    for (const chunk of [{ choices: [{ index: 0, delta: { role: "assistant", content: "HTTP SDK verified" }, finish_reason: null }] }, { choices: [{ index: 0, delta: {}, finish_reason: "stop" }] }, { choices: [], usage: { prompt_tokens: 10, completion_tokens: 3, total_tokens: 13 } }]) res.write(`data: ${JSON.stringify({ id: "http", object: "chat.completion.chunk", created: 1, model: "mock", ...chunk })}\n\n`);
    res.end("data: [DONE]\n\n"); });
  mock.listen(0, "127.0.0.1"); await once(mock, "listening"); const modelPort = (mock.address() as { port: number }).port;
  await writeFile(join(agent, "models.json"), JSON.stringify({ providers: { mock: { api: "openai-completions", baseUrl: `http://127.0.0.1:${modelPort}/v1`, apiKey: "local-test", models: [{ id: "mock", contextWindow: 100000, maxTokens: 1000 }] } } }));
  await writeFile(join(agent, "settings.json"), JSON.stringify({ defaultProvider: "mock", defaultModel: "mock", retry: { enabled: false }, skills:[join(temp,"explicit-skill")], packages:["npm:@pi-desk/fixture-never-installed"] }));
  const skillFixtures=[
    [join(agent,"skills","pi-desk-shared"),"pi-desk-shared","User description"],
    [join(cwd,".pi","skills","pi-desk-shared"),"pi-desk-shared","Project description"],
    [join(cwd,".agents","skills","pi-desk-agents"),"pi-desk-agents","Agents directory description"],
    [join(temp,"explicit-skill"),"pi-desk-explicit","Configured description"],
  ];
  for(const [dir,name,description] of skillFixtures){await mkdir(dir,{recursive:true});await writeFile(join(dir,"SKILL.md"),`---\nname: ${name}\ndescription: ${description}\n---\n# Skill details\n\nFull body fixture: <script>window.skillExecuted=true</script>\n`);}
  const skillExtensionMarker=join(temp,"skill-list-must-not-execute-extensions");await mkdir(join(cwd,".pi","extensions"),{recursive:true});await writeFile(join(cwd,".pi","extensions","skill-probe.ts"),`import {writeFileSync} from 'node:fs';export default function(){writeFileSync(${JSON.stringify(skillExtensionMarker)},'executed');}`);
  const openerBin=join(temp,"opener-bin"),openerArgs=join(temp,"opener-args"),openerCwd=join(temp,"opener-cwd");
  if(process.platform==="linux") { await mkdir(openerBin); await writeFile(join(openerBin,"xdg-open"),'#!/bin/sh\nprintf "%s\\n" "$@" > "$PI_DESK_OPEN_TEST_ARGS"\npwd > "$PI_DESK_OPEN_TEST_CWD"\n',{mode:0o755}); }
  const startupSession=(id:string,workspaceId:string,updatedAt:string)=>({id,workspaceId,title:id,cwd:workspaceId==="older"?join(temp,"older"):cwd,mode:"minimal",status:"idle",depth:0,draft:"",createdAt:"2020-01-01T00:00:00.000Z",updatedAt});
  await mkdir(join(temp,"desk"));await writeFile(join(temp,"desk","state.json"),JSON.stringify({version:1,preferences:{theme:"light"},sessions:[startupSession("older-history","older","2021-01-01T00:00:00.000Z"),startupSession("newest-history","ssh-menu","2022-01-01T00:00:00.000Z"),startupSession("old-remote-history","ssh-menu","2020-01-01T00:00:00.000Z")],workspaces:[{id:"empty",name:"Empty fixture",path:join(temp,"empty"),trusted:true},{id:"older",name:"Older fixture",path:join(temp,"older"),trusted:true},{id:"ssh-menu",name:"SSH menu fixture",path:"/srv/fixture",trusted:false,remote:{target:"fixture@127.0.0.1",port:2222,historyImported:true}}]}));
  const child = spawn(process.execPath, [resolve("dist/server.js"), "--port", "0", "--no-open"], { cwd: process.cwd(), windowsHide: true, env: { ...process.env, ...(process.platform==="linux"?{PATH:openerBin+":"+process.env.PATH,PI_DESK_OPEN_TEST_ARGS:openerArgs,PI_DESK_OPEN_TEST_CWD:openerCwd}:{}), PI_CODING_AGENT_DIR: agent, PI_DESK_DIR: join(temp, "desk") }, stdio: ["ignore", "pipe", "pipe"] });
  t.after(async () => { mock.close(); mock.closeAllConnections(); await stopChild(child); });
  let output = ""; let errors = ""; child.stderr.on("data", chunk => { errors += chunk.toString(); });
  const origin = await new Promise<string>((resolve, reject) => { const timer = setTimeout(() => reject(new Error("service did not start: " + errors)), 30000); child.stdout.on("data", chunk => { output += chunk.toString(); const match = /http:\/\/127\.0\.0\.1:\d+/.exec(output); if (match) { clearTimeout(timer); resolve(match[0]); } }); child.once("exit", code => { clearTimeout(timer); reject(new Error(`service exited ${code}: ${errors}`)); }); });
  const request = async (path: string, data?: unknown, method = data === undefined ? "GET" : "POST") => { const res = await fetch(origin + "/api" + path, { method, body: data === undefined ? undefined : JSON.stringify(data), headers: data === undefined ? undefined : { "Content-Type": "application/json" } }); return { status: res.status, data: await res.json() }; };
  assert.equal((await fetch(origin)).status, 200);
  assert.deepEqual((await request("/state")).data.workspaces.map((item:any)=>item.id),["ssh-menu","older","empty"],"startup orders workspaces by their newest session and keeps empty workspaces last");
  assert.equal((await request("/workspaces/empty",{pinned:true},"PATCH")).data.pinned,true);
  assert.deepEqual((await request("/state")).data.workspaces.map((item:any)=>item.id),["empty","ssh-menu","older"]);
  assert.equal(JSON.parse(await readFile(join(temp,"desk/state.json"),"utf8")).workspaces[0].pinned,true);
  assert.equal((await request("/workspaces/ssh-menu",{pinned:true},"PATCH")).data.pinned,true);
  assert.deepEqual((await request("/state")).data.workspaces.map((item:any)=>item.id),["ssh-menu","empty","older"],"pinned workspaces retain activity ordering within their group");
  assert.equal((await request("/workspaces/empty",{pinned:"yes",name:"Must not change"},"PATCH")).status,400);
  assert.equal((await request("/state")).data.workspaces.find((item:any)=>item.id==="empty").name,"Empty fixture");
  assert.equal((await request("/workspaces/ssh-menu",{pinned:false},"PATCH")).data.pinned,false);
  assert.equal((await request("/workspaces/empty",{pinned:false},"PATCH")).data.pinned,false);
  assert.deepEqual((await request("/state")).data.workspaces.map((item:any)=>item.id),["ssh-menu","older","empty"],"unpinning restores activity ordering");
  const sshInfo=await request("/workspaces/ssh-menu/ssh-info");assert.equal(sshInfo.status,200);assert.equal(sshInfo.data.hostname,"127.0.0.1");assert.equal(sshInfo.data.user,"fixture");assert.equal(sshInfo.data.port,2222);assert.ok(!("identityfile" in sshInfo.data));assert.equal((await request("/state")).data.workspaces[0].remote.connected,false);
assert.equal((await request("/workspaces/ssh-menu/disconnect",{})).status,200);
  assert.equal((await fetch(origin + "/api/state", { headers: { Origin: "https://untrusted.example" } })).status, 403);
  assert.equal((await fetch(origin + "/api/state", { headers: { "Sec-Fetch-Site": "cross-site" } })).status, 403);
  const navigation = await new Promise<number>(resolve => { httpRequest(origin, { headers: { "Sec-Fetch-Site": "cross-site", "Sec-Fetch-Mode": "navigate", "Sec-Fetch-Dest": "document" } }, response => { response.resume(); resolve(response.statusCode!); }).end(); }); assert.equal(navigation, 200);
  const oldPreferences=(await request("/state")).data.preferences;
  assert.equal((await request("/preferences",{maxRetries:5})).data.maxRetries,5);
  for(const maxRetries of [-1,1.5,101,"3",null])assert.equal((await request("/preferences",{maxRetries,theme:"dark"})).status,400);
  assert.deepEqual((await request("/state")).data.preferences,{...oldPreferences,maxRetries:5},"invalid requests must not change any preferences");
  assert.equal((await request("/preferences",{maxRetries:0})).data.maxRetries,0);
  const w = (await request("/workspaces", { path: cwd, trusted: false })).data;
  assert.equal(w.trusted,true);
  for(const resource of ["skills","plugins","mcp"])assert.equal((await request(`/${resource}/${w.id}`)).status,200);
  const session = (await request("/sessions", { workspaceId: w.id, mode: "minimal" })).data;
  const pluginList=await request(`/plugins/${w.id}`);assert.equal(pluginList.status,200);
  assert.ok(pluginList.data.some((p:any)=>p.id==="builtin:subagent"&&p.name==="subagent"&&p.enabled));assert.match((await request(`/plugins/${w.id}?id=builtin%3Asubagent`)).data.text,/function delegate/);
  assert.equal((await request(`/plugins/${w.id}`,{id:"builtin:subagent",enabled:false})).data.plugins.find((p:any)=>p.id==="builtin:subagent").enabled,false);
  assert.equal((await request(`/plugins/${w.id}`,{id:"builtin:subagent",enabled:true})).status,200);
  assert.ok(pluginList.data.some((p:any)=>p.id==="builtin:browser"&&p.name==="desk_browser"&&p.enabled));assert.match((await request(`/plugins/${w.id}?id=builtin%3Abrowser`)).data.text,/class BrowserHost/);
  assert.equal((await request(`/plugins/${w.id}`,{id:"builtin:browser",enabled:false})).data.plugins.find((p:any)=>p.id==="builtin:browser").enabled,false);
  assert.equal((await request("/state")).data.preferences.pluginOverrides["builtin:browser"],false);
  assert.equal((await request(`/plugins/${w.id}`,{id:"builtin:browser",enabled:true})).status,200);
  const plugin=pluginList.data.find((p:any)=>p.name==="skill-probe");assert.ok(plugin&&plugin.type==="项目插件"&&plugin.enabled);
  assert.match((await request(`/plugins/${w.id}?id=${encodeURIComponent(plugin.id)}`)).data.text,/skill-list-must-not-execute/);await assert.rejects(access(skillExtensionMarker));
  assert.equal((await request(`/plugins/${w.id}?id=${encodeURIComponent(join(temp,"outside.txt"))}`)).status,400);
  assert.equal((await request(`/plugins/${w.id}`,{id:plugin.id,enabled:"false"})).status,400);
  assert.equal((await request(`/plugins/${w.id}`,{id:plugin.id,enabled:false})).data.plugins.find((p:any)=>p.id===plugin.id).enabled,false);
  assert.equal((await request("/state")).data.preferences.pluginOverrides[plugin.id],false);await assert.rejects(access(skillExtensionMarker));
  assert.equal((await request(`/plugins/${w.id}`,{id:plugin.id,enabled:true})).status,200);
  const apps=await request("/applications"); assert.equal(apps.status,200); assert.ok(apps.data.every((a:any)=>["file-manager","cursor","vscode","gitbash","powershell"].includes(a.id)&&!("command" in a)));
  assert.equal((await request(`/workspaces/${w.id}/open`,{applicationId:"file-manager;echo bad"})).status,400);
  if(process.platform==="linux") {
    assert.ok(apps.data.some((a:any)=>a.id==="file-manager"));
    assert.equal((await request(`/workspaces/${w.id}/open`,{applicationId:"file-manager",sessionId:session.id})).status,200);
    for(let i=0;i<20;i++){try{await access(openerArgs);break;}catch{await new Promise(resolve=>setTimeout(resolve,25));}}
    assert.equal(await readFile(openerArgs,"utf8"),cwd+"\n");assert.equal((await readFile(openerCwd,"utf8")).trim(),cwd);
  }
  assert.equal((await request(`/sessions/${session.id}`,{pinned:true},"PATCH")).data.pinned,true);
  assert.equal((await request("/state")).data.sessions.find((r:any)=>r.id===session.id).pinned,true);
  assert.equal(JSON.parse(await readFile(join(temp,"desk/state.json"),"utf8")).sessions.find((r:any)=>r.id===session.id).pinned,true);
  assert.equal((await request(`/sessions/${session.id}`,{pinned:"yes"},"PATCH")).status,400);
  assert.equal((await request(`/sessions/${session.id}`,{pinned:false},"PATCH")).data.pinned,false);
  const draftImage = { type: "image", id: "draft", name: "saved.png", mimeType: "image/png", data: Buffer.from("saved image").toString("base64") };
  await request(`/sessions/${session.id}`, { draft: "**Markdown** draft", draftImages: [draftImage] }, "PATCH");
  const composer = (await request(`/sessions/${session.id}/composer`)).data;
  assert.equal(composer.text, "**Markdown** draft"); assert.equal(composer.images[0].data, draftImage.data);
  assert.ok(!("data" in (await request("/state")).data.sessions.find((r: any) => r.id === session.id).draftImages[0]));
  const missing = (await request("/sessions", { workspaceId: w.id, mode: "minimal", model: "mock/removed-model" })).data;
  await request(`/sessions/${missing.id}`, { draft: "preserve this draft" }, "PATCH");
  assert.equal((await request(`/sessions/${missing.id}/prompt`, { text: "preserve this draft" })).status, 400);
  assert.equal((await request(`/sessions/${missing.id}`)).data.record.draft, "preserve this draft");
  assert.equal((await request(`/files/${w.id}?path=hello.txt`)).data.text, "workspace preview");
  const traversal = await request(`/files/${w.id}?path=../outside.txt`); assert.equal(traversal.status, 400); assert.ok(!JSON.stringify(traversal.data).includes("outside-workspace"));
  const externalPreview=await request(`/files/${w.id}/preview`,{path:join(temp,"outside.txt")});assert.equal(externalPreview.status,200);assert.equal(externalPreview.data.text,"outside-workspace");
  assert.equal((await request(`/files/${w.id}/preview`,{path:temp})).status,400);
  await writeFile(join(cwd,"unsupported.bin"),Buffer.from([0xff,0,1]));assert.match((await request(`/files/${w.id}/preview`,{path:"unsupported.bin"})).data.unsupported,/暂不支持/);
  await writeFile(join(cwd,"sample.pdf"),"%PDF-1.4\npreview fixture");assert.equal(Buffer.from((await request(`/files/${w.id}/preview`,{path:"sample.pdf"})).data.pdf,"base64").toString(),"%PDF-1.4\npreview fixture");
  await writeFile(join(cwd,"large.txt"),Buffer.alloc(2*1024*1024+1,97));assert.match((await request(`/files/${w.id}?path=large.txt`)).data.unsupported,/2 MiB/);
  await mkdir(join(cwd,"html"));await writeFile(join(cwd,"html","index.html"),'<link rel="stylesheet" href="style.css"><h1 id="result">Loading</h1><script type="module" src="main.js"></script>');await writeFile(join(cwd,"html","style.css"),'h1 { color: rgb(12, 34, 56); }');await writeFile(join(cwd,"html","main.js"),'document.querySelector("h1").textContent="HTML fixture ready";');
  const htmlPreview=(await request(`/files/${w.id}/preview`,{path:"html/index.html"})).data;assert.match(htmlPreview.html,/^\/preview\//);const htmlRoot=htmlPreview.html.slice(0,htmlPreview.html.lastIndexOf("/")+1);
  const htmlResponse=await fetch(origin+htmlPreview.html,{headers:{Origin:"null","Sec-Fetch-Site":"cross-site"}});assert.equal(htmlResponse.status,200);assert.match(htmlResponse.headers.get("content-security-policy")!,/sandbox allow-scripts/);assert.equal(htmlResponse.headers.get("x-frame-options"),null);assert.equal((await fetch(origin+htmlRoot+"main.js",{headers:{Origin:"null"}})).headers.get("access-control-allow-origin"),"null");
  assert.equal((await fetch(origin+htmlRoot+"..%2fhello.txt")).status,403);assert.equal((await fetch(origin+htmlRoot+"../index.html")).status,404);assert.equal((await fetch(origin+htmlPreview.html,{method:"POST"})).status,404);assert.equal((await fetch(origin+"/api/state",{headers:{Origin:"null"}})).status,403);
  for(const url of ["file:///tmp/secret","javascript:alert(1)","https://user:secret@example.com/"])assert.equal((await request("/open-url",{url})).status,400);
  assert.ok((await request(`/files/${w.id}?search=hltxt`)).data.matches.includes("hello.txt"));
  await t.test("message links select previews when the sidebar is closed or showing Start",{skip:process.platform!=="win32"},async()=>{
    const {chromium}=await import("playwright-core"),browser=await chromium.launch({channel:"msedge",headless:true});
    const linked=(await request("/sessions",{workspaceId:w.id,mode:"minimal"})).data,page=await browser.newPage({viewport:{width:1440,height:900}});let external=0;const errors:string[]=[];page.on("pageerror",error=>errors.push(error.message));
    try{
      await request(`/sessions/${linked.id}/prompt`,{text:`[文件](hello.txt) [网页](http://127.0.0.1:${modelPort}/sidebar) [缺失文件](missing.txt) [HTML](html/index.html) [二进制](unsupported.bin) [重定向](http://127.0.0.1:${modelPort}/redirect) 和 \`hello.txt\``});
      for(let i=0;i<60&&(await request(`/sessions/${linked.id}`)).data.record.status==="running";i++)await new Promise(resolve=>setTimeout(resolve,50));
      await page.addInitScript(id=>{if(window!==window.top)return;localStorage.setItem("pi-desk.active",id);localStorage.setItem("pi-desk.inspector","false");},linked.id);
      await page.route("**/api/open-url",async route=>{external++;await route.fulfill({status:200,contentType:"application/json",body:'{"opened":true}'});});
      await page.goto(origin);await page.getByRole("button",{name:"文件",exact:true}).click();
      const pane=page.getByRole("complementary",{name:"右侧文件与浏览器面板"});await pane.getByRole("tabpanel",{name:"hello.txt",exact:true}).getByText("workspace preview",{exact:true}).waitFor({timeout:10000});
      await pane.getByRole("tab",{name:"开始",exact:true}).click();await page.getByRole("button",{name:"hello.txt",exact:true}).click();assert.equal(await pane.getByRole("tab",{name:"hello.txt",exact:true}).getAttribute("aria-selected"),"true");
      await pane.getByRole("button",{name:"收起右侧面板",exact:true}).click();await page.getByRole("link",{name:"网页",exact:true}).click();await pane.getByRole("tab",{name:"Sidebar fixture",exact:true}).waitFor({timeout:10000});assert.equal(external,0,"web links use the embedded browser");
      await pane.getByRole("button",{name:"收起右侧面板",exact:true}).click();await page.route("**/api/browser",async route=>{if(route.request().method()==="GET")await new Promise(resolve=>setTimeout(resolve,500));await route.continue().catch(()=>{});});
      await page.getByRole("button",{name:"文件",exact:true}).click();await pane.getByRole("tabpanel",{name:"hello.txt",exact:true}).waitFor();await page.waitForTimeout(750);assert.equal(await pane.getByRole("tab",{name:"hello.txt",exact:true}).getAttribute("aria-selected"),"true","handled browser requests must not override a later file click");
      await pane.getByRole("button",{name:"收起右侧面板",exact:true}).click();await page.getByRole("button",{name:"缺失文件",exact:true}).click();await pane.getByRole("alert").getByText(/无法预览文件/).waitFor();assert.equal(await pane.getByRole("tabpanel",{name:"开始",exact:true}).count(),0);assert.deepEqual(errors,[]);
      await page.evaluate(()=>{Object.defineProperty(navigator,"clipboard",{configurable:true,value:{writeText:async(value:string)=>{(window as any).copied=value;}}});});
      await page.getByRole("button",{name:"文件",exact:true}).click();await pane.getByRole("tab",{name:"开始",exact:true}).click();
      const fileLink=page.getByRole("button",{name:"文件",exact:true});await fileLink.evaluate(node=>{const range=document.createRange();range.selectNodeContents(node);window.getSelection()!.removeAllRanges();window.getSelection()!.addRange(range);(node as HTMLElement).click();});assert.equal(await pane.getByRole("tab",{name:"开始",exact:true}).getAttribute("aria-selected"),"true");
      assert.equal(await fileLink.evaluate(node=>node.dispatchEvent(new MouseEvent("contextmenu",{bubbles:true,cancelable:true}))),true,"selected text keeps the native menu");assert.equal(await page.getByRole("menu",{name:"文件或链接操作"}).count(),0);await page.evaluate(()=>window.getSelection()!.removeAllRanges());await fileLink.focus();await page.keyboard.press("Space");await pane.getByRole("tab",{name:"hello.txt",exact:true,selected:true}).waitFor();assert.equal(await pane.getByRole("tab",{name:"hello.txt",exact:true}).getAttribute("aria-selected"),"true");
      await fileLink.click({button:"right"});const menu=page.getByRole("menu",{name:"文件或链接操作"});await menu.getByRole("menuitem",{name:"复制文本内容",exact:true}).click();assert.equal(await page.evaluate(()=>(window as any).copied),"workspace preview");
      await fileLink.click({button:"right"});await menu.getByRole("menuitem",{name:"复制完整路径",exact:true}).click();assert.equal(await page.evaluate(()=>(window as any).copied),join(cwd,"hello.txt").replaceAll("\\","/"));
      const previewRoute=`**/api/files/${w.id}/preview`;await page.route(previewRoute,async route=>{await new Promise(resolve=>setTimeout(resolve,200));await route.continue();});await fileLink.evaluate(node=>(node as HTMLElement).style.cssText="position:fixed;right:8px;bottom:8px;z-index:1000");await fileLink.click({button:"right"});await menu.getByRole("menuitem",{name:"复制文本内容",exact:true}).waitFor();await page.waitForFunction(()=>{const box=document.querySelector('[role="menu"][aria-label="文件或链接操作"]')!.getBoundingClientRect();return box.right<=innerWidth-7&&box.bottom<=innerHeight-7;});await page.keyboard.press("Tab");assert.equal(await menu.count(),0);assert.notEqual(await page.evaluate(()=>document.activeElement?.getAttribute("aria-label")),"文件或链接操作");await fileLink.evaluate(node=>(node as HTMLElement).style.cssText="");await page.unroute(previewRoute);
      await fileLink.click({button:"right"});await menu.getByRole("menuitem",{name:"在新标签中预览",exact:true}).click();await pane.getByRole("tab",{name:"hello.txt",exact:true}).nth(1).waitFor();assert.equal(await pane.getByRole("tab",{name:"hello.txt",exact:true}).count(),2);await fileLink.click();assert.equal(await pane.getByRole("tab",{name:"hello.txt",exact:true}).count(),2,"normal file clicks reuse existing previews");
      const launches:any[]=[];await page.route(`**/api/workspaces/${w.id}/open`,async route=>{launches.push(route.request().postDataJSON());await route.fulfill({status:200,contentType:"application/json",body:'{"opened":true}'});});
      await fileLink.click({button:"right"});await menu.getByRole("menuitem",{name:"用应用打开",exact:true}).click();await Promise.all([page.waitForResponse(`**/api/workspaces/${w.id}/open`),page.getByRole("menu",{name:"用应用打开",exact:true}).getByRole("menuitem",{name:"文件资源管理器",exact:true}).click()]);assert.equal(launches.at(-1).path,join(cwd,"hello.txt").replaceAll("\\","/"));
      await pane.getByRole("button",{name:"显示目录树",exact:true}).click();const tree=pane.getByRole("complementary",{name:"目录树",exact:true});await tree.getByRole("button",{name:"hello.txt",exact:true}).click({button:"right"});await Promise.all([page.waitForResponse(`**/api/workspaces/${w.id}/open`),menu.getByRole("menuitem",{name:"在文件管理器中显示",exact:true}).click()]);assert.equal(launches.at(-1).applicationId,"file-manager");await pane.getByRole("button",{name:"隐藏目录树",exact:true}).click();
      await page.getByRole("button",{name:"二进制",exact:true}).click({button:"right"});await menu.getByText("此文件格式暂不支持预览",{exact:true}).waitFor();assert.equal(await menu.getByRole("menuitem",{name:"复制文本内容",exact:true}).count(),0);await page.keyboard.press("Escape");
      const web=page.getByRole("link",{name:"网页",exact:true}),before=(await request("/browser")).data.tabs.length;await web.click();await pane.getByRole("tab",{name:"Sidebar fixture",exact:true}).waitFor();assert.equal((await request("/browser")).data.tabs.length,before,"normal URL clicks reuse the managed browser tab");
      await web.click({button:"right"});await menu.getByRole("menuitem",{name:"复制链接",exact:true}).click();assert.equal(await page.evaluate(()=>(window as any).copied),`http://127.0.0.1:${modelPort}/sidebar`);
      await web.click({button:"right"});await menu.getByRole("menuitem",{name:"在新标签中打开",exact:true}).click();await pane.getByRole("tab",{name:"Sidebar fixture",exact:true}).nth(1).waitFor();assert.equal((await request("/browser")).data.tabs.length,before+1);
      await web.click({button:"right"});await Promise.all([page.waitForResponse("**/api/open-url"),menu.getByRole("menuitem",{name:"用系统默认浏览器打开",exact:true}).click()]);assert.equal(external,1);
      const redirected=page.getByRole("link",{name:"重定向",exact:true});await redirected.click();await pane.getByRole("tab",{name:"Sidebar fixture",exact:true}).nth(2).waitFor();const redirectedCount=(await request("/browser")).data.tabs.length;await redirected.click();await pane.getByRole("tab",{name:"Sidebar fixture",exact:true,selected:true}).waitFor();assert.equal((await request("/browser")).data.tabs.length,redirectedCount,"redirected links reuse the original requested URL");
      await page.getByRole("button",{name:"HTML",exact:true}).click();const html=page.frameLocator('iframe[title="HTML 网页预览"]');await html.getByRole("heading",{name:"HTML fixture ready",exact:true}).waitFor();assert.equal(await html.getByRole("heading").evaluate(node=>getComputedStyle(node).color),"rgb(12, 34, 56)");assert.equal(await html.getByRole("heading").evaluate(()=>{try{return Boolean(parent.document.body);}catch{return false;}}),false,"HTML scripts cannot access the app document");
      await pane.getByRole("button",{name:"文件显示方式",exact:true}).click();await page.getByRole("menuitemradio",{name:"源代码",exact:true}).click();assert.equal(await pane.locator('iframe[title="HTML 网页预览"]').count(),0);assert.match((await pane.getByRole("tabpanel",{name:"index.html",exact:true}).textContent())!,/main.js/);await pane.getByRole("button",{name:"文件显示方式",exact:true}).click();await page.getByRole("menuitemradio",{name:"网页效果",exact:true}).click();await html.getByRole("heading",{name:"HTML fixture ready",exact:true}).waitFor();assert.deepEqual(errors,[]);
    }catch(error){await page.screenshot({path:join(temp,"resource-menu-failure.png")}).catch(()=>{});throw error;}finally{await browser.close();await request("/browser",{action:"stop"});await request(`/sessions/${linked.id}`,undefined,"DELETE");}
  });
  const { git } = await import("./subagents.ts"); await git(cwd,["init","-b","main"]); await git(cwd,["config","user.name","Test"]); await git(cwd,["config","user.email","test@localhost"]); await git(cwd,["add","."]); await git(cwd,["commit","-m","fixture"]);
  await writeFile(join(cwd,"hello.txt"),"staged line\n"); await git(cwd,["add","hello.txt"]); await writeFile(join(cwd,"hello.txt"),"unstaged line\n");
  const diffs=(await request(`/git/${w.id}`)).data; assert.match(diffs.staged,/\+staged line/); assert.match(diffs.unstaged,/\+unstaged line/);
  const authSave=await request("/models",{operation:"api_key",provider:"mock",key:"local-test-auth"}); assert.equal(authSave.status,200); assert.ok(!JSON.stringify(authSave.data).includes("local-test-auth")); assert.match(await readFile(join(agent,"auth.json"),"utf8"),/local-test-auth/);
  assert.equal((await request("/models",{operation:"logout",provider:"mock"})).status,200);
  const stateBeforeProbe=(await request("/state")).data;
  const probe=await request("/models/test",{provider:"mock",model:"mock"});assert.equal(probe.status,200,JSON.stringify(probe.data));assert.ok(probe.data.durationMs>=0);
  assert.equal((await request("/models/test",{provider:"mock",model:"missing"})).status,400);
  assert.equal((await request("/models/test",{provider:123,model:"mock"})).status,400);
  assert.deepEqual((await request("/state")).data,stateBeforeProbe,"model connection probes do not create or alter conversations");
  const beforeDraftConfig=await readFile(join(agent,"models.json"),"utf8"),draftProvider={id:"draft-fixture",api:"openai-completions",baseUrl:`http://127.0.0.1:${modelPort}/v1`,apiKey:"draft-secret-key",headers:{"X-Private":"draft-header-secret"}};
  const discovered=await request("/models/discover",{provider:draftProvider});assert.equal(discovered.status,200,JSON.stringify(discovered.data));assert.deepEqual(discovered.data.map((m:any)=>m.id),["discovered-one","discovered-two"]);
  const streamed=await fetch(origin+"/api/models/test?stream=1",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({provider:draftProvider.id,model:"draft-model",prompt:"editable probe prompt",draft:{provider:draftProvider,model:{id:"draft-model",contextWindow:64000,maxTokens:1000}}})});
  assert.equal(streamed.status,200);const probeLogs=await streamed.text(),events=probeLogs.trim().split("\n").map(line=>JSON.parse(line));
  assert.ok(events.some(e=>e.kind==="request"&&JSON.stringify(e.data).includes("editable probe prompt")),probeLogs);assert.ok(events.some(e=>e.kind==="response"&&e.data.status===200));assert.ok(events.some(e=>e.kind==="output"));assert.ok(events.some(e=>e.kind==="result"));assert.ok(!probeLogs.includes("draft-secret-key")&&!probeLogs.includes("draft-header-secret"),"draft credentials never appear in streamed logs");
  assert.equal(await readFile(join(agent,"models.json"),"utf8"),beforeDraftConfig,"discovering and probing drafts do not save configuration");
  assert.equal((await request("/models/selection",{provider:"mock",models:[]})).status,400,"the default model cannot be removed from the picker");
  const imported=await request("/model-config",{operation:"import",createProvider:true,provider:{...draftProvider,id:"import-fixture"},models:discovered.data});assert.equal(imported.status,200,JSON.stringify(imported.data));
  assert.equal((await request("/models/selection",{provider:"import-fixture",models:["discovered-one"]})).status,200);
  const selectedCatalog=(await request("/models")).data;assert.deepEqual(selectedCatalog.models.filter((m:any)=>m.provider==="import-fixture").map((m:any)=>m.id),["discovered-one"]);assert.equal(selectedCatalog.allModels.filter((m:any)=>m.provider==="import-fixture").length,2);
  assert.deepEqual(JSON.parse(await readFile(join(temp,"desk/state.json"),"utf8")).preferences.modelSelection["import-fixture"],["discovered-one"]);
  const settingsWithDefault=await readFile(join(agent,"settings.json"),"utf8"),withoutDefault=JSON.parse(settingsWithDefault);delete withoutDefault.defaultProvider;delete withoutDefault.defaultModel;await writeFile(join(agent,"settings.json"),JSON.stringify(withoutDefault));
  assert.equal((await request("/models/selection",{provider:"mock",models:[]})).status,200);
  const autoSelected=(await request("/sessions",{workspaceId:w.id,mode:"minimal"})).data;
  assert.equal((await request(`/sessions/${autoSelected.id}`)).data.model,"import-fixture/discovered-one","automatic model choice for a new conversation respects imported models");
  await writeFile(join(agent,"settings.json"),settingsWithDefault);assert.equal((await request("/models/selection",{provider:"mock",models:["mock"]})).status,200);
  const beforeInvalidImport=await readFile(join(agent,"models.json"),"utf8");assert.equal((await request("/model-config",{operation:"import",provider:{...draftProvider,id:"import-fixture"},models:[discovered.data[0],{id:"invalid",contextWindow:-1,maxTokens:10}]})).status,400);assert.equal(await readFile(join(agent,"models.json"),"utf8"),beforeInvalidImport);
  assert.equal((await request("/model-config",{operation:"delete_provider",provider:"import-fixture"})).status,200);
  // Model CRUD stays in Pi's native files, keeps secrets private, and defaults affect new sessions.
  const configUrl = "/model-config";
  const nativeModels = JSON.parse(await readFile(join(agent,"models.json"),"utf8")); nativeModels.customRoot = {preserve:true};
  await writeFile(join(agent,"models.json"),JSON.stringify(nativeModels));
  const provider = {id:"custom",api:"openai-completions",baseUrl:`http://127.0.0.1:${modelPort}/v1`,apiKey:"model-config-secret",headers:{"X-Private":"header-config-secret"},compat:{supportsStore:false},customProvider:true};
  const modelConfig = {id:"alpha",name:"Alpha",contextWindow:64000,maxTokens:1000,reasoning:true,input:["text","image"],thinkingLevelMap:{high:"high"},inputLimits:{images:{resize:{maxWidth:1200}}},cost:{input:1,output:2,cacheRead:0,cacheWrite:0},customModel:true};
  const added = await request(configUrl,{operation:"save",createProvider:true,provider,model:modelConfig,compaction:{reserveTokens:0,keepRecentTokens:300}});
  assert.equal(added.status,200,JSON.stringify(added.data)); assert.ok(added.data.catalog.models.some((m:any)=>m.provider==="custom"&&m.id==="alpha"));
  assert.ok(!JSON.stringify(added.data).includes("model-config-secret")); assert.ok(!JSON.stringify(added.data).includes("header-config-secret"));
  const managed=added.data.providers.find((p:any)=>p.id==="custom"); assert.equal(managed.hasApiKey,true); assert.equal(managed.headers["X-Private"],"[已保存]");
  assert.equal((await request(configUrl,{operation:"default",provider:"custom",model:"alpha"})).status,200);
  const newSession=(await request("/sessions",{workspaceId:w.id,mode:"minimal"})).data;
  assert.equal((await request(`/sessions/${newSession.id}`)).data.model,"custom/alpha");
  assert.equal((await request(`/sessions/${session.id}`)).data.model,"mock/mock");
  const modelEdited=await request(configUrl,{operation:"save",originalProvider:"custom",originalModel:"alpha",provider:{...managed,id:"renamed"},model:{...managed.models[0],id:"beta",name:"Beta"},compaction:{reserveTokens:0,keepRecentTokens:400}});
  assert.equal(modelEdited.status,200,JSON.stringify(modelEdited.data)); assert.equal(modelEdited.data.catalog.defaultProvider,"renamed"); assert.equal(modelEdited.data.catalog.defaultModel,"beta");
  let document=JSON.parse(await readFile(join(agent,"models.json"),"utf8")), settings=JSON.parse(await readFile(join(agent,"settings.json"),"utf8"));
  assert.equal(document.customRoot.preserve,true); assert.equal(document.providers.renamed.apiKey,"model-config-secret"); assert.equal(document.providers.renamed.headers["X-Private"],"header-config-secret");
  assert.equal(document.providers.renamed.models[0].cost.output,2); assert.equal(document.providers.renamed.models[0].customModel,true); assert.equal(document.providers.renamed.models[0].inputLimits.images.resize.maxWidth,1200);
  assert.equal(settings.retry.enabled,false); assert.deepEqual(settings.compaction.modelOverrides["renamed/beta"],{reserveTokens:0,keepRecentTokens:400}); assert.ok(!settings.compaction.modelOverrides["custom/alpha"]);
  const beforeInvalid=await readFile(join(agent,"models.json"),"utf8");
  assert.equal((await request(configUrl,{operation:"save",createProvider:true,provider:{...provider,id:"renamed"},model:{...modelConfig,id:"new-model"}})).status,400);
  assert.equal((await request(configUrl,{operation:"save",createProvider:true,provider:{...provider,id:"anthropic"},model:modelConfig})).status,400);
  assert.equal((await request(configUrl,{operation:"save",provider:{...provider,id:"renamed"},model:{...modelConfig,id:"beta"}})).status,400);
  assert.equal((await request(configUrl,{operation:"save",provider:{...provider,id:"bad"},model:{...modelConfig,maxTokens:64001}})).status,400);
  assert.equal((await request(configUrl,{operation:"save",provider:{...provider,id:"bad",baseUrl:"file:///bad"},model:modelConfig})).status,400);
  assert.equal((await request(configUrl,{operation:"save",provider:{...provider,id:"__proto__"},model:modelConfig})).status,400);
  assert.equal((await request(configUrl,{operation:"save",provider:{...provider,id:"bad"},model:{...modelConfig,inputLimits:{images:{resize:{maxWidth:-1}}}}})).status,400);
  assert.equal((await request(configUrl,{operation:"default",provider:"missing",model:"missing"})).status,400);
  assert.equal(await readFile(join(agent,"models.json"),"utf8"),beforeInvalid);
  const renamedProvider=modelEdited.data.providers.find((p:any)=>p.id==="renamed");
  const second=await request(configUrl,{operation:"save",provider:renamedProvider,model:{...modelConfig,id:"gamma"},compaction:{reserveTokens:200}}); assert.equal(second.status,200);
  assert.equal(second.data.providers.find((p:any)=>p.id==="renamed").models.length,2);
  assert.equal((await request(configUrl,{operation:"delete_model",provider:"renamed",model:"beta"})).status,400);
  assert.equal((await request(configUrl,{operation:"default",provider:"mock",model:"mock"})).status,200);
  assert.equal((await request(configUrl,{operation:"delete_model",provider:"renamed",model:"beta"})).status,200);
  settings=JSON.parse(await readFile(join(agent,"settings.json"),"utf8")); assert.equal(settings.defaultModel,"mock"); assert.equal(settings.defaultProvider,"mock"); assert.ok(!settings.compaction.modelOverrides["renamed/beta"]);
  assert.equal((await request(`/sessions/${newSession.id}/prompt`,{text:"deleted model must not run"})).status,400);
  let currentProvider=(await request(configUrl)).data.providers.find((p:any)=>p.id==="renamed");
  assert.deepEqual(currentProvider.models.map((m:any)=>m.id),["gamma"]);
  assert.equal((await request(configUrl,{operation:"save",originalProvider:"renamed",originalModel:"gamma",provider:currentProvider,model:currentProvider.models[0],clearApiKey:true})).status,200);
  assert.equal(JSON.parse(await readFile(join(agent,"models.json"),"utf8")).providers.renamed.apiKey,undefined);
  currentProvider=(await request(configUrl)).data.providers.find((p:any)=>p.id==="renamed");
  assert.equal((await request(configUrl,{operation:"save",originalProvider:"renamed",originalModel:"gamma",provider:{...currentProvider,apiKey:"replacement-secret"},model:currentProvider.models[0],compaction:{reserveTokens:200}})).status,200);
  assert.equal((await request(configUrl,{operation:"default",provider:"renamed",model:"gamma"})).status,200);
  assert.equal((await request(configUrl,{operation:"delete_provider",provider:"renamed"})).status,400);
  assert.equal((await request(configUrl,{operation:"default",provider:"mock",model:"mock"})).status,200);
  assert.equal((await request(configUrl,{operation:"delete_provider",provider:"renamed"})).status,200);
  settings=JSON.parse(await readFile(join(agent,"settings.json"),"utf8")); assert.equal(settings.defaultProvider,"mock"); assert.equal(settings.defaultModel,"mock"); assert.ok(!settings.compaction.modelOverrides["renamed/gamma"]);

  assert.equal((await request(configUrl)).data.providers.some((p:any)=>p.id==="renamed"),false);
  const noAuth=await request(configUrl,{operation:"save",provider:{id:"unconfigured",api:"openai-completions",baseUrl:provider.baseUrl},model:modelConfig}); assert.equal(noAuth.status,200);
  assert.ok(noAuth.data.providers.some((p:any)=>p.id==="unconfigured")); assert.ok(!noAuth.data.catalog.models.some((m:any)=>m.provider==="unconfigured"));
  assert.equal((await request(configUrl,{operation:"default",provider:"unconfigured",model:"alpha"})).status,400);
  assert.equal((await request(configUrl,{operation:"delete_provider",provider:"unconfigured"})).status,200);
  const settingsBeforeCorruption=await readFile(join(agent,"settings.json"),"utf8"), modelsBeforeCorruption=await readFile(join(agent,"models.json"),"utf8");
  await writeFile(join(agent,"settings.json"),JSON.stringify({compaction:[]}));
  assert.equal((await request(configUrl,{operation:"delete_provider",provider:"mock"})).status,400); assert.equal(await readFile(join(agent,"models.json"),"utf8"),modelsBeforeCorruption);
  await writeFile(join(agent,"settings.json"),settingsBeforeCorruption);
  await writeFile(join(agent,"models.json"),JSON.stringify({providers:null}));
  assert.equal((await request(configUrl,{operation:"delete_provider",provider:"mock"})).status,400); assert.equal(await readFile(join(agent,"models.json"),"utf8"),JSON.stringify({providers:null}));
  await writeFile(join(agent,"models.json"),modelsBeforeCorruption);
  const preserved=await readFile(join(agent,"models.json"),"utf8"); await writeFile(join(agent,"models.json"),"{ invalid source");
  assert.equal((await request(configUrl,{operation:"delete_provider",provider:"mock"})).status,400); assert.equal(await readFile(join(agent,"models.json"),"utf8"),"{ invalid source");
  await writeFile(join(agent,"models.json"),"// Pi supports JSON comments and trailing commas\n"+preserved.replace(/}\s*$/,",}"));
  assert.equal((await request(configUrl)).status,200); await writeFile(join(agent,"models.json"),preserved);
  assert.equal((await request(configUrl,{operation:"default",provider:"mock",model:"mock"})).status,200);
  const resourceUrl=`/resources/${w.id}?scope=project`;
  const defaults=await request(resourceUrl);assert.ok(defaults.data.agentDefaults["agents/scout.md"]);
  const workerUrl=resourceUrl+"&path=agents/worker.md",workerDefault=await request(workerUrl);assert.equal(workerDefault.data.builtin,true);await assert.rejects(access(join(cwd,".pi","agents","worker.md")));
  const roleText="---\nname: worker\ndescription: Edited worker\nmodel: mock/mock\n---\nWorker prompt from the editor";
  assert.equal((await request(resourceUrl,{operation:"save",path:"agents/worker.md",text:roleText})).status,200);assert.equal((await request(workerUrl)).data.text,roleText);
  assert.deepEqual((await request(workerUrl)).data.agent,{name:"worker",description:"Edited worker",model:"mock/mock",instructions:"Worker prompt from the editor"});
  assert.equal((await request(resourceUrl,{operation:"save",path:"agents/worker.md",text:"---\nmodel: []\n---\ninvalid",overwrite:true})).status,400);assert.equal((await request(workerUrl)).data.text,roleText);
  assert.equal((await request(resourceUrl,{operation:"save",path:"agents/broken.md",text:"---\nname: [broken\n---\ninvalid"})).status,400);
  assert.equal((await request(resourceUrl,{operation:"save",path:"agents/empty.md",text:"---\nname: empty\n---\n"})).status,400);
  assert.equal((await request(resourceUrl,{operation:"save",path:"agents/../../outside.md",text:"bad"})).status,400);
  const preservedRole=roleText.replace("model: mock/mock","model: mock/mock\ntools: [read, grep]\ncustom: keep");await request(resourceUrl,{operation:"save",path:"agents/worker.md",text:preservedRole,overwrite:true});
  const roleForm={name:"edited-worker",description:"说明：保留引号 \"和换行\"\n第二行",model:"",instructions:"Prompt with --- and : symbols\n\nKeep this exact text."};
  assert.equal((await request(resourceUrl,{operation:"save",path:"agents/worker.md",agent:roleForm,overwrite:true})).status,200);const roleRoundtrip=(await request(workerUrl)).data;assert.deepEqual(roleRoundtrip.agent,roleForm);assert.match(roleRoundtrip.text,/custom: keep/);assert.match(roleRoundtrip.text,/tools:\n\s+- read/);assert.ok(!roleRoundtrip.text.includes("model:"));
  for(const agent of [{...roleForm,name:" "},{...roleForm,instructions:""},{...roleForm,model:[]},[]])assert.equal((await request(resourceUrl,{operation:"save",path:"agents/worker.md",agent,overwrite:true})).status,400);
  assert.deepEqual((await request(workerUrl)).data.agent,roleForm,"invalid form submissions must not alter existing role data");
  assert.equal((await request(resourceUrl,{operation:"save",path:"prompts/form.md",agent:roleForm})).status,400);
  assert.equal((await request(resourceUrl,{operation:"save",path:"agents/new-form.md",agent:{...roleForm,name:"new-form",model:"mock/mock"}})).status,200);assert.equal((await request(resourceUrl+"&path=agents/new-form.md")).data.agent.model,"mock/mock");await request(resourceUrl,{operation:"delete",path:"agents/new-form.md"});
  assert.equal((await request(resourceUrl,{operation:"delete",path:"agents/worker.md"})).status,200);assert.equal((await request(workerUrl)).data.builtin,true);
  const skillsUrl=`/skills/${w.id}`, skillList=await request(skillsUrl);assert.equal(skillList.status,200);
  for(const [,name] of skillFixtures)assert.ok(skillList.data.some((s:any)=>s.name===name));
  const shared=skillList.data.filter((s:any)=>s.name==="pi-desk-shared");assert.equal(shared.length,1);assert.equal(shared[0].description,"Project description");
  assert.equal(shared[0].path,join(cwd,".pi","skills","pi-desk-shared","SKILL.md"));assert.ok(!JSON.stringify(skillList.data).includes("Full body fixture"));
  const skillDetail=await request(skillsUrl+`?path=${encodeURIComponent(shared[0].path)}`);assert.equal(skillDetail.status,200);assert.equal(skillDetail.data.text,await readFile(shared[0].path,"utf8"));
  assert.equal((await request(skillsUrl+`?path=${encodeURIComponent(join(agent,"auth.json"))}`)).status,400);
  assert.equal((await request(resourceUrl,{operation:"save",path:"skills/new/SKILL.md",text:"must not create"})).status,400);
  assert.equal((await request(resourceUrl,{operation:"delete",path:"skills/pi-desk-shared/SKILL.md"})).status,400);assert.equal(await readFile(shared[0].path,"utf8"),skillDetail.data.text);
  assert.ok(skillList.data.every((s:any)=>s.enabled===true));
  const disabledSkill=await request(skillsUrl,{operation:"toggle",path:shared[0].path,enabled:false});assert.equal(disabledSkill.status,200);assert.equal(disabledSkill.data.skills.find((s:any)=>s.name==="pi-desk-shared").enabled,false);
  assert.ok((await request("/state")).data.preferences.disabledSkills.includes("pi-desk-shared"));
  assert.ok(JSON.parse(await readFile(join(temp,"desk","state.json"),"utf8")).preferences.disabledSkills.includes("pi-desk-shared"));
  assert.equal((await request(skillsUrl+`?path=${encodeURIComponent(shared[0].path)}`)).status,200);
  assert.equal((await request(skillsUrl,{operation:"toggle",path:shared[0].path,enabled:"false"})).status,400);
  assert.equal((await request(skillsUrl,{operation:"delete",path:join(agent,"auth.json")})).status,400);
  assert.equal((await request(skillsUrl,{operation:"toggle",path:join(agent,"auth.json"),enabled:false})).status,400);
  const enabledSkill=await request(skillsUrl,{operation:"toggle",path:shared[0].path,enabled:true});assert.equal(enabledSkill.status,200);assert.equal(enabledSkill.data.skills.find((s:any)=>s.name==="pi-desk-shared").enabled,true);
  const deletedSkill=await request(skillsUrl,{operation:"delete",path:shared[0].path});assert.equal(deletedSkill.status,200);await assert.rejects(access(shared[0].path));
  const fallbackSkill=deletedSkill.data.skills.find((s:any)=>s.name==="pi-desk-shared");assert.equal(fallbackSkill.path,join(agent,"skills","pi-desk-shared","SKILL.md"));await access(fallbackSkill.path);
  await assert.rejects(access(skillExtensionMarker));

  assert.equal((await request(resourceUrl,{operation:"save",path:"prompts/review.md",text:"Review this change"})).status,200);
  assert.equal((await request(resourceUrl+"&path=prompts/review.md")).data.text,"Review this change");
  assert.equal((await request(resourceUrl,{operation:"save",path:"prompts/../../outside.md",text:"bad"})).status,400);
  assert.equal((await request(resourceUrl,{operation:"save",path:"prompts/review.md",text:"overwrite"})).status,400);
  await request(resourceUrl,{operation:"mcp",config:{mcpServers:{probe:{command:"echo",env:{TOKEN:"secret-test-value"}}}}});
  const resourceList=(await request(resourceUrl)).data; assert.equal(resourceList.mcp.mcpServers.probe.env.TOKEN,"[已保存]"); assert.ok(!JSON.stringify(resourceList).includes("secret-test-value"));
  await request(resourceUrl,{operation:"mcp",config:resourceList.mcp}); assert.equal(JSON.parse(await readFile(join(cwd,".pi","mcp.json"),"utf8")).mcpServers.probe.env.TOKEN,"secret-test-value");
  {
  const mcpUrl=`/mcp/${w.id}`,mcpFile=join(agent,"mcp.json"),mcpProcess=join(temp,"mcp-manager.pid"),mcpCalled=join(temp,"mcp-manager.called"),mcpScript=join(temp,"mcp-manager.mjs");
  await writeFile(mcpScript,`import {createInterface} from 'node:readline';import {writeFileSync} from 'node:fs';writeFileSync(${JSON.stringify(mcpProcess)},String(process.pid));const lines=createInterface({input:process.stdin});lines.on('close',()=>process.exit());if(process.argv[2]==='fail'){console.error(process.env.TOKEN);process.exit(1);}lines.on('line',line=>{const m=JSON.parse(line);if(m.id===undefined||process.argv[2]==='hang')return;const result=m.method==='initialize'?{protocolVersion:m.params.protocolVersion,capabilities:{tools:{}},serverInfo:{name:'manager-fixture',version:'1.0'},instructions:'Local MCP manager fixture instructions'}:m.method==='tools/list'?(m.params?.cursor?{tools:[{name:'second',description:'Second page',inputSchema:{type:'object',properties:{}}}]}:{tools:[{name:'echo',description:'Echo fixture',inputSchema:{type:'object',properties:{message:{type:'string'}},required:['message']}}],nextCursor:'second-page'}):{};if(m.method==='tools/call')writeFileSync(${JSON.stringify(mcpCalled)},'unexpected');process.stdout.write(JSON.stringify({jsonrpc:'2.0',id:m.id,result})+'\\n');});`);
  const mcpConfig={command:process.execPath,args:[mcpScript],description:"Manager fixture",env:{TOKEN:"mcp-manager-secret-value"}};
  assert.equal((await request(mcpUrl,{operation:"save",scope:"global",name:"manager",definition:mcpConfig})).status,200);
  await mkdir(join(temp,".pi"),{recursive:true});await writeFile(join(temp,".pi","mcp.json"),JSON.stringify({mcpServers:{parent:{command:"must-not-start"}}}));
  const emptyMcpProject=join(temp,"no-mcp-files");await mkdir(emptyMcpProject);const emptyMcpWorkspace=(await request("/workspaces",{path:emptyMcpProject,trusted:true})).data;assert.equal((await request(`/mcp/${emptyMcpWorkspace.id}`)).status,200);await assert.rejects(access(join(emptyMcpProject,".pi")));
  let managed=(await request(mcpUrl)).data;assert.ok(!managed.servers.some((s:any)=>s.name==="parent"));assert.equal(managed.servers.find((s:any)=>s.name==="manager").enabled,true);await assert.rejects(access(mcpProcess));
  let definition=(await request(mcpUrl+"?scope=global&name=manager")).data;assert.equal(definition.definition.env.TOKEN,"[已保存]");assert.ok(!JSON.stringify(managed).includes("mcp-manager-secret-value"));
  assert.equal((await request(mcpUrl,{operation:"save",scope:"project",name:"manager",definition:{enabled:false}})).status,200);
  managed=(await request(mcpUrl)).data;assert.equal(managed.servers.find((s:any)=>s.name==="manager").enabled,false);assert.equal(managed.servers.find((s:any)=>s.name==="manager").overridden,true);
  assert.equal((await request(mcpUrl+"?scope=global")).data.servers.find((s:any)=>s.name==="manager").enabled,true);
  const allMcp=(await request(mcpUrl+"?scope=all")).data;
  assert.deepEqual(allMcp.servers.filter((s:any)=>s.name==="manager").map((s:any)=>[s.scope,s.enabled]),[["project",false],["global",true]]);
  assert.ok(!JSON.stringify(allMcp).includes("mcp-manager-secret-value"));
  await request(mcpUrl,{operation:"toggle",scope:"global",name:"manager",enabled:false});await request(mcpUrl,{operation:"toggle",scope:"project",name:"manager",enabled:true});assert.equal((await request(mcpUrl)).data.servers.find((s:any)=>s.name==="manager").enabled,true);
  await request(mcpUrl,{operation:"delete",scope:"project",name:"manager"});assert.equal((await request(mcpUrl)).data.servers.find((s:any)=>s.name==="manager").enabled,false);
  definition=(await request(mcpUrl+"?scope=global&name=manager")).data;const beforeDebug=await readFile(mcpFile,"utf8");
  const debugged=await request(mcpUrl,{operation:"debug",scope:"global",name:"manager",original:"manager",revision:definition.revision,definition:{...definition.definition,description:"Unsaved debug content"}});assert.equal(debugged.status,200);assert.equal(debugged.data.state,"connected");assert.match(debugged.data.instructions,/manager fixture/);assert.deepEqual(debugged.data.tools.map((t:any)=>t.name),["echo","second"]);assert.ok(debugged.data.tools[0].inputSchema.properties.message);assert.equal(await readFile(mcpFile,"utf8"),beforeDebug);await assert.rejects(access(mcpCalled));
  // SDK shutdown allows 500 ms grace plus 2 s before SIGKILL; allow CI scheduling overhead.
  const waitFor=async(predicate:()=>Promise<boolean>)=>{const deadline=Date.now()+10000;while(Date.now()<deadline){if(await predicate())return;await new Promise(r=>setTimeout(r,30));}throw new Error("MCP fixture cleanup timed out");};const gone=(pid:number)=>{try{process.kill(pid,0);return false;}catch{return true;}};let probePid=Number(await readFile(mcpProcess,"utf8"));await waitFor(async()=>gone(probePid));
  const failed=await request(mcpUrl,{operation:"debug",scope:"global",name:"manager",original:"manager",definition:{...definition.definition,args:[mcpScript,"fail"]}});assert.ok(failed.data.error);assert.ok(!JSON.stringify(failed.data).includes("mcp-manager-secret-value"));
  const mcpHttp=createServer(async(req,res)=>{if(req.method!=="POST"){res.writeHead(405).end();return;}if(req.headers.authorization!=="Bearer mcp-http-secret"){res.writeHead(401).end();return;}const chunks:Buffer[]=[];for await(const chunk of req)chunks.push(Buffer.from(chunk));const message=JSON.parse(Buffer.concat(chunks).toString());if(message.id===undefined){res.writeHead(202).end();return;}const result=message.method==="initialize"?{protocolVersion:message.params.protocolVersion,capabilities:{tools:{}},serverInfo:{name:"HTTP fixture",version:"1"},instructions:"Streamable HTTP fixture instructions"}:{tools:[{name:"http_echo",description:"HTTP echo",inputSchema:{type:"object",properties:{}}}]};res.writeHead(200,{"Content-Type":"application/json"});res.end(JSON.stringify({jsonrpc:"2.0",id:message.id,result}));});mcpHttp.listen(0,"127.0.0.1");await once(mcpHttp,"listening");t.after(()=>{mcpHttp.close();mcpHttp.closeAllConnections();});
  await request(mcpUrl,{operation:"save",scope:"global",name:"http-manager",definition:{url:`http://127.0.0.1:${(mcpHttp.address() as {port:number}).port}/mcp`,headers:{Authorization:"Bearer mcp-http-secret"},enabled:false}});const httpDefinition=(await request(mcpUrl+"?scope=global&name=http-manager")).data;assert.equal(httpDefinition.definition.headers.Authorization,"[已保存]");const httpDebug=await request(mcpUrl,{operation:"debug",scope:"global",name:"http-manager",original:"http-manager",definition:httpDefinition.definition});assert.equal(httpDebug.data.state,"connected");assert.equal(httpDebug.data.tools[0].name,"http_echo");assert.match(httpDebug.data.instructions,/HTTP fixture/);assert.ok(!JSON.stringify(httpDebug).includes("mcp-http-secret"));
  probePid=Number(await readFile(mcpProcess,"utf8"));const cancelDebug=new AbortController();const cancelled=fetch(origin+"/api"+mcpUrl,{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({operation:"debug",scope:"global",name:"manager",original:"manager",definition:{...definition.definition,args:[mcpScript,"hang"]}}),signal:cancelDebug.signal});await waitFor(async()=>Number(await readFile(mcpProcess,"utf8"))!==probePid);const hangingPid=Number(await readFile(mcpProcess,"utf8"));cancelDebug.abort();await assert.rejects(cancelled);await waitFor(async()=>gone(hangingPid));
  const beforeInvalid=await readFile(mcpFile,"utf8");assert.equal((await request(mcpUrl,{operation:"save",scope:"global",name:"broken",definition:{command:123}})).status,400);assert.equal(await readFile(mcpFile,"utf8"),beforeInvalid);assert.equal((await request(mcpUrl,{operation:"save",scope:"global",name:"sse",definition:{type:"sse",url:"http://127.0.0.1/mcp"}})).status,400);
  assert.equal((await request(mcpUrl,{operation:"save",scope:"project",name:"override",definition:{enabled:false}})).status,400);
  assert.equal((await request(mcpUrl,{operation:"save",scope:"project",name:"auth",definition:{url:"http://127.0.0.1/mcp",auth:{provider:"mock"}}})).status,400);
  await request(mcpUrl,{operation:"save",scope:"global",name:"another",definition:{command:"not-started"}});assert.equal((await request(mcpUrl,{operation:"save",scope:"global",name:"manager",original:"manager",revision:definition.revision,definition:definition.definition})).status,400);
  const toggles=await Promise.all(["manager","another"].map(name=>request(mcpUrl,{operation:"toggle",scope:"global",name,enabled:false})));assert.ok(toggles.every(r=>r.status===200));assert.ok((await request(mcpUrl+"?scope=global")).data.servers.every((s:any)=>!s.enabled));
  await writeFile(mcpFile,"\uFEFF"+await readFile(mcpFile,"utf8"));assert.ok((await request(mcpUrl)).data.servers.some((s:any)=>s.name==="manager"));
  definition=(await request(mcpUrl+"?scope=global&name=manager")).data;assert.equal((await request(mcpUrl,{operation:"save",scope:"global",name:"renamed",original:"manager",revision:definition.revision,definition:definition.definition})).status,200);assert.equal(JSON.parse(await readFile(mcpFile,"utf8")).mcpServers.renamed.env.TOKEN,"mcp-manager-secret-value");
  await request(mcpUrl,{operation:"save",scope:"global",name:"constructor",definition:{command:"not-started",enabled:false}});assert.ok((await request(mcpUrl+"?scope=global")).data.servers.some((s:any)=>s.name==="constructor"));
  await request(mcpUrl,{operation:"delete",scope:"global",name:"renamed"});assert.ok(!(await request(mcpUrl)).data.servers.some((s:any)=>s.name==="renamed"));
  }
  const localPackage=join(temp,"local-package"); await mkdir(localPackage); await writeFile(join(localPackage,"package.json"),JSON.stringify({name:"pi-desk-test-resource",version:"1.0.0",pi:{extensions:["extension.ts"],skills:["skills"]}})); await writeFile(join(localPackage,"extension.ts"),"export default function () {}\n");
  await mkdir(join(localPackage,"skills","pi-desk-package"),{recursive:true});await writeFile(join(localPackage,"skills","pi-desk-package","SKILL.md"),"---\nname: pi-desk-package\ndescription: Installed package skill\n---\nPackage body\n");
  assert.equal((await request(resourceUrl,{operation:"package_install",source:localPackage})).status,200);
  assert.ok((await request(skillsUrl)).data.some((s:any)=>s.name==="pi-desk-package"));
  assert.equal((await request(resourceUrl)).data.packages.length,1);
  assert.equal((await request(resourceUrl,{operation:"package_remove",source:localPackage})).status,200); await access(localPackage);
  assert.equal((await request(resourceUrl,{operation:"delete",path:"prompts/review.md"})).status,200);
  const abort = new AbortController(); const stream = await fetch(origin + "/api/events", { signal: abort.signal }); const reader = stream.body!.getReader(); assert.match(new TextDecoder().decode((await reader.read()).value), /"type":"state"/);
  assert.equal((await request(`/sessions/${session.id}/prompt`, { text: "background test" })).status, 202); abort.abort();
  let snapshot;
  for (let i = 0; i < 60; i++) { snapshot = (await request(`/sessions/${session.id}`)).data; if (snapshot.record.status === "idle" && snapshot.stats.assistantMessages > 0) break; await new Promise(resolve => setTimeout(resolve, 100)); }
  assert.equal(snapshot.stats.tokens.output, 3); assert.equal(snapshot.record.status, "idle"); assert.ok(!("cost" in snapshot.stats));
  assert.equal((await request("/state")).data.workspaces[0].id,w.id);
  await request("/sessions/older-history",{draft:"draft does not change activity order"},"PATCH");assert.equal((await request("/state")).data.workspaces[0].id,w.id);
  assert.equal((await request("/sessions/older-history/prompt",{text:"workspace live order fixture"})).status,202);
  let activeOrder;for(let attempt=0;attempt<100;attempt++){activeOrder=(await request("/state")).data;if(activeOrder.sessions.find((r:any)=>r.id==="older-history").status==="running")break;await new Promise(resolve=>setTimeout(resolve,10));}assert.equal(activeOrder.workspaces[0].id,"older");assert.equal(activeOrder.sessions.find((r:any)=>r.id==="older-history").status,"running","workspace moves before the reply completes");
  for(let attempt=0;attempt<60;attempt++){if((await request("/state")).data.sessions.find((r:any)=>r.id==="older-history").status!=="running")break;await new Promise(resolve=>setTimeout(resolve,50));}
  const traces=(await request(`/sessions/${session.id}/traces`)).data;
  assert.ok(traces.some((entry:any)=>entry.type==="assistant"&&entry.data.messageTimestamp===snapshot.messages.at(-1).timestamp));
  assert.ok(traces.some((entry:any)=>entry.type==="settled"));
  const sessionEntries=(await readFile(snapshot.record.sessionPath,"utf8")).trim().split("\n").map(line=>JSON.parse(line));
  assert.equal(sessionEntries.filter(entry=>entry.type==="custom"&&entry.customType==="pi-desk-event").length,traces.length);
  await assert.rejects(access(join(temp,"desk","traces")));
  await request(`/sessions/${session.id}/queue`,{operation:"replace",pending:[{id:"lost",text:"preserve queued text",behavior:"followUp",images:[{type:"image",id:"lost",name:"lost.png",mimeType:"image/png",cacheKey:"f".repeat(64)}]}]});
  assert.equal((await request(`/sessions/${session.id}/queue`,{operation:"send",id:"lost"})).status,400);
  assert.equal((await request(`/sessions/${session.id}/composer`)).data.pending[0].text,"preserve queued text");
  await request(`/sessions/${session.id}/queue`,{operation:"replace",pending:[{id:"resumed",text:"resumed queue test",behavior:"followUp",images:[]}]});
  assert.equal((await request(`/sessions/${session.id}/queue`,{operation:"send",id:"resumed"})).status,202);
  for(let i=0;i<60;i++){const state=(await request(`/sessions/${session.id}`)).data;if(state.record.status==="idle"&&state.stats.assistantMessages===2)break;await new Promise(resolve=>setTimeout(resolve,100));}
  assert.equal((await request(`/sessions/${session.id}/composer`)).data.pending.length,0);
  const beforeResend=(await request(`/sessions/${session.id}`)).data,lastUser=beforeResend.messages.findLast((m:any)=>m.role==="user"),oldReply=beforeResend.messages.at(-1);
  assert.equal((await request(`/sessions/${session.id}/message`,{operation:"resend",entryId:beforeResend.messages.find((m:any)=>m.role==="user").entryId,text:"old message"})).status,400);
  assert.equal((await request(`/sessions/${session.id}/message`,{operation:"resend",entryId:lastUser.entryId,text:"HTTP revised fixture"})).status,200);
  let resent;for(let i=0;i<100;i++){resent=(await request(`/sessions/${session.id}`)).data;if(resent.record.status==="idle"&&resent.messages.some((m:any)=>JSON.stringify(m.content).includes("HTTP revised fixture"))&&resent.messages.at(-1).role==="assistant")break;await new Promise(resolve=>setTimeout(resolve,50));}
  assert.equal(resent.record.status,"idle");assert.equal(resent.messages.filter((m:any)=>m.role==="user").length,2);assert.ok(!resent.messages.some((m:any)=>m.entryId===lastUser.entryId||m.entryId===oldReply.entryId));
  assert.equal((await request(`/sessions/${session.id}/message`,{operation:"resend",entryId:lastUser.entryId,text:"duplicate click"})).status,400);
  assert.equal((await request(`/sessions/${session.id}`, {thinking:"off"}, "PATCH")).status, 200);
  assert.equal((await request(`/sessions/${session.id}`, {thinking:"high"}, "PATCH")).status, 400);
  for(const [action,method] of [["tree","GET"],["navigate","POST"],["compact","POST"],["export","GET"]]) assert.equal((await request(`/sessions/${session.id}/${action}`,method==="POST"?{}:undefined,method)).status,404);
  const userEntry=(await request(`/sessions/${session.id}`)).data.messages.find((m:any)=>m.role==="user");
  const historicalFork=(await request(`/sessions/${session.id}/fork`,{entryId:userEntry.entryId})).data;
  const historicalSnapshot=(await request(`/sessions/${historicalFork.id}`)).data;
  assert.equal(historicalSnapshot.messages.at(-1).entryId,userEntry.entryId);assert.equal(historicalSnapshot.messages.filter((m:any)=>m.role==="user").length,1);
  const fork = (await request(`/sessions/${session.id}/fork`, {})).data;
  const forkSnapshot = (await request(`/sessions/${fork.id}`)).data;
  assert.equal((await request(`/sessions/${fork.id}`, undefined, "DELETE")).status, 200);
  await assert.rejects(access(forkSnapshot.record.sessionPath)); await access(snapshot.record.sessionPath);
  const reopened = await fetch(origin + "/api/events", { signal: AbortSignal.timeout(2000) }); const replay = reopened.body!.getReader(); assert.match(new TextDecoder().decode((await replay.read()).value), /background test/); await replay.cancel();
  const ws = new WebSocket(origin.replace("http", "ws") + `/api/terminal/${w.id}`, { origin }); await once(ws, "open");
  const terminalOutput = await new Promise<string>((resolve, reject) => { let value = ""; const timer = setTimeout(() => reject(new Error("PTY timed out: " + value)), 10000); ws.on("message", chunk => { const data = JSON.parse(String(chunk)); if (data.type === "error") { clearTimeout(timer); reject(new Error(data.message)); } if (data.type === "data") { value += data.data; if (value.includes("PI_DESK_PTY_OK")) { clearTimeout(timer); resolve(value); } } }); ws.send(JSON.stringify({ type: "resize", cols: 100, rows: 25 })); ws.send(JSON.stringify({ type: "input", data: process.platform === "win32" ? "Write-Output ('PI_DESK_' + 'PTY_OK')\r" : "printf 'PI_DESK_%s\\n' 'PTY_OK'\r" })); });
  assert.match(terminalOutput, /PI_DESK_PTY_OK/); ws.send(JSON.stringify({ type: "close" })); ws.close();
  // Real CONNECT tunnels: global fallback, per-model overrides, direct mode and Google global fetch.
  for(let index=0;index<2;index++){
    const proxy=createServer();const sockets=new Set<import("node:net").Socket>();
    proxy.on("connect",(req,socket,head)=>{
      proxyAuth.push(String(req.headers["proxy-authorization"]||""));
      const target=connect(modelPort,"127.0.0.1",()=>{proxyPorts.set(target.localPort!,index);socket.write("HTTP/1.1 200 Connection Established\r\n\r\n");if(head.length)target.write(head);socket.pipe(target);target.pipe(socket);});
      for(const item of [socket,target]){sockets.add(item);item.on("close",()=>sockets.delete(item));item.on("error",()=>{socket.destroy();target.destroy();});}
    });
    proxy.listen(0,"127.0.0.1");await once(proxy,"listening");
    t.after(()=>{for(const socket of sockets)socket.destroy();proxy.close();});
    proxyHits[index].push(`http://${index===0?"user:password@":""}127.0.0.1:${(proxy.address() as {port:number}).port}`);
  }
  const globalProxy=proxyHits[0].shift()!,modelProxy=proxyHits[1].shift()!;
  for(const proxy of ["socks5://127.0.0.1:1080","http://localhost/path","http://localhost/#fragment",42,"http://localhost\n"]){assert.equal((await request("/proxy",{proxy})).status,400);}
  assert.equal((await request("/proxy",{proxy:globalProxy})).status,200);
  const routes=JSON.parse(await readFile(join(agent,"models.json"),"utf8"));
  routes.providers.mock.models.push({id:"proxy-other",contextWindow:100000,maxTokens:1000});
  routes.providers["google-fixture"]={api:"google-generative-ai",apiKey:"fixture",baseUrl:`http://127.0.0.1:${modelPort}`,models:[{id:"gemini-fixture",contextWindow:100000,maxTokens:1000}]};
  await writeFile(join(agent,"models.json"),JSON.stringify(routes));
  const other=(await request("/sessions",{workspaceId:w.id,mode:"minimal",model:"mock/proxy-other"})).data;
  const google=(await request("/sessions",{workspaceId:w.id,mode:"minimal",model:"google-fixture/gemini-fixture"})).data;
  const runProxy=async(id:string)=>{
    const before=(await request(`/sessions/${id}`)).data.stats.assistantMessages;
    assert.equal((await request(`/sessions/${id}/prompt`,{text:"proxy route fixture"})).status,202);
    for(let attempt=0;attempt<100;attempt++){const result=(await request(`/sessions/${id}`)).data;assert.notEqual(result.record.status,"error",JSON.stringify(result));if(result.record.status==="idle"&&result.stats.assistantMessages>before){assert.equal(result.messages.at(-1).role,"assistant");assert.ok(!result.messages.at(-1).errorMessage,JSON.stringify(result.messages.at(-1)));return;}await new Promise(resolve=>setTimeout(resolve,50));}assert.fail("proxy request timed out");
  };
  assert.equal((await request("/proxy",{model:"mock/proxy-other",proxy:modelProxy})).status,200);
  await Promise.all([runProxy(session.id),runProxy(other.id),runProxy(google.id)]);
  assert.equal(proxyHits[0].length,2);assert.equal(proxyHits[1].length,1);assert.ok(proxyAuth.includes("Basic "+Buffer.from("user:password").toString("base64")));
  assert.equal((await request("/proxy",{model:"mock/mock",proxy:null})).status,200);
  await runProxy(session.id);assert.equal(proxyHits[0].length,2);
  assert.equal((await request("/proxy",{model:"mock/mock",proxy:""})).status,200);
  await runProxy(session.id);assert.equal(proxyHits[0].length,3);
  const savedProxy=JSON.parse(await readFile(join(temp,"desk/state.json"),"utf8")).preferences;
  assert.equal(savedProxy.proxy.global,globalProxy);assert.equal(savedProxy.proxy.models["mock/proxy-other"],modelProxy);assert.ok(!Object.hasOwn(savedProxy.proxy.models,"mock/mock"));assert.equal(savedProxy.theme,"light");
  assert.equal((await request("/proxy",{proxy:""})).status,200);await runProxy(session.id);assert.equal(proxyHits[0].length,3);
  for(const provider of ["", "constructor", "__proto__", "bad\nprovider"])assert.equal((await request("/proxy",{provider,proxy:modelProxy})).status,400);
  assert.equal((await request("/proxy",{proxy:globalProxy})).status,200);
  assert.equal((await request("/proxy",{provider:"mock",proxy:modelProxy})).status,200);
  let providerHits=proxyHits[1].length,globalHits=proxyHits[0].length;
  await runProxy(session.id);assert.equal(proxyHits[1].length,++providerHits);assert.equal(proxyHits[0].length,globalHits,"provider proxy overrides global proxy");
  assert.equal((await request("/models/test",{provider:"mock",model:"mock"})).status,200);assert.equal(proxyHits[1].length,++providerHits,"connection tests use the provider proxy");
  assert.equal((await request("/models/discover",{provider:{...routes.providers.mock,id:"mock"}})).status,200);assert.equal(proxyHits[1].length,++providerHits,"model discovery uses the provider proxy");
  assert.equal((await request("/proxy",{model:"mock/mock",proxy:null})).status,200);await runProxy(session.id);assert.equal(proxyHits[1].length,providerHits,"a model's direct setting overrides the provider proxy");
  assert.equal((await request("/proxy",{model:"mock/mock",proxy:globalProxy})).status,200);await runProxy(session.id);assert.equal(proxyHits[0].length,++globalHits,"model proxy overrides provider proxy");
  assert.equal((await request("/proxy",{model:"mock/mock",proxy:""})).status,200);
  assert.equal((await request("/proxy",{provider:"mock",proxy:null})).status,200);await runProxy(session.id);assert.equal(proxyHits[0].length,globalHits);assert.equal(proxyHits[1].length,providerHits,"provider direct ignores global proxy");
  assert.equal(JSON.parse(await readFile(join(temp,"desk/state.json"),"utf8")).preferences.proxy.providers.mock,null);
  assert.equal((await request("/proxy",{provider:"mock",proxy:""})).status,200);await runProxy(session.id);assert.equal(proxyHits[0].length,++globalHits,"clearing provider proxy restores global inheritance");
  assert.equal((await request("/proxy",{proxy:""})).status,200);
  assert.equal((await request(`/workspaces/${w.id}`,{name:"  Renamed workspace  "},"PATCH")).data.name,"Renamed workspace");
  assert.equal((await request(`/workspaces/${w.id}`,{name:"   "},"PATCH")).status,400);
  const duplicate=await request("/workspaces",{path:temp,name:"Other workspace",trusted:false});assert.equal(duplicate.status,200);
  assert.equal((await request(`/workspaces/${w.id}`,{name:"Other workspace"},"PATCH")).status,400);
  assert.equal((await request(`/workspaces/${w.id}/disconnect`,{})).status,400);assert.equal((await request(`/workspaces/${w.id}/ssh-info`)).status,400);
  const scheduled=await request("/schedules",{name:"Preserve paused schedule",prompt:"fixture",workspaceId:w.id,model:"mock/mock",mode:"minimal",frequency:"once",runAt:new Date(Date.now()+86400000).toISOString(),enabled:true});assert.equal(scheduled.status,200,JSON.stringify(scheduled.data));
  const sessionsBefore=(await request("/state")).data.sessions;
  assert.equal((await request(`/workspaces/missing`,undefined,"DELETE")).status,400);
  assert.equal((await request(`/workspaces/${w.id}/remove`,{})).status,200);
  const removedState=(await request("/state")).data;assert.equal(removedState.workspaces.find((item:any)=>item.id===w.id).hidden,true);assert.deepEqual(removedState.sessions.map((r:any)=>r.id),sessionsBefore.filter((r:any)=>r.workspaceId!==w.id).map((r:any)=>r.id));
  assert.equal(removedState.schedules.find((task:any)=>task.id===scheduled.data.id).enabled,false);
  assert.equal((await request("/schedules",{...scheduled.data,enabled:true})).status,400);
  assert.equal((await request(`/sessions/${session.id}/reload`,{})).status,400);for(const record of sessionsBefore.filter((r:any)=>r.workspaceId===w.id&&r.sessionPath))await assert.rejects(access(record.sessionPath));await access(join(cwd,"hello.txt"));
  assert.equal(JSON.parse(await readFile(join(temp,"desk","state.json"),"utf8")).workspaces.find((item:any)=>item.id===w.id).hidden,true);
  const restored=await request("/workspaces",{path:cwd,trusted:true});assert.equal(restored.data.id,w.id);assert.equal(restored.data.hidden,false);assert.equal(restored.data.name,"Renamed workspace");
  assert.equal((await request("/state")).data.sessions.some((r:any)=>r.workspaceId===w.id),false);await request("/discover",{});assert.equal((await request("/state")).data.sessions.some((r:any)=>r.workspaceId===w.id),false);
  assert.equal((await request("/state")).data.schedules.find((task:any)=>task.id===scheduled.data.id).enabled,false);
});

test("development HTTP: native Vite config, transformed React entry and isolated state", {timeout:45000}, async t=>{
  const temp=await realpath(await mkdtemp(join(tmpdir(),"pi-desk-dev-"))),agent=join(temp,"agent");await mkdir(agent);
  const child=spawn(process.execPath,[resolve("src/server.ts"),"--dev","--port","0","--no-open"],{cwd:process.cwd(),windowsHide:true,env:{...process.env,PI_CODING_AGENT_DIR:agent,PI_CODING_AGENT_SESSION_DIR:join(temp,"sessions"),PI_DESK_DIR:join(temp,"desk")},stdio:["ignore","pipe","pipe"]});
  t.after(()=>stopChild(child));
  let output="",errors="";child.stderr.on("data",chunk=>{errors+=String(chunk);});
  const origin=await new Promise<string>((resolve,reject)=>{
    const timer=setTimeout(()=>reject(Error("development service did not start: "+errors)),30000);
    child.stdout.on("data",chunk=>{output+=String(chunk);const match=/http:\/\/127\.0\.0\.1:\d+/.exec(output);if(match){clearTimeout(timer);resolve(match[0]);}});
    child.once("error",error=>{clearTimeout(timer);reject(error);});child.once("exit",code=>{clearTimeout(timer);reject(Error(`development service exited ${code}: ${errors}`));});
  });
  const page=await fetch(origin);assert.equal(page.status,200);assert.match(await page.text(),/@vite\/client/);
  const entry=await fetch(origin+"/src/web/main.tsx");assert.equal(entry.status,200);assert.match(await entry.text(),/createRoot/);
  const health=await(await fetch(origin+"/api/health")).json();assert.equal(health.runtime,"Node.js");assert.equal(health.agentDir,agent);
  const state=await(await fetch(origin+"/api/state")).json();assert.deepEqual(state.sessions,[]);assert.deepEqual(state.workspaces,[]);
  assert.equal((await fetch(origin+"/api/state",{headers:{Origin:"https://untrusted.example"}})).status,403);
});

test("Windows open-with: installed apps, visible launch, PowerShell cwd and startup errors", {skip:process.platform!=="win32"}, async()=>{
  const {api}=await import("./api.ts"),{default:cp}=await import("node:child_process"),{syncBuiltinESMExports}=await import("node:module"),{promisify}=await import("node:util"),{EventEmitter}=await import("node:events"),{Readable}=await import("node:stream");
  const root=await realpath(await mkdtemp(join(tmpdir(),"pi-desk-open-"))),cwd=join(root,"项目 & 'quoted',=100%"),programs=join(root,"Programs"),system=join(root,"Windows");await mkdir(cwd);
  const file=join(cwd,"file & 'quoted',=100%.txt"),outside=join(root,"outside.txt");await writeFile(file,"file open fixture");await writeFile(outside,"outside preview fixture");const {fileURLToPath}=await import("node:url");
  const paths=[join(system,"explorer.exe"),join(programs,"Cursor/Cursor.exe"),join(programs,"Git/git-bash.exe"),join(programs,"PowerShell/7/pwsh.exe")];
  for(const path of paths){await mkdir(resolve(path,".."),{recursive:true});await writeFile(path,"");}
  const keys=["PATH","LOCALAPPDATA","ProgramFiles","ProgramFiles(x86)","SystemRoot","ELECTRON_RUN_AS_NODE","NODE_OPTIONS","VSCODE_IPC_HOOK_CLI"],before=keys.map(key=>[key,process.env[key]] as const),originalSpawn=cp.spawn,originalExec=cp.execFile;let outcome="ok";const launches:any[]=[];
  try{
    Object.assign(process.env,{PATH:"",LOCALAPPDATA:join(root,"Local"),ProgramFiles:programs,"ProgramFiles(x86)":join(root,"x86"),SystemRoot:system,ELECTRON_RUN_AS_NODE:"1",NODE_OPTIONS:"--inspect",VSCODE_IPC_HOOK_CLI:"stale"});
    cp.execFile=Object.assign(()=>{throw Error("not registered");},{[promisify.custom]:async(command:any,args:any,options:any)=>{if(command==="reg.exe")throw Error("not registered");launches.push({command,args,options});if(outcome!=="ok")throw Object.assign(Error("shell launch failed"),{code:outcome==="explorer"?1:23});return {stdout:"",stderr:""};}}) as any;
    cp.spawn=((command:any,args:any,options:any)=>{launches.push({command,args,options});const child=new EventEmitter() as any;child.unref=()=>{};setImmediate(()=>outcome==="error"?child.emit("error",Error("ENOENT")):child.emit("exit",outcome==="fail"?23:outcome==="explorer"?1:0));return child;}) as any;syncBuiltinESMExports();
    const runtime={store:{state:{workspaces:[{id:"fixture",path:cwd,trusted:true}]}},remote:{workspaceFor:()=>undefined}} as any;
    const request=async(path:string,data?:unknown)=>{let value:any;const req=Readable.from(data?[Buffer.from(JSON.stringify(data))]:[]) as any;req.method=data?"POST":"GET";req.headers={"content-type":"application/json"};await api(runtime,req,{writeHead(){},end(raw:string){value=JSON.parse(raw);}} as any,new URL("http://localhost/api"+path));return value;};
    const apps=await request("/applications");assert.deepEqual(apps.map((app:any)=>app.id),["file-manager","cursor","gitbash","powershell"]);assert.ok(apps.every((app:any)=>!("command" in app)));
    for(const id of ["cursor","gitbash","powershell"]){assert.equal((await request("/workspaces/fixture/open",{applicationId:id})).opened,true);const last=launches.at(-1);assert.equal(last.options.cwd,cwd);assert.equal(last.options.windowsHide,false);assert.equal(last.options.env.ELECTRON_RUN_AS_NODE,undefined);assert.equal(last.options.env.NODE_OPTIONS,undefined);assert.equal(last.options.env.VSCODE_IPC_HOOK_CLI,undefined);assert.deepEqual(last.args,id==="powershell"?["-NoLogo","-NoExit","-WindowStyle","Normal"]:[id==="gitbash"?"--cd="+cwd:cwd]);}
    assert.equal((await request("/workspaces/fixture/open",{applicationId:"cursor",path:"file & 'quoted',=100%.txt"})).opened,true);assert.deepEqual(launches.at(-1).args,[file]);assert.equal(launches.at(-1).options.cwd,cwd);
    assert.equal((await request("/workspaces/fixture/open",{applicationId:"cursor",path:outside})).opened,true);assert.deepEqual(launches.at(-1).args,[outside]);assert.equal(launches.at(-1).options.cwd,root);
    assert.equal((await request("/workspaces/fixture/open",{applicationId:"file-manager",path:file})).opened,true);assert.equal(launches.at(-1).args[0],"/select,");assert.equal(fileURLToPath(launches.at(-1).args[1]),file);
    const fileLaunches=launches.length;await assert.rejects(request("/workspaces/fixture/open",{applicationId:"cursor",path:"."}),/普通文件/);await assert.rejects(request("/workspaces/fixture/open",{applicationId:"powershell",path:file}),/不支持打开文件/);await assert.rejects(request("/workspaces/fixture/open",{applicationId:"cursor",path:"missing.txt"}),/ENOENT/);await assert.rejects(request("/workspaces/fixture/open",{applicationId:"cursor",path:null}),/文本字段/);assert.equal(launches.length,fileLaunches);
    await assert.rejects(request("/workspaces/fixture/open",{applicationId:"vscode"}),/应用未安装/);outcome="fail";await assert.rejects(request("/workspaces/fixture/open",{applicationId:"cursor"}),/退出码 23/);outcome="error";await assert.rejects(request("/workspaces/fixture/open",{applicationId:"powershell"}),/无法启动 PowerShell/);await assert.rejects(request("/workspaces/fixture/open",{applicationId:"file-manager"}),/无法打开文件资源管理器/);
    outcome="explorer";assert.equal((await request("/workspaces/fixture/open",{applicationId:"file-manager"})).opened,true);const shell=launches.at(-1);assert.equal(shell.command,paths[0]);assert.equal(shell.options.cwd,cwd);assert.equal(shell.options.env.NODE_OPTIONS,undefined);assert.equal(shell.options.windowsHide,false);assert.equal(shell.options.timeout,10000);assert.deepEqual(shell.args,["file:///"+cwd.replaceAll("\\","/").replaceAll(" ","%20").replaceAll(",","%2C").replaceAll("=","%3D").replaceAll("100%","100%25")]);
    outcome="ok";assert.equal((await request("/open-url",{url:"https://example.com/a?query=中文&x=1"})).opened,true);const browser=launches.at(-1);assert.equal(browser.command,"rundll32.exe");assert.deepEqual(browser.args,["url.dll,FileProtocolHandler","https://example.com/a?query=%E4%B8%AD%E6%96%87&x=1"]);assert.equal(browser.options.windowsHide,false);assert.equal(browser.options.env.ELECTRON_RUN_AS_NODE,undefined);
    const count=launches.length;await assert.rejects(request("/open-url",{url:"file:///C:/secret"}),/HTTP/);assert.equal(launches.length,count);outcome="error";await assert.rejects(request("/open-url",{url:"https://example.com"}),/默认浏览器/);
  }finally{cp.spawn=originalSpawn;cp.execFile=originalExec;syncBuiltinESMExports();for(const [key,value] of before)if(value===undefined)delete process.env[key];else process.env[key]=value;}
});

test("message file links: native paths, Markdown bases, safe protocols and code blocks",async()=>{
  const {resolveLink,renderMarkdown,markdownText}=await import("../frontend/src/web/lib/links.ts"),base="D:/project 中文";
  assert.equal(markdownText("---\n## 检查完成\n\n已读取 **文件**，查看 [结果](README.md)。\n\n| 检查项 | 状态 |\n| --- | --- |\n| 文件 | 完成 |"),"检查完成 已读取 文件，查看 结果。 检查项 状态 文件 完成");
  assert.equal(markdownText("```bash\ngit status --short\n```\n\n第一行\n第二行 ![图片](image.png)"),"git status --short 第一行 第二行 图片");
  assert.deepEqual(resolveLink("file:///C:/A%20B/report.md#L12",base),{kind:"file",path:"C:/A B/report.md"});
  assert.deepEqual(resolveLink("..\\outside.txt:12:3",base),{kind:"file",path:"D:/outside.txt"});
  assert.deepEqual(resolveLink("image.png","/tmp/project/docs"),{kind:"file",path:"/tmp/project/docs/image.png"});
  assert.match(renderMarkdown("文件 D:\\project\\.hidden\\说明.md",base),/data-file-path="D:\/project\/.hidden\/说明.md"/);
  assert.doesNotMatch(renderMarkdown("https://example.com/docs/file.md",base),/data-file-path/);
  for(const bad of ["javascript:alert(1)","data:text/html,hello","https://user:secret@example.com"])assert.equal(resolveLink(bad,base),undefined);
  const html=renderMarkdown("[报告](C:/A B/中文.txt) 和 [网页](https://example.com) 与 `README.md`\n\n绝对路径 D:\\资料\\说明.txt",base);
  assert.match(html,/data-file-path="C:\/A B\/中文.txt"/);assert.match(html,/data-file-path="D:\/project 中文\/README.md"/);assert.match(html,/data-file-path="D:\/资料\/说明.txt"/);assert.match(html,/href="https:\/\/example.com\/" title="https:\/\/example.com\/"/);
  assert.doesNotMatch(renderMarkdown("```txt\nC:/A/file.txt\n```",base),/data-file-path/);
  assert.doesNotMatch(renderMarkdown("[文件](javascript:alert(1)) <script>alert(1)</script>",base),/<script>|href="javascript:/);
  assert.doesNotMatch(renderMarkdown("[`README.md`](https://example.com)",base),/data-file-path/);
  assert.doesNotMatch(renderMarkdown("普通文字 report.txt 与 `Math.random`、`foo.bar`",base),/data-file-path/);assert.match(renderMarkdown("`Dockerfile` 与 `src/index.ts`",base),/role="button" data-file-path=/);
});

test("terminal startup: early input survives asynchronous SSH attachment",{timeout:10000},async t=>{
  const {attachTerminals}=await import("./terminals.ts"),server=createServer();
  let attach!:()=>void;const ready=new Promise<void>(resolve=>attach=resolve);
  const runtime={store:{state:{workspaces:[{id:"fixture",remote:{target:"fixture"}}]}},remote:{async terminal(_workspace:unknown,socket:WebSocket){await ready;socket.on("message",raw=>socket.send(raw.toString()));}}} as unknown as import("./runtime.ts").Runtime;
  const close=attachTerminals(server,runtime,()=>true);t.after(()=>{close();server.close();server.closeAllConnections();});
  server.listen(0,"127.0.0.1");await once(server,"listening");
  const socket=new WebSocket(`ws://127.0.0.1:${(server.address() as {port:number}).port}/api/terminal/fixture`);await once(socket,"open");
  const echoed=once(socket,"message");socket.send(JSON.stringify({type:"input",data:"first command\r"}));
  await new Promise(resolve=>setTimeout(resolve,100));attach();
  assert.equal(JSON.parse(String((await echoed)[0])).data,"first command\r");socket.close();
});

test("composer IME guard: composing, late confirmation, key release, blur and cleanup",async()=>{
  const {observeComposition}=await import("../frontend/src/web/dsh/keyboard-composition.ts");
  // Node's EventTarget needs an options object to remove capture listeners; match browser semantics.
  const target=new EventTarget(),remove=target.removeEventListener.bind(target);
  target.removeEventListener=(type,listener,options)=>remove(type,listener,typeof options==="boolean"?{capture:options}:options);
  const document=Object.assign(target,{defaultView:new EventTarget()}) as unknown as Document;
  const observer=observeComposition(document),key=(extra:Partial<KeyboardEvent>={})=>({isComposing:false,keyCode:13,...extra} as KeyboardEvent);
  assert.equal(observer.guards(key()),false);
  document.dispatchEvent(new Event("compositionstart"));assert.equal(observer.guards(key()),true);assert.equal(observer.guards(key()),true);
  document.dispatchEvent(new Event("compositionend"));assert.equal(observer.guards(key()),true);assert.equal(observer.guards(key()),false);
  document.dispatchEvent(new Event("compositionend"));document.dispatchEvent(new Event("keyup"));assert.equal(observer.guards(key()),false);
  assert.equal(observer.guards(key({isComposing:true})),true);assert.equal(observer.guards(key({keyCode:229})),true);
  document.dispatchEvent(new Event("compositionstart"));document.defaultView!.dispatchEvent(new Event("blur"));assert.equal(observer.guards(key()),false);
  observer.dispose();document.dispatchEvent(new Event("compositionstart"));assert.equal(observer.guards(key()),false);
});
