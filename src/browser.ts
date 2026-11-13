import { existsSync } from "node:fs";
import { join } from "node:path";
import { randomUUID } from "node:crypto";
import { spawn } from "node:child_process";
import { Type } from "typebox";
import { defineTool } from "@earendil-works/pi-coding-agent";
import type { BrowserContext, Page } from "playwright-core";
import type { Runtime } from "./runtime.ts";
import type { SessionRecord } from "./types.ts";
import { deskDir } from "./storage.ts";

export async function openExternal(value:string) {
  const url=new URL(value);if(!["http:","https:"].includes(url.protocol)||url.username||url.password||/[\0-\x1f\x7f]/.test(value))throw new Error("仅支持不含认证信息的 HTTP / HTTPS 网页");
  const args=process.platform==="win32"?["rundll32.exe","url.dll,FileProtocolHandler",url.href]:process.platform==="darwin"?["open",url.href]:["xdg-open",url.href];
  const env={...process.env};for(const key of Object.keys(env))if(["ELECTRON_RUN_AS_NODE","NODE_OPTIONS","VSCODE_IPC_HOOK_CLI"].includes(key.toUpperCase()))delete env[key];
  await new Promise<void>((resolve,reject)=>{const child=spawn(args[0],args.slice(1),{env,windowsHide:false,detached:true,stdio:"ignore"});const finish=(error?:Error)=>{clearTimeout(timer);child.unref();error?reject(error):resolve();};const timer=setTimeout(()=>finish(),1000);child.once("error",()=>finish(new Error("无法启动系统默认浏览器")));child.once("exit",code=>finish(code!==0?new Error("系统默认浏览器启动失败"):undefined));});
}

