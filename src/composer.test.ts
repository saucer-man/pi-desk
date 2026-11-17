import {test} from "node:test";
import assert from "node:assert/strict";
import {existsSync} from "node:fs";
import {mkdtemp,mkdir,writeFile,readFile} from "node:fs/promises";
import {tmpdir} from "node:os";
import {join,resolve} from "node:path";
import {spawn} from "node:child_process";
import {once} from "node:events";
import {createServer} from "node:http";
import {chromium} from "playwright-core";

test("composer: project file search, references, file paste, reasoning sync and interactive questions",{timeout:60000},async t=>{
  const executablePath=[process.env.PI_DESK_BROWSER_EXECUTABLE,"C:/Program Files/Google/Chrome/Application/chrome.exe","C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe","/Applications/Google Chrome.app/Contents/MacOS/Google Chrome","/usr/bin/chromium","/usr/bin/google-chrome"].find(path=>path&&existsSync(path));
  if(!executablePath){t.skip("Chrome / Edge / Chromium unavailable");return;}
  const temp=await mkdtemp(join(tmpdir(),"pi-desk-composer-")),project=join(temp,"project"),agent=join(temp,"agent");
  await mkdir(join(project,"src"),{recursive:true});await mkdir(join(project,"docs"));await mkdir(agent);
  for(const path of ["src/app.tsx","docs/app.tsx","docs/设计 (草稿) [v1].md"])await writeFile(join(project,path),"fixture content");
  const answers:string[]=[];
  const model=createServer(async(req,res)=>{
    let body="";for await(const chunk of req)body+=chunk;const request=JSON.parse(body),last=request.messages.at(-1);
    res.writeHead(200,{"Content-Type":"text/event-stream"});
    const tool=last.role!=="tool"&&JSON.stringify(last).includes("question-");
    if(last.role==="tool")answers.push(last.content);
    const delegated=last.role!=="tool"&&JSON.stringify(last).includes("question-delegate");
    const args=delegated?{tasks:[{task:JSON.stringify(last).includes("question-delegate-child")?"question-choice":"question-delegate-child",mode:"standard",access:"read"}]}:{question:"请选择实现方式",...(JSON.stringify(last).includes("question-choice")?{options:["最小修改","完整重写"]}:{})};
    for(const chunk of [{choices:[{index:0,delta:tool?{role:"assistant",tool_calls:[{index:0,id:"ask",type:"function",function:{name:delegated?"subagent":"request_user_input",arguments:JSON.stringify(args)}}]}:{role:"assistant",content:"回答已收到"},finish_reason:null}]},{choices:[{index:0,delta:{},finish_reason:tool?"tool_calls":"stop"}]},{choices:[],usage:{prompt_tokens:10,completion_tokens:3,total_tokens:13}}])res.write(`data: ${JSON.stringify({id:"fixture",object:"chat.completion.chunk",created:1,model:"mock",...chunk})}\n\n`);
    res.end("data: [DONE]\n\n");
  });model.listen(0,"127.0.0.1");await once(model,"listening");t.after(()=>{model.close();model.closeAllConnections();});
  await writeFile(join(agent,"models.json"),JSON.stringify({providers:{mock:{api:"openai-completions",baseUrl:`http://127.0.0.1:${(model.address() as {port:number}).port}/v1`,apiKey:"fixture",models:[{id:"mock",reasoning:true,contextWindow:100000,maxTokens:1000}]}}}));
  await writeFile(join(agent,"settings.json"),JSON.stringify({defaultProvider:"mock",defaultModel:"mock"}));
  const child=spawn(process.execPath,[resolve("dist/server.js"),"--port","0","--no-open"],{windowsHide:true,env:{...process.env,PI_DESK_DIR:join(temp,"desk"),PI_CODING_AGENT_DIR:agent,PI_CODING_AGENT_SESSION_DIR:join(agent,"sessions")},stdio:["ignore","pipe","pipe"]});
  t.after(async()=>{if(child.exitCode===null){const exited=once(child,"exit");child.kill();await exited;}});
  const url=await new Promise<string>((resolve,reject)=>{const timer=setTimeout(()=>reject(Error("production server startup timeout")),15000);child.once("error",reject);child.stdout.on("data",chunk=>{const match=String(chunk).match(/http:\/\/127\.0\.0\.1:\d+/);if(match){clearTimeout(timer);resolve(match[0]);}});child.once("exit",code=>{clearTimeout(timer);reject(Error(`production server exited: ${code}`));});});
  const request=async(path:string,data?:unknown,method=data?"POST":"GET")=>{const response=await fetch(url+"/api"+path,{method,headers:data?{"Content-Type":"application/json"}:undefined,body:data?JSON.stringify(data):undefined});const body=await response.json();assert.ok(response.ok,JSON.stringify(body));return body;};
  const workspace=await request("/workspaces",{path:project}),session=await request("/sessions",{workspaceId:workspace.id,mode:"minimal"});
  const browser=await chromium.launch({executablePath,headless:true});t.after(()=>browser.close());
  const page=await browser.newPage({viewport:{width:1280,height:900}}),errors:string[]=[];page.setDefaultTimeout(8000);page.on("pageerror",error=>errors.push(error.message));
  const restored=page.waitForResponse(response=>response.url().endsWith(`/sessions/${session.id}/composer`));await page.goto(url);await restored;
  const editor=page.getByRole("textbox",{name:"任务输入",exact:true}),options=page.getByRole("option"),picker=page.getByRole("listbox",{name:"项目文件搜索结果"});
  await editor.waitFor();
  // Configuration changes must reach the composer without sending a message.
  await page.getByRole("button",{name:"选择模型",exact:true}).click();await page.getByRole("menuitem",{name:/^推理等级/}).click();
  await page.getByRole("menuitemradio",{name:"High",exact:true}).click();
  await page.waitForFunction(()=>document.querySelector('[aria-label="选择模型"]')?.textContent?.includes("High"));
  assert.equal((await request(`/sessions/${session.id}`)).thinking,"high");
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
  await editor.click();await editor.press("ControlOrMeta+a");await editor.press("Backspace");await editor.fill("请检查 @app");await options.first().waitFor();assert.equal(await options.count(),2);
  await editor.press("ArrowDown");const path=await options.nth(1).getAttribute("title");await editor.press("Enter");
  await page.getByRole("button",{name:`预览文件：${path}`,exact:true}).waitFor();assert.equal(await picker.count(),0);assert.match(await editor.innerText(),/请检查/);
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
  await page.getByRole("region",{name:"工作区会话"}).getByRole("button",{name:/^Question fixture/}).click();
  const card=page.getByRole("form",{name:"助手提问"}),answer=page.getByRole("textbox",{name:"提问回答"});
  for(const scenario of ["choice","text","cancel","stop","delegate"]){
    await editor.fill(`question-${scenario}`);await page.getByRole("button",{name:"发送任务",exact:true}).click();await card.waitFor();
    if(scenario==="choice"){
      await page.screenshot({path:join(temp,"question-desktop.png")});await page.setViewportSize({width:375,height:812});
      const bounds=await card.boundingBox();assert.ok(bounds&&bounds.x>=0&&bounds.x+bounds.width<=375,"question card fits narrow screens");
      await page.screenshot({path:join(temp,"question-mobile.png")});await page.setViewportSize({width:1280,height:900});
      await page.reload();await card.waitFor();await card.getByRole("button",{name:"最小修改",exact:true}).click();
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
  for(const name of ["../escape.txt","a:stream",".."]){const response=await fetch(url+`/api/files/${workspace.id}/attach`,{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({files:[{name,data:"aGk="}]})});assert.equal(response.status,400);}
  await page.getByRole("button",{name:"设置",exact:true}).click();await page.getByRole("navigation",{name:"设置",exact:true}).getByRole("button",{name:"模型管理",exact:true}).click();
  await page.getByRole("button",{name:"编辑 mock/mock",exact:true}).click();const modelDialog=page.getByRole("dialog",{name:"编辑模型",exact:true});
  await modelDialog.getByRole("textbox",{name:"模型 ID",exact:true}).waitFor();assert.equal(await modelDialog.getByLabel("模型显示名称",{exact:true}).count(),0);assert.equal(await page.getByText("清除 models.json 中的 API Key",{exact:true}).count(),0);
  await modelDialog.getByRole("button",{name:"关闭",exact:true}).click();await page.getByRole("button",{name:"管理供应商 mock",exact:true}).click();await page.getByRole("menuitem",{name:"供应商设置",exact:true}).click();
  const providerDialog=page.getByRole("dialog",{name:"供应商设置",exact:true});await providerDialog.getByText("供应商代理",{exact:true}).click();await providerDialog.getByLabel("供应商连接方式",{exact:true}).waitFor();assert.equal(await providerDialog.getByText("清除 models.json 中的 API Key",{exact:true}).count(),0);
  await providerDialog.getByRole("button",{name:"关闭",exact:true}).click();await page.locator("summary").filter({hasText:/^代理设置$/}).click();await page.getByRole("textbox",{name:"全局代理",exact:true}).waitFor();assert.equal(await page.getByLabel("模型代理",{exact:true}).count(),0);
  console.log(`Composer visual fixtures: ${temp}`);assert.deepEqual(errors,[]);
});
