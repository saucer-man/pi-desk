import {test} from "node:test";
import assert from "node:assert/strict";
import {existsSync} from "node:fs";
import {mkdtemp,mkdir,writeFile,readFile,unlink} from "node:fs/promises";
import {tmpdir} from "node:os";
import {join,resolve} from "node:path";
import {spawn} from "node:child_process";
import {once} from "node:events";
import {createServer} from "node:http";
import {randomUUID} from "node:crypto";
import {chromium} from "playwright-core";

test("composer: project file search, references, file paste, reasoning sync and interactive questions",{timeout:60000},async t=>{
  const executablePath=[process.env.PI_DESK_BROWSER_EXECUTABLE,"C:/Program Files/Google/Chrome/Application/chrome.exe","C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe","/Applications/Google Chrome.app/Contents/MacOS/Google Chrome","/usr/bin/chromium","/usr/bin/google-chrome"].find(path=>path&&existsSync(path));
  if(!executablePath){t.skip("Chrome / Edge / Chromium unavailable");return;}
  const temp=await mkdtemp(join(tmpdir(),"pi-desk-composer-")),project=join(temp,"project"),agent=join(temp,"agent");
  await mkdir(join(project,"src"),{recursive:true});await mkdir(join(project,"docs"));await mkdir(agent);
  for(const path of ["src/app.tsx","docs/app.tsx","docs/设计 (草稿) [v1].md"])await writeFile(join(project,path),"fixture content");
  const answers:string[]=[],requests:any[]=[];
  let releaseSwitch!:()=>void,startedSwitch!:()=>void;const switchGate=new Promise<void>(resolve=>releaseSwitch=resolve),switchStarted=new Promise<void>(resolve=>startedSwitch=resolve);t.after(()=>releaseSwitch());
  let releaseFork!:()=>void;const forkGate=new Promise<void>(resolve=>releaseFork=resolve);t.after(()=>releaseFork());
  const model=createServer(async(req,res)=>{
    if(req.url==="/mcp"&&req.method!=="POST"){res.writeHead(405).end();return;}
    let body="";for await(const chunk of req)body+=chunk;const request=JSON.parse(body);
    if(req.url==="/mcp"){
      if(req.headers.authorization!=="Bearer timing-fixture-secret"){res.writeHead(401).end();return;}
      if(request.id===undefined){res.writeHead(202).end();return;}
      if(request.method==="initialize")await new Promise(resolve=>setTimeout(resolve,150));
      const result=request.method==="initialize"?{protocolVersion:request.params.protocolVersion,capabilities:{tools:{}},serverInfo:{name:"timing",version:"1"}}:{tools:[]};
      res.writeHead(200,{"Content-Type":"application/json"}).end(JSON.stringify({jsonrpc:"2.0",id:request.id,result}));return;
    }
    const last=request.messages.at(-1);requests.push(request);
    if(last.role!=="tool"&&JSON.stringify(last).includes("live-switch")){startedSwitch();await switchGate;}
    res.writeHead(200,{"Content-Type":"text/event-stream"});
    if(last.role!=="tool"&&JSON.stringify(last).includes("live-fork")){
      res.write(`data: ${JSON.stringify({id:"fixture",object:"chat.completion.chunk",created:1,model:"mock",choices:[{index:0,delta:{role:"assistant",content:"fork-streaming-fixture"},finish_reason:null}]})}\n\n`);
      await forkGate;
    }
    const switching=last.role!=="tool"&&JSON.stringify(last).includes("live-switch");
    const tool=switching||last.role!=="tool"&&JSON.stringify(last).includes("question-");
    if(last.role==="tool")answers.push(last.content);
    const delegated=last.role!=="tool"&&JSON.stringify(last).includes("question-delegate");
    const args=switching?{path:"src/app.tsx"}:delegated?{tasks:[{task:JSON.stringify(last).includes("question-delegate-child")?"question-choice":"question-delegate-child",mode:"standard",access:"read"}]}:{question:"请选择实现方式",...(JSON.stringify(last).includes("question-choice")?{options:["最小修改","完整重写"]}:{})};
    for(const chunk of [{choices:[{index:0,delta:tool?{role:"assistant",tool_calls:[{index:0,id:"ask",type:"function",function:{name:switching?"read":delegated?"subagent":"request_user_input",arguments:JSON.stringify(args)}}]}:{role:"assistant",content:"回答已收到"},finish_reason:null}]},{choices:[{index:0,delta:{},finish_reason:tool?"tool_calls":"stop"}]},{choices:[],usage:{prompt_tokens:10,completion_tokens:3,total_tokens:13}}])res.write(`data: ${JSON.stringify({id:"fixture",object:"chat.completion.chunk",created:1,model:"mock",...chunk})}\n\n`);
    res.end("data: [DONE]\n\n");
  });model.listen(0,"127.0.0.1");await once(model,"listening");t.after(()=>{model.close();model.closeAllConnections();});
  await writeFile(join(agent,"models.json"),JSON.stringify({providers:{mock:{api:"openai-completions",baseUrl:`http://127.0.0.1:${(model.address() as {port:number}).port}/v1`,apiKey:"fixture",models:[{id:"mock",reasoning:true,contextWindow:100000,maxTokens:1000},{id:"plain",reasoning:false},{id:"limited",reasoning:true,thinkingLevelMap:{high:null,xhigh:"high"}}]}}}));
  await writeFile(join(agent,"settings.json"),JSON.stringify({defaultProvider:"mock",defaultModel:"mock"}));
  await mkdir(join(project,".pi"));await writeFile(join(project,".pi","settings.json"),JSON.stringify({defaultThinkingLevel:"minimal"}));
  const child=spawn(process.execPath,[resolve("dist/server.js"),"--port","0","--no-open"],{windowsHide:true,env:{...process.env,PI_DESK_SESSION_TIMING:"1",PI_DESK_DIR:join(temp,"desk"),PI_CODING_AGENT_DIR:agent,PI_CODING_AGENT_SESSION_DIR:join(agent,"sessions")},stdio:["ignore","pipe","pipe"]});
  let stderr="";child.stderr.setEncoding("utf8").on("data",chunk=>stderr+=chunk);
  const timings=()=>stderr.split(/\r?\n/).filter(line=>line.startsWith("[session-timing] ")&&line.endsWith("}")).map(line=>JSON.parse(line.slice(17)));
  t.after(async()=>{if(child.exitCode===null){const exited=once(child,"exit");child.kill();await exited;}});
  const url=await new Promise<string>((resolve,reject)=>{const timer=setTimeout(()=>reject(Error("production server startup timeout")),15000);child.once("error",reject);child.stdout.on("data",chunk=>{const match=String(chunk).match(/http:\/\/127\.0\.0\.1:\d+/);if(match){clearTimeout(timer);resolve(match[0]);}});child.once("exit",code=>{clearTimeout(timer);reject(Error(`production server exited: ${code}`));});});
  const request=async(path:string,data?:unknown,method=data?"POST":"GET")=>{const response=await fetch(url+"/api"+path,{method,headers:data?{"Content-Type":"application/json"}:undefined,body:data?JSON.stringify(data):undefined});const body=await response.json();assert.ok(response.ok,JSON.stringify(body));return body;};
  const workspace=await request("/workspaces",{path:project});
  const browser=await chromium.launch({executablePath,headless:true});t.after(()=>browser.close());
  const page=await browser.newPage({viewport:{width:1280,height:900}}),errors:string[]=[];page.setDefaultTimeout(8000);page.on("pageerror",error=>errors.push(error.message));
  let releaseCatalog!:()=>void;const catalogGate=new Promise<void>(resolve=>releaseCatalog=resolve);t.after(()=>releaseCatalog());
  await page.route("**/api/models{,?*}",async route=>{await catalogGate;await route.continue().catch(()=>{});});
  await page.goto(url);
  await page.getByRole("button",{name:"选择模型",exact:true}).click();
  const loadingThinking=page.getByRole("menuitem",{name:"推理等级",exact:true});await loadingThinking.waitFor();assert.match(await loadingThinking.innerText(),/加载中/);assert.equal(await loadingThinking.isDisabled(),true);assert.equal(await loadingThinking.getAttribute("aria-busy"),"true");
  await page.keyboard.press("Escape");releaseCatalog();await page.unroute("**/api/models{,?*}");
  const editor=page.getByRole("textbox",{name:"任务输入",exact:true}),options=page.getByRole("option"),picker=page.getByRole("listbox",{name:"项目文件搜索结果"});
  await editor.waitFor();
  await mkdir(join(project,".pi","extensions"));const shutdownHook=join(project,".pi","extensions","shutdown.ts");await writeFile(shutdownHook,'export default function(pi){pi.on("session_shutdown",()=>new Promise(()=>{}));}');
  const emptyLoaded=page.waitForResponse(r=>r.request().method()==="GET"&&/\/api\/sessions\/[^/?]+$/.test(r.url())&&r.status()===200);
  await page.getByRole("button",{name:"新建任务",exact:true}).click();await emptyLoaded;await page.getByRole("button",{name:"更多操作：新会话",exact:true}).focus();await page.keyboard.press("Enter");await page.getByRole("menuitem",{name:"删除会话",exact:true}).click();
  const deletion=page.getByRole("dialog",{name:"删除会话",exact:true});await deletion.getByRole("button",{name:"确认删除",exact:true}).click();await deletion.waitFor({state:"detached",timeout:8000});assert.equal((await request("/state")).sessions.length,0);await unlink(shutdownHook);
  // A draft can configure SDK-supported reasoning without creating an empty session.
  const openThinking=async()=>{await page.getByRole("button",{name:"选择模型",exact:true}).click();await page.getByRole("menuitem",{name:/^推理等级/}).click();};
  const chooseModel=async(name:string)=>{await page.getByRole("button",{name:"选择模型",exact:true}).click();await page.getByRole("menuitem",{name:/^模型(?!设置)/}).click();await page.getByRole("menuitemradio",{name:new RegExp(`^${name}`)}).click();};
  await openThinking();assert.equal(await page.getByRole("menuitemradio",{name:"Minimal",exact:true}).getAttribute("aria-checked"),"true");
  await page.getByRole("menuitemradio",{name:"High",exact:true}).click();
  await chooseModel("plain");await openThinking();assert.equal(await page.getByRole("menuitemradio").count(),1);assert.equal(await page.getByRole("menuitemradio",{name:"Off",exact:true}).getAttribute("aria-checked"),"true");await page.keyboard.press("Escape");await page.keyboard.press("Escape");
  await chooseModel("limited");await openThinking();assert.equal(await page.getByRole("menuitemradio",{name:"High",exact:true}).count(),0);await page.getByRole("menuitemradio",{name:"XHigh",exact:true}).waitFor();await page.keyboard.press("Escape");await page.keyboard.press("Escape");
  await chooseModel("mock");await openThinking();assert.equal(await page.getByRole("menuitemradio",{name:"High",exact:true}).getAttribute("aria-checked"),"true");await page.screenshot({path:join(temp,"new-session-reasoning.png")});await page.keyboard.press("Escape");await page.keyboard.press("Escape");
  assert.equal((await request("/state")).sessions.length,0);
  const invalid=await fetch(url+"/api/sessions",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({workspaceId:workspace.id,mode:"minimal",model:"mock/plain",thinking:"high"})});assert.equal(invalid.status,400);assert.equal((await request("/state")).sessions.length,0);
  await editor.fill("new-session-fixture");const created=page.waitForResponse(response=>response.url().endsWith("/api/sessions")&&response.request().method()==="POST");await page.getByRole("button",{name:"发送任务",exact:true}).click();const session=await(await created).json();
  await page.getByRole("button",{name:"停止运行及所有子任务",exact:true}).waitFor({state:"detached"});await page.getByText("回答已收到",{exact:true}).waitFor();
  assert.equal(session.thinking,"high");assert.equal((await request(`/sessions/${session.id}`)).thinking,"high");assert.equal(requests[0].reasoning_effort,"high");
  await page.reload();await editor.waitFor();await page.waitForFunction(()=>document.querySelector('[aria-label="选择模型"]')?.textContent?.includes("High"));
  // Configuration changes must reach the composer without sending a message.
  await page.getByRole("button",{name:"选择模型",exact:true}).click();await page.getByRole("menuitem",{name:/^推理等级/}).click();
  await page.getByRole("menuitemradio",{name:"Low",exact:true}).click();
  await page.waitForFunction(()=>document.querySelector('[aria-label="选择模型"]')?.textContent?.includes("Low"));
  assert.equal((await request(`/sessions/${session.id}`)).thinking,"low");
  // Switching updates capabilities without any prompt; the previous snapshot must not supply the old list.
  const countBefore=requests.length;let releaseModel!:()=>void;const modelGate=new Promise<void>(resolve=>releaseModel=resolve);t.after(()=>releaseModel());
  await page.route(`**/api/sessions/${session.id}`,async route=>{if(route.request().method()==="PATCH"&&route.request().postDataJSON().model)await modelGate;await route.continue();});
  await chooseModel("plain");await page.getByRole("button",{name:"选择模型",exact:true}).click();
  assert.match(await page.getByRole("menuitem",{name:"推理等级",exact:true}).innerText(),/加载中/);await page.keyboard.press("Escape");releaseModel();
  await page.waitForFunction(()=>document.querySelector('[aria-label="选择模型"]')?.textContent?.includes("plain"));
  await openThinking();assert.equal(await page.getByRole("menuitemradio").count(),1);await page.getByRole("menuitemradio",{name:"Off",exact:true}).waitFor();await page.keyboard.press("Escape");await page.keyboard.press("Escape");
  await page.unroute(`**/api/sessions/${session.id}`);await chooseModel("limited");await openThinking();assert.equal(await page.getByRole("menuitemradio",{name:"High",exact:true}).count(),0);await page.getByRole("menuitemradio",{name:"XHigh",exact:true}).waitFor();await page.keyboard.press("Escape");await page.keyboard.press("Escape");
  await chooseModel("mock");await openThinking();await page.getByRole("menuitemradio",{name:"High",exact:true}).click();assert.equal(requests.length,countBefore,"capability changes never require a prompt");
  // A temporarily incomplete snapshot falls back to the SDK model catalog.
  await page.route(`**/api/sessions/${session.id}`,async route=>{if(route.request().method()!=="GET"){await route.continue();return;}const response=await route.fetch();await route.fulfill({response,json:{...await response.json(),thinkingLevels:[]}});});
  const capabilities=page.waitForResponse(r=>r.url().includes("/api/models")&&r.status()===200);await page.reload();await capabilities;
  await openThinking();await page.getByRole("menuitemradio",{name:"High",exact:true}).waitFor();await page.getByRole("menuitemradio",{name:"Low",exact:true}).click();
  await page.waitForFunction(()=>document.querySelector('[aria-label="选择模型"]')?.textContent?.includes("Low"));await page.unroute(`**/api/sessions/${session.id}`);
  // Hold an actual provider request while selecting settings for its automatic tool continuation.
  await request(`/sessions/${session.id}`,{thinking:"high"},"PATCH");const beforeSwitch=requests.length;
  await request(`/sessions/${session.id}/prompt`,{text:"live-switch"});await switchStarted;
  await chooseModel("limited");await page.waitForFunction(()=>document.querySelector('[aria-label="选择模型"]')?.textContent?.includes("limited"));
  await openThinking();await page.getByRole("menuitemradio",{name:"Low",exact:true}).click();
  await page.waitForFunction(()=>document.querySelector('[aria-label="选择模型"]')?.textContent?.includes("Low"));
  assert.equal((await request(`/sessions/${session.id}`)).record.status,"running");assert.equal(requests[beforeSwitch].model,"mock");assert.equal(requests[beforeSwitch].reasoning_effort,"high");
  const modeChange=await fetch(url+`/api/sessions/${session.id}`,{method:"PATCH",headers:{"Content-Type":"application/json"},body:JSON.stringify({mode:session.mode==="standard"?"minimal":"standard"})});assert.equal(modeChange.status,400);
  releaseSwitch();await page.getByRole("button",{name:"停止运行及所有子任务",exact:true}).waitFor({state:"detached"});
  assert.equal(requests[beforeSwitch+1].model,"limited");assert.equal(requests[beforeSwitch+1].reasoning_effort,"low");assert.equal(requests[beforeSwitch].reasoning_effort,"high");
  await request(`/sessions/${session.id}`,{model:"mock/mock",thinking:"low"},"PATCH");
  // Fork persisted messages during a real stream; selecting a fork must leave the parent running.
  const parent=await request(`/sessions/${session.id}`),reply=parent.messages.find((m:any)=>m.role==="assistant"),user=parent.messages.find((m:any)=>m.role==="user");
  await request(`/sessions/${session.id}/prompt`,{text:"live-fork"});
  const streamingReply=page.locator("article").filter({hasText:"fork-streaming-fixture"});await streamingReply.waitFor();
  assert.equal(await streamingReply.getByRole("toolbar").count(),0,"unfinished replies have no fork action");
  assert.equal((await request(`/sessions/${session.id}`)).messages.at(-1).entryId,undefined);
  const invalidFork=await fetch(url+`/api/sessions/${session.id}/fork`,{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({entryId:"unsaved-entry"})});assert.equal(invalidFork.status,400);
  const source=await readFile(parent.record.sessionPath,"utf8"),branchIds:string[]=[];
  for(const [label,entry] of [["回复操作",reply],["用户消息操作",user]] as const){
    const toolbar=page.getByRole("toolbar",{name:label,exact:true}).first();
    for(const name of ["编辑","删除","从上下文中排除"])assert.equal(await toolbar.getByRole("button",{name,exact:true}).isDisabled(),true);
    assert.equal(await toolbar.getByRole("button",{name:"fork",exact:true}).isEnabled(),true);
    const forkResponse=page.waitForResponse(r=>r.url().endsWith(`/sessions/${session.id}/fork`)&&r.request().method()==="POST");
    await toolbar.getByRole("button",{name:"fork",exact:true}).click();const response=await forkResponse;assert.equal(response.status(),200);const branch=await response.json();branchIds.push(branch.id);
    await page.waitForFunction(id=>localStorage.getItem("pi-desk.active")===id,branch.id);
    assert.equal((await request(`/sessions/${branch.id}`)).messages.at(-1).entryId,entry.entryId,"fork ends at the selected historical message");
    assert.equal((await request(`/sessions/${session.id}`)).record.status,"running");assert.equal(await readFile(parent.record.sessionPath,"utf8"),source,"fork never rewrites the live parent");
    await page.getByTitle(parent.record.title,{exact:true}).click();await streamingReply.waitFor();
  }
  for(const operation of ["edit","delete","exclude"]){const response=await fetch(url+`/api/sessions/${session.id}/message`,{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({entryId:reply.entryId,operation,text:"blocked while running"})});assert.equal(response.status,400);}
  await page.getByRole("button",{name:`更多操作：${parent.record.title}`,exact:true}).focus();await page.keyboard.press("Enter");
  assert.equal(await page.getByRole("menuitem",{name:"分叉会话",exact:true}).isEnabled(),true);
  const latestFork=page.waitForResponse(r=>r.url().endsWith(`/sessions/${session.id}/fork`)&&r.request().method()==="POST");await page.getByRole("menuitem",{name:"分叉会话",exact:true}).click();const latestResponse=await latestFork;assert.equal(latestResponse.status(),200);const latestBranch=await latestResponse.json();branchIds.push(latestBranch.id);
  await page.waitForFunction(id=>localStorage.getItem("pi-desk.active")===id,latestBranch.id);
  assert.ok(!(await request(`/sessions/${latestBranch.id}`)).messages.some((m:any)=>JSON.stringify(m.content).includes("fork-streaming-fixture")));
  const branchFiles=await Promise.all(branchIds.map(async id=>{const snapshot=await request(`/sessions/${id}`);return {path:snapshot.record.sessionPath,text:await readFile(snapshot.record.sessionPath,"utf8")};}));
  assert.equal((await request(`/sessions/${session.id}`)).record.status,"running");releaseFork();
  const forkDeadline=Date.now()+8000;let completed;do{completed=await request(`/sessions/${session.id}`);if(completed.record.status!=="running")break;await new Promise(resolve=>setTimeout(resolve,25));}while(Date.now()<forkDeadline);
  assert.equal(completed.record.status,"idle");assert.ok(JSON.stringify(completed.messages.at(-1).content).includes("fork-streaming-fixture回答已收到"),"the parent completes after switching to a fork");
  for(const branch of branchFiles)assert.equal(await readFile(branch.path,"utf8"),branch.text,"later parent output never enters existing forks");
  await page.getByTitle(parent.record.title,{exact:true}).click();await page.getByRole("button",{name:"发送任务",exact:true}).waitFor();
  // Windows native file drops retain their absolute paths; blob-only pastes get a real cached path.
  const nativePath=join(project,"docs","设计 (草稿) [v1].md").replaceAll("\\","/");
  await page.route("**/api/files/*/attach",async route=>{if(route.request().postDataJSON().clipboard)await route.fulfill({json:{paths:[nativePath]}});else await route.continue();});
  await editor.click();await editor.evaluate(element=>element.dispatchEvent(new ClipboardEvent("paste",{bubbles:true,cancelable:true,clipboardData:new DataTransfer()})));
  await page.getByRole("button",{name:`预览文件：${nativePath}`,exact:true}).waitFor();
  await editor.click();await editor.press("ControlOrMeta+a");await editor.press("Backspace");await page.unroute("**/api/files/*/attach");
  await page.route("**/api/files/*/attach",async route=>{if(route.request().postDataJSON().clipboard)await route.fulfill({json:{paths:[]}});else await route.continue();});
  const uploaded=page.waitForResponse(response=>response.url().endsWith(`/files/${workspace.id}/attach`)&&response.request().postDataJSON().files);
  await editor.evaluate(element=>{const data=new DataTransfer();data.items.add(new File(["pasted fixture"],"粘贴文件.txt",{type:"text/plain"}));element.dispatchEvent(new ClipboardEvent("paste",{bubbles:true,cancelable:true,clipboardData:data}));});
  const cached=(await(await uploaded).json()).paths[0];assert.equal(await readFile(cached,"utf8"),"pasted fixture");
  await page.getByRole("button",{name:`预览文件：${cached.replaceAll("\\","/")}`,exact:true}).waitFor();
  await editor.click();await editor.press("ControlOrMeta+a");await editor.press("Backspace");await page.unroute("**/api/files/*/attach");
  await editor.evaluate((element,path)=>{const data=new DataTransfer();data.setData("text/plain",`"${path}"`);element.dispatchEvent(new ClipboardEvent("paste",{bubbles:true,cancelable:true,clipboardData:data}));},nativePath);
  await page.getByRole("button",{name:`预览文件：${nativePath}`,exact:true}).waitFor();
  await new Promise(resolve=>setTimeout(resolve,750));await page.reload();await page.getByRole("button",{name:`预览文件：${nativePath}`,exact:true}).waitFor();
  await editor.click();await editor.press("ControlOrMeta+a");await editor.press("Backspace");
  await editor.evaluate(element=>{const data=new DataTransfer();data.setData("text/plain","/todo");element.dispatchEvent(new ClipboardEvent("paste",{bubbles:true,cancelable:true,clipboardData:data}));});
  assert.equal((await editor.innerText()).trim(),"/todo","slash commands remain ordinary text on paste");
  assert.equal(await page.getByRole("button",{name:/^预览文件：/}).count(),0);
  await request(`/sessions/${session.id}`,{draft:""},"PATCH");const reset=page.waitForResponse(response=>response.url().endsWith(`/sessions/${session.id}/composer`));await page.reload();await reset;await editor.waitFor();await editor.fill("Check @app");await options.first().waitFor();assert.equal(await options.count(),2);
  await editor.press("ArrowDown");const path=await options.nth(1).getAttribute("title");await editor.press("Enter");
  await page.getByRole("button",{name:`预览文件：${path}`,exact:true}).waitFor();assert.equal(await picker.count(),0);assert.match(await editor.innerText(),/Check/);
  // A single backspace removes the whole reference; undo restores the node.
  await editor.press("Backspace");await editor.press("Backspace");assert.equal(await page.getByRole("button",{name:`预览文件：${path}`,exact:true}).count(),0);
  await editor.press("ControlOrMeta+z");await page.getByRole("button",{name:`预览文件：${path}`,exact:true}).waitFor();
  await page.getByRole("button",{name:`移除文件引用：${path}`,exact:true}).click();assert.equal(await page.getByRole("button",{name:`预览文件：${path}`,exact:true}).count(),0);
  await page.getByRole("button",{name:"添加文件或调用指令",exact:true}).click();await page.getByRole("menuitem",{name:"引用项目文件",exact:true}).click();await options.first().waitFor();
  await editor.press("ControlOrMeta+End");await editor.pressSequentially("设计");await options.first().waitFor();assert.equal(await options.count(),1);await editor.press("Tab");
  const special="docs/设计 (草稿) [v1].md";await page.getByRole("button",{name:`预览文件：${special}`,exact:true}).waitFor();
  await new Promise(resolve=>setTimeout(resolve,750));const saved=await request(`/sessions/${session.id}/composer`);assert.ok(saved.text.includes("docs/%E8%AE%BE%E8%AE%A1%20%28"));assert.match(saved.text,/\[@设计/);
  await request(`/sessions/${session.id}`,{title:"File reference draft"},"PATCH");const second=await request("/sessions",{workspaceId:workspace.id,mode:"minimal"});await request(`/sessions/${second.id}`,{title:"Empty draft"},"PATCH");
  const sidebar=page.getByRole("region",{name:"工作区会话"}),switched=page.waitForResponse(response=>response.url().endsWith(`/sessions/${second.id}/composer`));
  await sidebar.getByRole("button",{name:/^Empty draft/}).click();await switched;assert.equal((await editor.innerText()).trim(),"");await editor.press("ControlOrMeta+z");assert.equal((await editor.innerText()).trim(),"");
  await sidebar.getByRole("button",{name:/^File reference draft/}).click();await page.getByRole("button",{name:`预览文件：${special}`,exact:true}).waitFor();
  await page.reload();await page.getByRole("button",{name:`预览文件：${special}`,exact:true}).waitFor();
  await editor.click();await editor.press("ControlOrMeta+End");await editor.pressSequentially(" @does-not-exist");await page.getByText("未找到匹配文件，试试文件名或路径。",{exact:true}).waitFor();
  await editor.press("Enter");assert.equal(await picker.count(),1);await editor.press("Escape");assert.equal(await picker.count(),0);
  await editor.fill("test@example.com");await new Promise(resolve=>setTimeout(resolve,200));assert.equal(await picker.count(),0);
  await page.route("**/api/files/*?search=*",route=>route.fulfill({status:503,contentType:"application/json",body:'{"error":"fixture unavailable"}'}));
  await editor.fill("@failure");await page.getByRole("alert").filter({hasText:"无法搜索：fixture unavailable"}).waitFor();await editor.press("Escape");await page.unroute("**/api/files/*?search=*");
  await editor.fill("@app");await options.first().waitFor();await editor.dispatchEvent("compositionstart");await editor.dispatchEvent("compositionend");await editor.press("Enter");assert.equal(await picker.count(),1);await editor.press("Enter");assert.equal(await picker.count(),0);
  await page.route("**/api/sessions/*/prompt",route=>route.fulfill({status:202,contentType:"application/json",body:'{"accepted":true}'}));
  const [sent]=await Promise.all([page.waitForRequest("**/api/sessions/*/prompt"),page.getByRole("button",{name:"发送任务",exact:true}).click()]);
  assert.match(sent.postDataJSON().text,/\[@app\.tsx\]\((docs|src)\/app\.tsx\)/);await page.unroute("**/api/sessions/*/prompt");
  // Real SDK tool execution waits for choice/free-text responses and cancels on stop.
  const interactive=await request("/sessions",{workspaceId:workspace.id,mode:"standard"});
  await request(`/sessions/${interactive.id}`,{title:"Question fixture"},"PATCH");await page.reload();
  const loaded=page.waitForResponse(response=>response.url().endsWith(`/sessions/${interactive.id}/composer`));await page.getByRole("region",{name:"工作区会话"}).getByRole("button",{name:/^Question fixture/}).click();await loaded;
  const card=page.getByRole("form",{name:"助手提问"}),answer=page.getByRole("textbox",{name:"提问回答"});
  const toolRows=page.getByRole("tabpanel",{name:"对话",exact:true}).locator("details").filter({has:page.locator(":scope > summary > code",{hasText:"request_user_input"})});
  for(const scenario of ["choice","text","cancel","stop","delegate"]){
    await editor.fill(`question-${scenario}`);await page.getByRole("button",{name:"发送任务",exact:true}).click();await card.waitFor();
    if(scenario==="choice"){
      await page.screenshot({path:join(temp,"question-desktop.png")});await page.setViewportSize({width:375,height:812});
      const bounds=await card.boundingBox();assert.ok(bounds&&bounds.x>=0&&bounds.x+bounds.width<=375,"question card fits narrow screens");
      await page.screenshot({path:join(temp,"question-mobile.png")});await page.setViewportSize({width:1280,height:900});
      await page.reload();await card.waitFor();await toolRows.waitFor();assert.equal(await toolRows.count(),1);
      assert.match(await toolRows.locator("summary").innerText(),/执行中/);assert.match(await toolRows.innerText(),/调用参数/);
      const runningRow=await toolRows.elementHandle();
      await card.getByRole("button",{name:"最小修改",exact:true}).click();
      await page.waitForFunction(row=>row?.querySelector("summary")?.textContent?.includes("完成"),runningRow);
      assert.equal(await toolRows.count(),1,"completion updates the existing tool row without adding a result row");
      assert.equal(await runningRow!.evaluate(row=>row.isConnected),true,"the running tool row stays mounted");
      await page.getByRole("button",{name:"发送任务",exact:true}).waitFor();
      await page.getByRole("tabpanel",{name:"对话",exact:true}).getByLabel(/展开或收起执行过程/).click();
      await toolRows.locator("summary").click();assert.match(await toolRows.innerText(),/调用参数[\s\S]*执行结果[\s\S]*最小修改/);
      await page.screenshot({path:join(temp,"tool-call-completed.png")});
      await page.reload();await editor.waitFor();await page.getByRole("tabpanel",{name:"对话",exact:true}).getByLabel(/展开或收起执行过程/).click();
      await toolRows.waitFor();assert.equal(await toolRows.count(),1);assert.match(await toolRows.locator("summary").innerText(),/完成/);
    }
    else if(scenario==="delegate")await card.getByRole("button",{name:"最小修改",exact:true}).click();
    else if(scenario==="text"){await answer.fill("使用现有组件");await answer.press("Enter");}
    else if(scenario==="cancel")await card.getByRole("button",{name:"取消提问",exact:true}).click();
    else await page.getByRole("button",{name:"停止运行及所有子任务",exact:true}).click();
    await card.waitFor({state:"detached"});await page.getByRole("button",{name:"发送任务",exact:true}).waitFor();
  }
  assert.ok(answers.some(answer=>answer.includes("最小修改")));assert.ok(answers.some(answer=>answer.includes("使用现有组件")));assert.ok(answers.some(answer=>answer.includes("cancelled")));
  assert.ok((await request("/state")).sessions.some((r:any)=>r.depth===2),"grandchild questions are answered from the root composer");
  const plugins=await request(`/plugins/${workspace.id}`);assert.ok(plugins.some((plugin:any)=>plugin.id==="builtin:question"&&plugin.enabled));
  await request(`/plugins/${workspace.id}`,{id:"builtin:question",enabled:false});assert.ok(!(await request(`/sessions/${interactive.id}`)).tools.includes("request_user_input"));
  await request(`/plugins/${workspace.id}`,{id:"builtin:question",enabled:true});assert.ok((await request(`/sessions/${interactive.id}`)).tools.includes("request_user_input"));
  // Repeated retry failures collapse visually; their native entries and final action target survive.
  const {SessionManager}=await import("@earendil-works/pi-coding-agent"),retryHistory=SessionManager.create(project,join(agent,"sessions"));
  const firstRetryUser=retryHistory.appendMessage({role:"user",content:"Repeated connection errors",timestamp:Date.now()});retryHistory.appendSessionInfo("Repeated connection errors");
  const appendFailures=(count:number,errorMessage="Connection error.",excludeAll=false,content:any[]=[])=>{let id="";for(let i=0;i<count;i++){id=retryHistory.appendMessage({role:"assistant",api:"openai-completions",provider:"mock",model:"mock",stopReason:"error",usage:{input:0,output:0,cacheRead:0,cacheWrite:0,totalTokens:0,cost:{input:0,output:0,cacheRead:0,cacheWrite:0,total:0}},content,errorMessage,timestamp:Date.now()});if(excludeAll||i<count-1)retryHistory.appendContextEdit(id,null);}return id;};
  const lastFailure=appendFailures(4);
  const nextRetryUser=retryHistory.appendMessage({role:"user",content:"Separate retry round",timestamp:Date.now()});retryHistory.appendContextEdit(nextRetryUser,null);const excludedFailure=appendFailures(1,"Connection error.",true);
  const interruptedUser=retryHistory.appendMessage({role:"user",content:"Interrupted retries",timestamp:Date.now()});appendFailures(2,"Connection error.",true);
  appendFailures(1,"Connection error.",false,[{type:"toolCall",id:"retry-read",name:"read",arguments:{path:"src/app.tsx"}}]);
  retryHistory.appendMessage({role:"toolResult",toolCallId:"retry-read",toolName:"read",content:[{type:"text",text:"retry tool result"}],isError:false,timestamp:Date.now()});
  appendFailures(2,"Connection error.",true);appendFailures(1,"Request timeout.");appendFailures(1,"Connection error.",false,[{type:"text",text:"Partial answer before failure"}]);appendFailures(1);
  retryHistory.appendCompaction("Retry compaction boundary",interruptedUser,1000);appendFailures(1);
  const recoveredUser=retryHistory.appendMessage({role:"user",content:"Recovered retry round",timestamp:Date.now()});appendFailures(3,"Connection error.",true);
  const successfulReply={...(retryHistory.getEntry(lastFailure) as any).message,stopReason:"stop",errorMessage:undefined,content:[{type:"text",text:"Retry recovered successfully"}],timestamp:Date.now()};retryHistory.appendMessage(successfulReply);
  const failedAgainUser=retryHistory.appendMessage({role:"user",content:"Later failed retry round",timestamp:Date.now()});appendFailures(2);
  const retryPath=retryHistory.getSessionFile()!,retryBytes=await readFile(retryPath,"utf8");await request("/discover",{});
  await page.getByTitle("Repeated connection errors",{exact:true}).click();
  const retryConversation=page.getByRole("tabpanel",{name:"对话",exact:true}),firstRetryTurn=retryConversation.locator(`[data-message-id="${firstRetryUser}"]`);
  await firstRetryTurn.getByText("连续出现 4 次",{exact:false}).waitFor();
  assert.equal(await firstRetryTurn.locator("article").count(),2);assert.equal(await firstRetryTurn.getByText("已从上下文排除",{exact:true}).count(),0,"the merged error uses the final attempt's context state");
  const excludedTurn=retryConversation.locator(`[data-message-id="${nextRetryUser}"]`);
  assert.equal(await excludedTurn.locator("article").filter({hasText:"Connection error."}).getByText("已从上下文排除",{exact:true}).count(),0,"connection errors hide the exclusion note");
  assert.equal(await excludedTurn.locator("article").filter({hasText:"Separate retry round"}).getByText("已从上下文排除",{exact:true}).count(),1,"ordinary excluded messages retain their note");
  const interrupted=retryConversation.locator(`[data-message-id="${interruptedUser}"]`);await interrupted.getByLabel(/展开或收起执行过程/).click();
  assert.equal(await interrupted.getByText("连续出现 2 次",{exact:false}).count(),2,"tool calls split repeated error groups");await interrupted.getByText("Request timeout.",{exact:true}).waitFor();await interrupted.getByText("Partial answer before failure",{exact:true}).waitFor();
  const recoveredTurn=retryConversation.locator(`[data-message-id="${recoveredUser}"]`);await recoveredTurn.getByText("Retry recovered successfully",{exact:true}).waitFor();assert.equal(await recoveredTurn.locator("article").count(),2,"a completed retry hides earlier connection errors in its own turn");assert.equal(await recoveredTurn.getByText("Connection error.",{exact:false}).count(),0);
  await retryConversation.locator(`[data-message-id="${failedAgainUser}"]`).getByText("连续出现 2 次",{exact:false}).waitFor();
  const retryFork=page.waitForResponse(r=>r.url().endsWith(`/sessions/${retryHistory.getSessionId()}/fork`));await firstRetryTurn.getByRole("button",{name:"fork",exact:true}).last().click();const retryBranch=await(await retryFork).json();
  assert.equal((await request(`/sessions/${retryBranch.id}`)).messages.at(-1).entryId,lastFailure,"merged actions target the final failed attempt");
  await page.getByTitle("Repeated connection errors",{exact:true}).click();await page.reload();await firstRetryTurn.getByText("连续出现 4 次",{exact:false}).waitFor();
  const retrySnapshot=await request(`/sessions/${retryHistory.getSessionId()}`);assert.equal(retrySnapshot.messages.find((m:any)=>m.entryId===excludedFailure).contextExcluded,true,"hiding the note preserves the actual exclusion");assert.equal(retrySnapshot.messages.filter((m:any)=>m.errorMessage).length,19,"all original errors remain available to the trajectory");assert.equal(await recoveredTurn.getByText("Connection error.",{exact:false}).count(),0,"recovered errors stay hidden after reload");assert.ok((await readFile(retryPath,"utf8")).startsWith(retryBytes),"display merging preserves every original entry");await retryConversation.getByText("上下文已压缩",{exact:true}).waitFor();
  const recoveredMessages=retrySnapshot.messages.slice(retrySnapshot.messages.findIndex((m:any)=>m.entryId===recoveredUser)+1,retrySnapshot.messages.findIndex((m:any)=>m.entryId===failedAgainUser)),recoveredErrors=recoveredMessages.filter((m:any)=>m.errorMessage);assert.equal(recoveredErrors.length,3);
  await page.getByRole("tab",{name:"轨迹",exact:true}).click();for(const error of recoveredErrors)await page.locator(`[data-trace-id^="message-${error.entryId}-"]`).waitFor();await page.getByRole("tab",{name:"对话",exact:true}).click();
  await page.screenshot({path:join(temp,"merged-retry-errors.png")});
  for(const name of ["../escape.txt","a:stream",".."]){const response=await fetch(url+`/api/files/${workspace.id}/attach`,{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({files:[{name,data:"aGk="}]})});assert.equal(response.status,400);}
  await page.getByRole("button",{name:"设置",exact:true}).click();await page.getByRole("navigation",{name:"设置",exact:true}).getByRole("button",{name:"模型管理",exact:true}).click();
  await page.getByRole("button",{name:"编辑 mock/mock",exact:true}).click();const modelDialog=page.getByRole("dialog",{name:"编辑模型",exact:true});
  await modelDialog.getByRole("textbox",{name:"模型 ID",exact:true}).waitFor();assert.equal(await modelDialog.getByLabel("模型显示名称",{exact:true}).count(),0);assert.equal(await page.getByText("清除 models.json 中的 API Key",{exact:true}).count(),0);
  await modelDialog.getByRole("button",{name:"关闭",exact:true}).click();await page.getByRole("button",{name:"管理供应商 mock",exact:true}).click();await page.getByRole("menuitem",{name:"供应商设置",exact:true}).click();
  const providerDialog=page.getByRole("dialog",{name:"供应商设置",exact:true});await providerDialog.getByText("供应商代理",{exact:true}).click();await providerDialog.getByLabel("供应商连接方式",{exact:true}).waitFor();assert.equal(await providerDialog.getByText("清除 models.json 中的 API Key",{exact:true}).count(),0);
  await providerDialog.getByRole("button",{name:"关闭",exact:true}).click();await page.locator("summary").filter({hasText:/^代理设置$/}).click();await page.getByRole("textbox",{name:"全局代理",exact:true}).waitFor();assert.equal(await page.getByLabel("模型代理",{exact:true}).count(),0);
  const gate=join(temp,"finish-startup"),entered=join(temp,"startup-entered");
  await writeFile(join(project,".pi","mcp.json"),JSON.stringify({mcpServers:{"slow-fixture":{url:`http://127.0.0.1:${(model.address() as {port:number}).port}/mcp`,headers:{Authorization:"Bearer timing-fixture-secret"}}}}));
  await writeFile(join(project,".pi","extensions","slow-start.ts"),`import {existsSync,writeFileSync} from "node:fs";
  export default function(pi){pi.on("session_start",async()=>{
    writeFileSync(${JSON.stringify(entered)}, "");
    const deadline=Date.now()+60000;
    while(!existsSync(${JSON.stringify(gate)})&&Date.now()<deadline)await new Promise(resolve=>setTimeout(resolve,25));
  });}`);
  const coldId=randomUUID(),timestamp=new Date().toISOString(),messageId=randomUUID();
  await writeFile(join(agent,"sessions","cold-history.jsonl"),[
    {type:"session",version:3,id:coldId,cwd:project,timestamp},
    {type:"message",id:messageId,parentId:null,timestamp,message:{role:"user",content:[{type:"text",text:"Saved history before startup"}],timestamp:Date.now()}},
    ...[
      {role:"assistant",api:"openai-completions",provider:"mock",model:"mock",stopReason:"toolUse",usage:{input:1,output:1,cacheRead:0,cacheWrite:0,totalTokens:2,cost:{input:0,output:0,cacheRead:0,cacheWrite:0,total:0}},content:[{type:"toolCall",id:"first",name:"bash",arguments:{command:"echo same"}},{type:"toolCall",id:"second",name:"bash",arguments:{command:"echo same"}},{type:"toolCall",id:"pending",name:"read",arguments:{path:"pending.txt"}}]},
      {role:"toolResult",toolCallId:"second",toolName:"bash",content:[{type:"text",text:"second result"},{type:"image",mimeType:"image/png",data:"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aS9sAAAAASUVORK5CYII="}]},
      {role:"toolResult",toolCallId:"first",toolName:"bash",isError:true,content:[{type:"text",text:"first failed"}]},
      {role:"toolResult",toolCallId:"orphan",toolName:"orphan_tool",content:[{type:"text",text:"standalone result"}]},
    ].map((message,index)=>({type:"message",id:`tool-${index}`,parentId:index?`tool-${index-1}`:messageId,timestamp,message:{...message,timestamp:Date.now()+index}})),
  ].map(entry=>JSON.stringify(entry)).join("\n")+"\n");
  await request("/discover",{});await page.keyboard.press("Escape");
  let snapshotReady=false;
  const full=page.waitForResponse(r=>r.url().endsWith(`/api/sessions/${coldId}`),{timeout:20000}).then(response=>{snapshotReady=true;return response;});
  try{
    const started=performance.now();await page.getByRole("button",{name:/^Saved history before startup/}).click();
    const history=page.getByRole("tabpanel",{name:"对话",exact:true}).getByText("Saved history before startup",{exact:true});
    await history.waitFor();const elapsed=Math.round(performance.now()-started);
    const deadline=Date.now()+8000;while(!existsSync(entered)&&Date.now()<deadline)await new Promise(resolve=>setTimeout(resolve,25));
    assert.ok(existsSync(entered),snapshotReady?await (await full).text():"the real SDK did not enter the startup hook");assert.equal(snapshotReady,false,"saved history is visible before the SDK snapshot");
    await page.getByRole("button",{name:"选择模型",exact:true}).click();assert.doesNotMatch(await page.getByRole("menuitem",{name:"推理等级",exact:true}).innerText(),/不可用/);await page.keyboard.press("Escape");
    const conversation=page.getByRole("tabpanel",{name:"对话",exact:true});await conversation.getByLabel(/展开或收起执行过程/).click();
    const bashRows=conversation.locator("details").filter({has:page.locator(":scope > summary > code",{hasText:/^bash$/})});
    assert.equal(await bashRows.count(),2,"identical commands with distinct toolCallIds stay separate");
    assert.match(await bashRows.nth(0).locator("summary").innerText(),/失败/);assert.match(await bashRows.nth(1).locator("summary").innerText(),/完成/);
    for(const [i,output] of ["first failed","second result"].entries()){await bashRows.nth(i).locator("summary").click();assert.match(await bashRows.nth(i).innerText(),new RegExp(`调用参数[\\s\\S]*执行结果[\\s\\S]*${output}`));}
    assert.equal(await bashRows.nth(1).getByRole("img",{name:"工具返回图片",exact:true}).count(),1);
    await conversation.getByText("orphan_tool",{exact:true}).click();await conversation.getByText("standalone result",{exact:false}).waitFor();
    assert.match(await conversation.locator("summary").filter({hasText:"pending.txt"}).innerText(),/未完成/);
    const pendingDeadline=Date.now()+7000;
    const pluginPending=()=>timings().find(row=>row.sessionId===coldId&&row.plugin==="slow-start.ts"&&row.phase==="plugin.session_start"&&row.event==="pending");
    while(!pluginPending()&&Date.now()<pendingDeadline)await new Promise(resolve=>setTimeout(resolve,50));
    assert.ok(pluginPending()?.elapsedMs>=4900,"a blocked plugin is named before the request ends");
    console.log(`Cold history visible in ${elapsed} ms with SDK startup blocked`);
  }finally{await writeFile(gate,"");await full.catch(()=>{});}
  assert.equal((await full).ok(),true);await openThinking();await page.getByRole("menuitemradio",{name:"High",exact:true}).waitFor();await page.keyboard.press("Escape");await page.keyboard.press("Escape");
  const mcpDeadline=Date.now()+6000;
  const mcpDone=()=>timings().some(row=>row.sessionId===coldId&&row.server==="slow-fixture"&&row.phase==="mcp.tools/list"&&row.event==="end");
  while(!mcpDone()&&Date.now()<mcpDeadline)await new Promise(resolve=>setTimeout(resolve,50));
  const coldTimings=timings().filter(row=>row.sessionId===coldId&&row.event==="end");
  assert.ok(coldTimings.some(row=>row.plugin==="slow-start.ts"&&row.elapsedMs>=4900));
  assert.ok(coldTimings.some(row=>row.server==="slow-fixture"&&row.phase==="mcp.initialize"&&row.elapsedMs>=140));assert.ok(mcpDone());
  for(const phase of ["request.history","request.snapshot","plugins.discover","resources.reload","history.open","history.project","response.serialize"])assert.ok(coldTimings.some(row=>row.phase===phase),phase);
  assert.doesNotMatch(stderr,/timing-fixture-secret|Saved history before startup/);
  console.log(`Composer visual fixtures: ${temp}`);assert.deepEqual(errors,[]);
});
