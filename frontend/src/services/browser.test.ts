// @vitest-environment node
import { readFileSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createRequire } from "node:module";
import { createContext, runInContext, runInNewContext } from "node:vm";
import { Window } from "happy-dom";
import ts from "typescript";
import { describe, expect, it } from "vitest";

// Exercise the shipped Pi extension, not a second implementation of its tools.
const source = readFileSync(new URL("../../../internal/appservice/resources/pi-desk-browser.ts", import.meta.url), "utf8");
const compiled = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 }, reportDiagnostics: true });
const require = createRequire(import.meta.url);

function harness(native = false, cwd = process.cwd()) {
  const tools: Record<string, any> = {}, events: Record<string, any> = {}, calls: any[] = [];
  const windows = [new Window({url:"https://example.test/a"}), new Window({url:"https://example.test/b"})];
  const contexts = windows.map(w => {
    w.document.body.innerHTML = '<label for="name">Name</label><input id="name"><button id="send">Send</button>';
    for (const el of w.document.querySelectorAll("input,button")) {
      el.getBoundingClientRect = () => new w.DOMRect(10,20,100,30);
    }
    Object.defineProperty(w.document, "elementFromPoint", {configurable:true,value:()=>w.document.querySelector("input")});
    return createContext({document:w.document,location:w.location,innerWidth:800,innerHeight:600,
      getComputedStyle:()=>({visibility:"visible"}),HTMLInputElement:w.HTMLInputElement,HTMLTextAreaElement:w.HTMLTextAreaElement,HTMLSelectElement:w.HTMLSelectElement,Event:w.Event});
  });
  let expired = false;
  const exports: any = {};
  const type = new Proxy({}, {get:(_t,key)=> (...args: any[])=>({kind:key,args})});
  runInNewContext(compiled.outputText, {
    exports, require:(name:string)=> name === "typebox" ? {Type:type} : name === "@earendil-works/pi-ai" ? {StringEnum:(values:unknown)=>values} : require(name),
    process:{platform:"win32",env:native ? process.env : {PI_DESK_BROWSER_URL:"http://127.0.0.1/browser",PI_DESK_BROWSER_TOKEN:"test",PI_DESK_BROWSER_THREAD:"thread"}},
    Buffer, URL, AbortSignal,
    fetch:native ? fetch : async (_url:string, options:any) => {
      const c=JSON.parse(options.body); calls.push(c);
      options.signal.throwIfAborted();
      if (expired && ["ensure","call","cdp","inspect"].includes(c.action)) return {ok:false,json:async()=>({error:"browser connection expired"})};
      const index=c.tabId === "b" ? 1 : 0;
      let value:any = {};
      if (c.action === "ensure") value={tabId:c.tabId,url:windows[index].location.href};
      if (c.action === "create") value={tabId:"created",url:c.params.url||"about:blank"};
      if (c.action === "cdp") value={endpoint:"ws://127.0.0.1/browser/cdp?tabId="+c.tabId};
      if (c.method === "events") value={events:[{sequence:1,params:{scriptId:"s1",url:"https://example.test/probe.js"}}],cursor:1,hasMore:false,dropped:0};
      if (c.method === "Debugger.getScriptSource") value={scriptSource:"const value = 42;\nconsole.log(value);"};
      if (c.method === "Debugger.searchInContent") value={result:[{lineNumber:1,lineContent:"console.log(value);"}]};
      if (c.method === "Debugger.evaluateOnCallFrame") value={result:{value:42}};
      if (c.method === "Page.getFrameTree") value={frameTree:{frame:{id:c.tabId}}};
      if (c.method === "Page.createIsolatedWorld") value={executionContextId:index+1};
      if (c.method === "Page.getLayoutMetrics") value={cssVisualViewport:{clientWidth:800,clientHeight:600},cssContentSize:{width:800,height:600}};
      if (c.method === "Page.captureScreenshot") value={data:Buffer.from([0xff,0xd8,0xff,0xc0,0,17,8,2,88,3,32,3,1,0,0]).toString("base64")};
      if (c.method === "Runtime.evaluate") {
        try { value={result:{value:runInContext(c.params.expression,contexts[(c.params.contextId??1)-1])}}; }
        catch(e) { value={exceptionDetails:{exception:{description:String(e)}}}; }
      }
      if (c.method === "Page.getNavigationHistory") value={currentIndex:1,entries:[{id:10},{id:20},{id:30}]};
      return {ok:true,json:async()=>value};
    },
  });
  exports.default({registerTool:(tool:any)=>tools[tool.name]=tool,on:(event:string,handler:any)=>events[event]=handler});
  const execute=(name:string,params:any,signal?:AbortSignal)=>tools[name].execute("test",params,signal,undefined,{cwd});
  return {tools,events,calls,windows,contexts,execute,expire:()=>{expired=true;}};
}