function executable() {
  return [process.env.PI_DESK_BROWSER_EXECUTABLE,process.platform==="win32"?join(process.env.PROGRAMFILES||"C:/Program Files","Google/Chrome/Application/chrome.exe"):"",process.platform==="win32"?join(process.env["PROGRAMFILES(X86)"]||"C:/Program Files (x86)","Microsoft/Edge/Application/msedge.exe"):"",process.platform==="darwin"?"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome":"",...(["/usr/bin/google-chrome","/usr/bin/chromium","/usr/bin/chromium-browser"])].find(p=>p&&existsSync(p));
}
export interface BrowserCommand {action:string;id?:string;url?:string;selector?:string;role?:string;name?:string;text?:string;key?:string;x?:number;y?:number;width?:number;height?:number}
export class BrowserHost {
  private context?:BrowserContext;
  private starting?:Promise<BrowserContext>;
  private pages=new Map<string,{page:Page;owner?:string;requestedUrl?:string;openedUrl?:string}>();
  private track(page:Page,owner?:string){let entry=[...this.pages].find(([,p])=>p.page===page);if(entry){if(owner)entry[1].owner=owner;return entry[0];}const id=randomUUID();this.pages.set(id,{page,owner});page.on("close",()=>this.pages.delete(id));page.on("popup",popup=>this.track(popup,this.pages.get(id)?.owner));page.on("dialog",dialog=>void dialog.dismiss().catch(()=>{}));return id;}
  private async start(){
    if(this.context)return this.context;if(this.starting)return this.starting;
    this.starting=(async()=>{const path=executable();if(!path)throw new Error("未找到 Chrome / Edge / Chromium，请先安装浏览器");const {chromium}=await import("playwright-core");const context=await chromium.launchPersistentContext(join(deskDir,"browser"),{executablePath:path,headless:process.env.PI_DESK_BROWSER_HEADLESS!=="0",viewport:{width:1100,height:720},acceptDownloads:false});this.context=context;context.on("close",()=>{this.context=undefined;this.pages.clear();});context.on("page",page=>this.track(page));for(const page of context.pages())this.track(page);return context;})();
    try{return await this.starting;}finally{this.starting=undefined;}
  }
  private url(value?:string){if(!value)return "about:blank";const url=new URL(value);if(!["http:","https:"].includes(url.protocol)||url.username||url.password)throw new Error("浏览器仅支持不含认证信息的 HTTP / HTTPS 地址");return url.href;}
  private page(id?:string,owner?:string){const entry=id?this.pages.get(id):[...this.pages.values()].find(p=>!owner||p.owner===owner||!p.owner);if(!entry||owner&&entry.owner&&entry.owner!==owner)throw new Error("浏览器标签不存在或不属于此会话");if(owner)entry.owner=owner;return entry.page;}
  async status(owner?:string){return {available:Boolean(executable()),running:Boolean(this.context),tabs:await Promise.all([...this.pages].filter(([,p])=>!owner||!p.owner||p.owner===owner).map(async([id,p])=>({id,url:p.page.url(),requestedUrl:p.page.url()===p.openedUrl?p.requestedUrl:undefined,title:await p.page.title().catch(()=>""),owned:Boolean(p.owner)})))};}
  async command(input:BrowserCommand,owner?:string){
    if(input.action==="tabs")return this.status(owner);
    if(input.action==="stop"){if(owner)throw new Error("整个浏览器只能由用户关闭");await this.close();return {closed:true};}
    if(input.action==="open"){const url=this.url(input.url);const page=await(await this.start()).newPage();const id=this.track(page,owner);if(url!=="about:blank")await page.goto(url,{waitUntil:"domcontentloaded",timeout:20000});Object.assign(this.pages.get(id)!,{requestedUrl:url,openedUrl:page.url()});return {id,url:page.url()};}
    const page=this.page(input.id,owner);const url=page.url();if(url!=="about:blank")this.url(url);
    if(["view","pointer","keyboard","scroll"].includes(input.action)){
      if(owner)throw new Error("此操作仅供浏览器面板使用");
      if(input.action==="view"){
        const {width,height}=input;if(typeof width!=="number"||typeof height!=="number"||!Number.isInteger(width)||!Number.isInteger(height)||width<240||width>2560||height<160||height>1600)throw new Error("浏览器视口尺寸无效");
        const size={width,height};if(page.viewportSize()?.width!==width||page.viewportSize()?.height!==height)await page.setViewportSize(size);
        return {id:input.id,url:page.url(),title:await page.title(),...size,image:`data:image/jpeg;base64,${(await page.screenshot({type:"jpeg",quality:75,timeout:15000})).toString("base64")}`};
      }
      if(input.action==="keyboard"){
        if(input.text!==undefined){if(typeof input.text!=="string"||input.text.length>100000)throw new Error("输入文本无效或过长");await page.keyboard.insertText(input.text);}
        else {if(!input.key||input.key.length>80)throw new Error("按键无效");await page.keyboard.press(input.key);}
      }else{
        if(!Number.isFinite(input.x)||!Number.isFinite(input.y)||Math.abs(input.x!)>10000||Math.abs(input.y!)>10000)throw new Error("浏览器坐标无效");
        if(input.action==="pointer")await page.mouse.click(input.x!,input.y!);else await page.mouse.wheel(input.x!,input.y!);
      }
      return {url:page.url()};
    }
    const locator=()=>{if(input.selector&&input.selector.length<2000)return page.locator(input.selector);if(input.role&&input.name&&input.name.length<2000)return page.getByRole(input.role as Parameters<Page["getByRole"]>[0],{name:input.name,exact:true});throw new Error("请提供元素角色和名称或 CSS 选择器");};
    if(input.action==="navigate"){const target=this.url(input.url);await page.goto(target,{waitUntil:"domcontentloaded",timeout:20000});Object.assign([...this.pages.values()].find(entry=>entry.page===page)!,{requestedUrl:target,openedUrl:page.url()});}
    else if(input.action==="back")await page.goBack({waitUntil:"domcontentloaded",timeout:20000});
    else if(input.action==="forward")await page.goForward({waitUntil:"domcontentloaded",timeout:20000});
    else if(input.action==="reload")await page.reload({waitUntil:"domcontentloaded",timeout:20000});
    else if(input.action==="read")return {url,title:await page.title(),content:(await page.locator("body").ariaSnapshot({timeout:10000})).slice(0,60000)};
    else if(input.action==="screenshot")return {url,image:`data:image/jpeg;base64,${(await page.screenshot({type:"jpeg",quality:65,timeout:15000})).toString("base64")}`};
    else if(input.action==="click")await locator().click({timeout:10000});
    else if(input.action==="fill"){if(typeof input.text!=="string"||input.text.length>100000)throw new Error("输入文本无效或过长");await locator().fill(input.text,{timeout:10000});}
    else if(input.action==="press"){if(!input.key||input.key.length>80)throw new Error("按键无效");await locator().press(input.key,{timeout:10000});}
    else if(input.action==="close"){await page.close();return {closed:true};}
    else throw new Error("未知浏览器操作");
    return {url:page.url(),title:await page.title().catch(()=>"")};
  }
  async close(){const context=this.context||await this.starting?.catch(()=>undefined);await context?.close();this.pages.clear();}
}
export function browserTool(runtime:Runtime,record:SessionRecord){return defineTool({name:"desk_browser",label:"独立浏览器",executionMode:"sequential",description:"Control Pi Desk's browser shown in the right sidebar. Browser access is allowed by default while the plugin is enabled. Actions: tabs, open, navigate, back, forward, reload, read (ARIA snapshot), screenshot, click, fill, press, close. Use a managed tab id and semantic role/name or CSS selector. Tabs owned by other agent sessions are inaccessible. Computer desktop control is supplied only through configured MCP tools.",parameters:Type.Object({action:Type.String(),id:Type.Optional(Type.String()),url:Type.Optional(Type.String()),selector:Type.Optional(Type.String()),role:Type.Optional(Type.String()),name:Type.Optional(Type.String()),text:Type.Optional(Type.String()),key:Type.Optional(Type.String())}),execute:async(_id,args)=>{const result=await runtime.browser.command(args,record.id);if(args.action!=="close")runtime.emit({type:"browser",sessionId:record.id,data:{id:"id" in result?result.id:args.id}});return {content:"image" in result?[{type:"image" as const,data:String(result.image).split(",")[1],mimeType:"image/jpeg"}]:[{type:"text" as const,text:JSON.stringify(result)}],details:result};}});}
