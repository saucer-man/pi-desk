import { randomUUID } from "node:crypto";
import type { Runtime } from "./runtime.ts";
import type { ScheduledTask } from "./types.ts";
import { safeError } from "./storage.ts";

export function nextRun(task: Pick<ScheduledTask,"frequency"|"runAt"|"time"|"weekday">, after = Date.now()): string | undefined {
  if (task.frequency === "once") return task.runAt && Date.parse(task.runAt) > after ? new Date(task.runAt).toISOString() : undefined;
  if (!["daily","weekly"].includes(task.frequency) || !/^([01]\d|2[0-3]):[0-5]\d$/.test(task.time || "")) throw new Error("定时任务时间无效");
  if (task.frequency === "weekly" && (!Number.isInteger(task.weekday) || task.weekday! < 0 || task.weekday! > 6)) throw new Error("星期无效");
  const [hour, minute] = task.time!.split(":").map(Number); const date = new Date(after); date.setHours(hour, minute, 0, 0);
  for (let i=0;i<8;i++,date.setDate(date.getDate()+1)) if (+date > after && (task.frequency === "daily" || date.getDay() === task.weekday)) return date.toISOString();
  throw new Error("无法计算下次运行时间");
}

export class Scheduler {
  private timer: ReturnType<typeof setInterval>;
  private pending = new Map<string, Promise<void>>();
  private closing = false;
  private runtime:Runtime;
  constructor(runtime: Runtime) {
    this.runtime=runtime;
    const tasks = runtime.store.state.schedules ??= [];
    for (const task of tasks) {
      if (task.lastStatus === "running") { task.lastStatus="paused";task.lastError="服务重启，执行已中断，未自动重放"; }
      if (task.enabled && (!task.nextRunAt || Date.parse(task.nextRunAt) <= Date.now())) {
        task.nextRunAt=nextRun(task); if (!task.nextRunAt) task.enabled=false;
        task.lastStatus="skipped";task.lastError="停机期间的计划未补跑，请按需手动运行";
      }
    }
    runtime.store.save();this.timer=setInterval(()=>this.tick(),1000);this.timer.unref();
  }
  async save(data: Record<string,unknown>, id?: string) {
    const previous=id?this.find(id):undefined;if (previous && this.pending.has(previous.id)) throw new Error("任务运行中，停止后再编辑");
    const value={...previous,...data} as unknown as ScheduledTask;
    for (const [key,max] of [["name",200],["prompt",1024*1024],["workspaceId",100],["model",500]] as const) if (typeof value[key]!=="string" || !value[key].trim() || value[key].length>max) throw new Error("任务名称、内容、工作区和模型不能为空或过长");
    if (!["standard","minimal"].includes(value.mode) || typeof value.enabled!=="boolean" || !["once","daily","weekly"].includes(value.frequency)) throw new Error("任务模式或频率无效");
    if (value.thinking!==undefined && !["off","minimal","low","medium","high","xhigh","max"].includes(value.thinking)) throw new Error("推理等级无效");
    const workspace=this.runtime.store.state.workspaces.find(w=>w.id===value.workspaceId&&!w.hidden);if(!workspace)throw new Error("工作区不存在或已移除");
    const catalog=await this.runtime.models();
    if (!catalog.models.some((m:{provider:string;id:string})=>`${m.provider}/${m.id}`===value.model)) throw new Error("请选择已认证的可用模型，不会自动替换模型");
    if (value.frequency==="once" && (!value.runAt || !Number.isFinite(Date.parse(value.runAt)))) throw new Error("单次任务需要有效日期时间");
    const next=nextRun(value);if(value.enabled&&!next)throw new Error("单次运行时间必须晚于当前时间");
    const now=new Date().toISOString();
    const task:ScheduledTask={id:previous?.id||randomUUID(),name:value.name.trim(),prompt:value.prompt,workspaceId:value.workspaceId,model:value.model,thinking:value.thinking,mode:value.mode,frequency:value.frequency,time:value.time,weekday:value.weekday,runAt:value.runAt,enabled:value.enabled,nextRunAt:value.enabled?next:undefined,createdAt:previous?.createdAt||now,updatedAt:now,lastRunAt:previous?.lastRunAt,lastSessionId:previous?.lastSessionId,lastStatus:previous?.lastStatus,lastError:previous?.lastError,lastSlot:previous?.lastSlot};
    if (previous) Object.assign(previous,task);else this.runtime.store.state.schedules!.push(task);this.runtime.changed();return task;
  }
  find(id:string) { const task=this.runtime.store.state.schedules!.find(t=>t.id===id);if(!task)throw new Error("定时任务不存在");return task; }
  remove(id:string) { this.find(id);if(this.pending.has(id))throw new Error("任务运行中，停止后再删除");this.runtime.store.state.schedules=this.runtime.store.state.schedules!.filter(t=>t.id!==id);this.runtime.changed(); }
  tick(now=Date.now()) { if(this.closing)return;for(const task of this.runtime.store.state.schedules!)if(task.enabled&&task.nextRunAt&&Date.parse(task.nextRunAt)<=now&&!this.pending.has(task.id))void this.run(task.id,false,now).catch(error=>this.runtime.emit({type:"notice",data:{message:safeError(error)}})); }
  run(id:string,manual=true,now=Date.now()) {
    const task=this.find(id);if(this.closing||this.pending.has(id))throw new Error("任务已在运行或服务正在退出");
    const job=this.execute(task,manual,now);this.pending.set(id,job);void job.then(()=>this.pending.delete(id),()=>this.pending.delete(id));return job;
  }
  private async execute(task:ScheduledTask,manual:boolean,now:number) {
    const slot=manual?new Date(now).toISOString():task.nextRunAt!;if(!manual&&task.lastSlot===slot)return;
    task.lastSlot=slot;task.lastRunAt=new Date(now).toISOString();task.lastStatus="running";task.lastError=undefined;task.lastSessionId=undefined;
    if(!manual){task.nextRunAt=nextRun(task,now);if(!task.nextRunAt)task.enabled=false;}
    // Persist the claimed slot before any SDK work, so a restart cannot replay side effects.
    this.runtime.changed();
    try {
      const workspace=this.runtime.store.state.workspaces.find(w=>w.id===task.workspaceId);if(!workspace||workspace.hidden)throw new Error("工作区不存在或已移除");
      const extra={title:task.name,model:task.model,scheduledTaskId:task.id};
      if(workspace.remote)await this.runtime.remote.connect(workspace);
      const record=this.runtime.create(task.workspaceId,task.mode,extra);task.lastSessionId=record.id;this.runtime.changed();
      await this.runtime.change(record.id,{model:task.model,thinking:task.thinking||"off"});await this.runtime.prompt(record.id,task.prompt);
      const snapshot=await this.runtime.snapshot(record.id);
      const error=snapshot.messages.at(-1)?.errorMessage;if(error)throw new Error(error);
      task.lastStatus=snapshot.record.status==="paused"?"paused":snapshot.record.status==="error"?"error":"idle";task.lastError=snapshot.record.error;
    } catch(error) {const record=this.runtime.store.state.sessions.find(r=>r.id===task.lastSessionId);task.lastStatus=this.closing||record?.status==="paused"?"paused":"error";task.lastError=safeError(error);if(record&&record.status!=="paused"){record.status=task.lastStatus==="paused"?"paused":"error";record.error=task.lastError;record.unread=true;}}
    finally {task.updatedAt=new Date().toISOString();this.runtime.changed();}
  }
  close(){this.closing=true;clearInterval(this.timer);}
  async drain(){await Promise.allSettled(this.pending.values());}
}