describe("bundled embedded browser tools", () => {
  it("selects native options without pointer input or submitting the form", async () => {
    const h=harness(), w=h.windows[0];
    w.document.body.innerHTML='<select><option value="a">Alpha</option><option value="b">Beta</option><option disabled value="c">Disabled</option></select><button>Submit</button>';
    const select=w.document.querySelector('select')!;
    select.getBoundingClientRect=()=>new w.DOMRect(10,20,100,30);
    Object.defineProperty(w.document,'elementFromPoint',{value:()=>select});
    let changes=0, clicks=0;
    select.addEventListener('change',()=>changes++);
    w.document.body.addEventListener('click',()=>clicks++);
    const read=async()=>JSON.parse((await h.execute('browser_read',{tabId:'a'})).content[0].text).page.elements[0];
    const first=await read();
    expect(first.options).toContainEqual({value:'b',label:'Beta',disabled:false});
    await h.execute('browser_select',{tabId:'a',ref:first.ref,value:'b'});
    expect(select.value).toBe('b');
    expect(changes).toBe(1);
    expect(clicks).toBe(0);
    expect(h.calls.some(c=>c.method==='Input.dispatchMouseEvent')).toBe(false);
    await expect(h.execute('browser_select',{tabId:'a',ref:first.ref,value:'a'})).rejects.toThrow('Stale');
    await expect(h.execute('browser_select',{tabId:'a',ref:(await read()).ref,value:'c'})).rejects.toThrow('disabled');
  });
  it("merges source and debugger operations into scoped native tools",async()=>{
    const h=harness();
    const read=JSON.parse((await h.execute("browser_scripts",{tabId:"b",action:"read",scriptId:"s1",offset:6,length:5})).content[0].text);
    expect(read.text).toBe("value");
    await h.execute("browser_scripts",{tabId:"b",action:"search",query:"value"});
    await h.execute("browser_debug",{tabId:"b",action:"setBreakpoint",scriptId:"s1",text:"log(value)"});
    expect(h.calls.at(-1)).toMatchObject({action:"inspect",tabId:"b",method:"Debugger.setBreakpoint",params:{location:{scriptId:"s1",lineNumber:1,columnNumber:8}}});
    await h.execute("browser_evaluate",{tabId:"b",expression:"value",callFrameId:"frame-1"});
    expect(h.calls.at(-1)).toMatchObject({method:"Debugger.evaluateOnCallFrame",params:{callFrameId:"frame-1"}});
    expect(h.calls.every(c=>c.action==="inspect" && c.tabId==="b")).toBe(true);
    h.expire();
    await expect(h.execute("browser_debug",{tabId:"b",action:"resume"})).rejects.toThrow("expired");
  });
  it("exports sources without overwrite or workspace traversal",async()=>{
    const cwd=mkdtempSync(join(tmpdir(),"pi-browser-export-"));
    try {
      const h=harness(false,cwd);
      await h.execute("browser_scripts",{tabId:"a",action:"save",scriptId:"s1",outputFile:"source.js"});
      expect(readFileSync(join(cwd,"source.js"),"utf8")).toContain("const value = 42");
      await expect(h.execute("browser_scripts",{tabId:"a",action:"save",scriptId:"s1",outputFile:"source.js"})).rejects.toThrow();
      await expect(h.execute("browser_scripts",{tabId:"a",action:"save",scriptId:"s1",outputFile:"../outside.js"})).rejects.toThrow("workspace");
      writeFileSync(join(cwd,"evaluate.js"),"1 + 2");
      const result=JSON.parse((await h.execute("browser_evaluate",{tabId:"a",inputFile:"evaluate.js"})).content[0].text);
      expect(result.result.value).toBe(3);
    } finally { rmSync(cwd,{recursive:true,force:true}); }
  });
  it("captures network, websocket and console through the existing broker",async()=>{
    const h=harness();
    for (const kind of ["network","websocket","console"]) {
      await h.execute("browser_"+kind,{tabId:"a",action:"list",since:12,limit:5});
      expect(h.calls.at(-1)).toMatchObject({action:"inspect",method:"events",params:{kind,since:12,limit:5}});
    }
    await h.execute("browser_network",{tabId:"a",action:"body",requestId:"request-1"});
    expect(h.calls.at(-1)).toMatchObject({method:"Network.getResponseBody",params:{requestId:"request-1"}});
    await h.execute("browser_network",{tabId:"a",action:"clear"});
    expect(h.calls.at(-1).method).toBe("clear");
    const c=new AbortController();c.abort();
    await expect(h.execute("browser_scripts",{tabId:"a",action:"list"},c.signal)).rejects.toThrow("cancelled");
  });
  it("provides a managed exact-tab Playwright connection without launching another browser",async()=>{
    const h=harness();
    const result=JSON.parse((await h.execute("browser_playwright",{tabId:"b"})).content[0].text);
    expect(h.calls).toEqual([expect.objectContaining({action:"cdp",tabId:"b",threadId:"thread"})]);
    expect(result.attach).toContain("@playwright/cli@0.1.21");
    expect(result.attach).toContain('attach --cdp="ws://127.0.0.1/browser/cdp?tabId=b"');
    expect(result.instructions.join(" ")).toContain("NEVER open");
    h.expire();
    await expect(h.execute("browser_playwright",{tabId:"b"})).rejects.toThrow("expired");
  });
  it.skipIf(!process.env.PI_DESK_BROWSER_NATIVE_TEST)("runs the shipped tools through real WebView2", async () => {
    const h=harness(true), tabId="native-test";
    await h.execute("browser_wait",{tabId,condition:"element",selector:"#field"});
    let read=JSON.parse((await h.execute("browser_read",{tabId})).content[0].text);
    await h.execute("browser_select",{tabId,ref:read.page.elements.find((e:any)=>e.tag==="select").ref,value:"b"});
    read=JSON.parse((await h.execute("browser_read",{tabId})).content[0].text);
    expect(read.page.elements.find((e:any)=>e.tag==="select").value).toBe("b");
    expect(read.page.text).not.toContain('{"ok":true}');
    const ref=read.page.elements.find((e:any)=>e.tag==="input").ref;
    await h.execute("browser_type",{tabId,ref,text:" extension 输入"});
    const response=await fetch(process.env.PI_DESK_BROWSER_URL!,{method:"POST",headers:{Authorization:`Bearer ${process.env.PI_DESK_BROWSER_TOKEN}`},body:JSON.stringify({threadId:"native-test",tabId,action:"call",method:"Runtime.evaluate",params:{expression:"document.querySelector('#field').value",returnByValue:true}})});
    expect((await response.json()).result.value).toContain(" extension 输入");
    const original=read.page.url;
    await h.execute("browser_navigate",{tabId,url:new URL("/second",original).href});
    await h.execute("browser_wait",{tabId,condition:"text",text:"Path: /second"});
    await expect(h.execute("browser_click",{tabId,ref})).rejects.toThrow("Stale");
    await h.execute("browser_navigate",{tabId,action:"back"});
    await h.execute("browser_wait",{tabId,condition:"text",text:"Path: / "});
    await h.execute("browser_navigate",{tabId,action:"forward"});
    await h.execute("browser_wait",{tabId,condition:"text",text:"Path: /second"});
    await h.execute("browser_navigate",{tabId,action:"reload"});
    await h.execute("browser_wait",{tabId,condition:"element",selector:"#field"});
    read=JSON.parse((await h.execute("browser_read",{tabId})).content[0].text);
    await h.execute("browser_click",{tabId,ref:read.page.elements.find((e:any)=>e.tag==="input").ref});
    await expect(h.execute("browser_screenshot",{tabId})).rejects.toThrow("tab is hidden");
    // Real native inspector connection, with the exact shipped extension API.
    const result=async(name:string,params:any)=>JSON.parse((await h.execute(name,{tabId,...params})).content[0].text);
    await result("browser_network",{action:"start"});
    await result("browser_websocket",{action:"start"});
    await result("browser_console",{action:"start"});
    const scripts=await result("browser_scripts",{action:"list",query:"probe.js"});
    const scriptId=scripts.events.find((e:any)=>e.params.url.endsWith("/probe.js"))?.params.scriptId;
    expect(scriptId).toBeTruthy();
    expect((await result("browser_scripts",{action:"read",scriptId})).text).toContain("const answer");
    const searched=await result("browser_scripts",{action:"search",query:"const answer",scriptId});
    expect(searched.results[0].matches[0].lineNumber).toBe(1);
    await result("browser_evaluate",{expression:"fetch('/api').then(r=>r.json()).then(v=>{console.log('inspect-network',v);return v})"});
    const requests=await result("browser_network",{action:"list",filter:"/api"});
    const requestId=requests.events.find((e:any)=>e.method==="Network.requestWillBeSent")?.params.requestId;
    expect(requestId).toBeTruthy();
    const body=await result("browser_network",{action:"body",requestId});
    expect(JSON.parse(body.body)).toEqual({ok:true});
    const detail=await result("browser_network",{action:"detail",requestId});
    expect(detail.events.some((e:any)=>e.params.initiator?.stack)).toBe(true);
    expect((await result("browser_console",{action:"list"})).events.some((e:any)=>JSON.stringify(e).includes("inspect-network"))).toBe(true);
    await result("browser_evaluate",{expression:"new Promise((resolve,reject)=>{const w=new WebSocket(location.origin.replace('http','ws')+'/ws');w.onopen=()=>w.send('inspect-echo');w.onmessage=e=>{resolve(e.data);w.close()};w.onerror=()=>reject(Error('socket failed'))})"});
    const messages=await result("browser_websocket",{action:"list"});
    expect(messages.events.some((e:any)=>e.method==="Network.webSocketFrameReceived" && e.params.response.payloadData==="inspect-echo")).toBe(true);
    const bp=await result("browser_debug",{action:"setBreakpoint",scriptId,text:"const answer"});
    expect(bp.breakpointId).toBeTruthy();
    await result("browser_evaluate",{expression:"setTimeout(()=>inspectProbe(41),50); 'scheduled'"});
    let state:any;
    for (let n=0;n<100;n++) {
      state=await result("browser_debug",{action:"state"});
      if (state.paused) break;
      await new Promise(r=>setTimeout(r,30));
    }
    expect(state.paused?.callFrames.length).toBeGreaterThan(0);
    const callFrameId=state.paused.callFrames[0].callFrameId;
    expect((await result("browser_evaluate",{expression:"value",callFrameId})).result.value).toBe(41);
    await result("browser_debug",{action:"stepOver"});
    await result("browser_debug",{action:"resume"});
    await result("browser_debug",{action:"removeBreakpoint",breakpointId:bp.breakpointId});
    await result("browser_debug",{action:"xhr",url:"/api"});
    expect((await result("browser_debug",{action:"state"})).xhrBreakpoints["/api"]).toBe(true);
    await result("browser_debug",{action:"removeXHR",url:"/api"});
    expect((await result("browser_frames",{})).contexts.events.length).toBeGreaterThan(0);
    await result("browser_network",{action:"clear"});
    expect((await result("browser_network",{action:"list"})).events).toHaveLength(0);
    // Stopping while paused must release the debugger so retained pages work.
    await result("browser_debug",{action:"pause"});
    await result("browser_debug",{action:"stop"});
    await h.execute("browser_wait",{tabId,condition:"element",selector:"#field"});
  },90000);
  it("transpiles and requires exact tab IDs for all page tools", async () => {
    expect(compiled.diagnostics).toEqual([]);
    const h=harness();
    for (const [name, tool] of Object.entries(h.tools)) {
      if (name !== "browser_tabs") expect(tool.parameters.args[0].tabId, name).toBeDefined();
    }
    await expect(h.execute("browser_read",{})).rejects.toThrow("tabId is required");
    await expect(h.execute("browser_tabs",{action:"close"})).rejects.toThrow("tabId is required");
    expect(h.calls).toHaveLength(0);
  });
  it("reads named controls in an isolated context and types into that exact tab", async () => {
    const h=harness();
    const read=JSON.parse((await h.execute("browser_read",{tabId:"b"})).content[0].text);
    expect(read.page.elements[0].name).toBe("Name");
    await h.execute("browser_type",{tabId:"b",ref:read.page.elements[0].ref,text:"中文"});
    expect(h.calls.find(c=>c.method==="Input.insertText")).toMatchObject({tabId:"b",params:{text:"中文"}});
    expect(h.calls.filter(c=>c.method==="Runtime.evaluate").every(c=>c.params.contextId===2)).toBe(true);
    await expect(h.execute("browser_type",{tabId:"b",ref:read.page.elements[0].ref,text:"again"})).rejects.toThrow("Stale");
  });
  it("rejects references from another tab, newer read, or removed element", async () => {
    const h=harness();
    const first=JSON.parse((await h.execute("browser_read",{tabId:"a"})).content[0].text).page.elements[0].ref;
    await expect(h.execute("browser_click",{tabId:"b",ref:first})).rejects.toThrow("Stale");
    const latest=JSON.parse((await h.execute("browser_read",{tabId:"a"})).content[0].text).page.elements[0].ref;
    await expect(h.execute("browser_click",{tabId:"a",ref:first})).rejects.toThrow("Stale");
    h.windows[0].document.querySelector("input")!.remove();
    await expect(h.execute("browser_click",{tabId:"a",ref:latest})).rejects.toThrow("Stale");
    expect(h.calls.some(c=>c.method==="Input.dispatchMouseEvent")).toBe(false);
  });
  it("rejects covered and disabled controls without input", async () => {
    const h=harness();
    const read=JSON.parse((await h.execute("browser_read",{tabId:"a"})).content[0].text);
    await expect(h.execute("browser_click",{tabId:"a",ref:read.page.elements[1].ref})).rejects.toThrow("covered");
    h.windows[0].document.querySelector("input")!.disabled=true;
    await expect(h.execute("browser_type",{tabId:"a",ref:read.page.elements[0].ref,text:"no"})).rejects.toThrow("disabled");
    expect(h.calls.some(c=>c.method.startsWith("Input."))).toBe(false);
  });
  it("rejects a navigation between reading and acting", async () => {
    const h=harness();
    const ref=JSON.parse((await h.execute("browser_read",{tabId:"a"})).content[0].text).page.elements[0].ref;
    h.windows[0].history.pushState({},"","/changed");
    await expect(h.execute("browser_click",{tabId:"a",ref})).rejects.toThrow("Stale");
    expect(h.calls.some(c=>c.method.startsWith("Input."))).toBe(false);
  });
  it("keeps coordinate screenshots scoped to the exact page and invalidates after acting", async () => {
    const h=harness();
    await h.execute("browser_screenshot",{tabId:"a"});
    await expect(h.execute("browser_click",{tabId:"b",x:20,y:20})).rejects.toThrow("screenshot");
    await h.execute("browser_click",{tabId:"a",x:20,y:20});
    expect(h.calls.at(-1)).toMatchObject({tabId:"a",method:"Input.dispatchMouseEvent",params:{x:20,y:20,type:"mouseReleased"}});
    await expect(h.execute("browser_click",{tabId:"a",x:20,y:20})).rejects.toThrow("screenshot");
    await h.execute("browser_screenshot",{tabId:"a"});
    h.windows[0].history.pushState({},"","/new");
    await expect(h.execute("browser_click",{tabId:"a",x:20,y:20})).rejects.toThrow("Page changed");
    await h.execute("browser_screenshot",{tabId:"a",fullPage:true});
    await expect(h.execute("browser_click",{tabId:"a",x:20,y:20})).rejects.toThrow("screenshot");
  });
  it("creates temporary tabs and navigates history on the supplied tab", async () => {
    const h=harness();
    await h.execute("browser_tabs",{action:"create",url:"https://example.test"});
    expect(h.calls[0]).toMatchObject({action:"create",params:{url:"https://example.test"}});
    await h.execute("browser_navigate",{tabId:"b",action:"back"});
    expect(h.calls.at(-1)).toMatchObject({tabId:"b",method:"Page.navigateToHistoryEntry",params:{entryId:10}});
    await h.execute("browser_navigate",{tabId:"a",action:"forward"});
    expect(h.calls.at(-1)).toMatchObject({tabId:"a",params:{entryId:30}});
    await h.execute("browser_navigate",{tabId:"a",action:"reload"});
    expect(h.calls.at(-1).method).toBe("Page.reload");
    await expect(h.execute("browser_navigate",{tabId:"a",url:"file:///secret"})).rejects.toThrow("Only http");
  });
  it("waits for visible elements, times out, and accepts cancellation", async () => {
    const h=harness();
    const result=await h.execute("browser_wait",{tabId:"a",condition:"element",selector:"#name"});
    expect(JSON.parse(result.content[0].text).matched).toBe(true);
    await expect(h.execute("browser_wait",{tabId:"a",condition:"element",selector:"#missing",timeoutMs:20})).rejects.toThrow("Timed out");
    const controller=new AbortController();controller.abort();
    await expect(h.execute("browser_wait",{tabId:"a",condition:"ready"},controller.signal)).rejects.toThrow("cancelled");
    await expect(h.execute("browser_wait",{tabId:"a",condition:"text"})).rejects.toThrow("Supply text");
  });
  it("honors expired connections and cleans temporary tabs at agent end", async () => {
    const h=harness();h.expire();
    await expect(h.execute("browser_key",{tabId:"a",key:"Enter"})).rejects.toThrow("expired");
    expect(h.calls.some(c=>c.method.startsWith("Input."))).toBe(false);
    await h.events.agent_end();
    expect(h.calls.at(-1).action).toBe("finish");
  });
});
