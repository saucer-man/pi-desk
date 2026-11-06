import type { Server, IncomingMessage } from "node:http";
import {WebSocketServer,WebSocket} from "ws";
import type {IPty} from "node-pty";
import type {Runtime} from "./runtime.ts";
import {workspace} from "./api.ts";
import {safeError} from "./storage.ts";
import {createRequire} from "node:module";
import {dirname,join} from "node:path";
import {chmod,stat} from "node:fs/promises";
export function attachTerminals(server:Server,runtime:Runtime,allowed:(req:IncomingMessage)=>boolean){
const terminals = new Map<string, { pty: IPty; buffer: string; clients: Set<WebSocket> }>();
const starting = new Map<string, Promise<void>>();
const sockets = new WebSocketServer({ noServer: true, maxPayload: 1024 * 1024 });
server.on("upgrade", (req, socket, head) => { if (!allowed(req) || !req.url?.startsWith("/api/terminal/")) { socket.destroy(); return; } sockets.handleUpgrade(req, socket, head, ws => sockets.emit("connection", ws, req)); });
sockets.on("connection", async (ws, req) => {
  // Hold early keystrokes until local or SSH startup has installed its handlers.
  ws.pause();
  const id = new URL(req.url!, "http://127.0.0.1").pathname.split("/")[3];
  try {
    const w = workspace(runtime, id);
    if(w.remote){await runtime.remote.terminal(w,ws);return;}
    if (!terminals.has(id)) {
      let pending = starting.get(id);
      if (!pending) {
        pending = (async () => {
          // node-pty 1.1.0 ships macOS spawn-helper without its executable bit (upstream #850).
          if(process.platform==="darwin"){
            const root=dirname(createRequire(import.meta.url).resolve("node-pty/package.json"));
            for(const dir of ["build/Release","build/Debug",`prebuilds/darwin-${process.arch}`])try{const helper=join(root,dir,"spawn-helper"),info=await stat(helper);if(!(info.mode&0o100))await chmod(helper,(info.mode&0o777)|0o100);}catch(error){if((error as NodeJS.ErrnoException).code!=="ENOENT")throw error;}
          }
          const { spawn: spawnPty } = await import("node-pty");
          const shell = process.platform === "win32" ? "powershell.exe" : process.env.SHELL || "/bin/bash";
          const pty = spawnPty(shell, process.platform === "win32" ? ["-NoLogo"] : [], { name: "xterm-256color", cols: 100, rows: 24, cwd: w.path, env: process.env as Record<string,string> });
          const terminal = { pty, buffer: "", clients: new Set<WebSocket>() }; terminals.set(id, terminal);
          pty.onData(data => { terminal.buffer = (terminal.buffer + data).slice(-200000); for (const client of terminal.clients) if (client.readyState === WebSocket.OPEN && client.bufferedAmount < 2 * 1024 * 1024) client.send(JSON.stringify({ type: "data", data })); });
          pty.onExit(({ exitCode }) => { for (const client of terminal.clients) { if (client.readyState === WebSocket.OPEN) client.send(JSON.stringify({ type: "exit", code: exitCode })); client.close(); } terminals.delete(id); });
        })(); starting.set(id, pending);
      }
      try { await pending; } finally { starting.delete(id); }
    }
    const terminal = terminals.get(id)!; if (ws.readyState !== WebSocket.OPEN) return;
    terminal.clients.add(ws); ws.send(JSON.stringify({ type: "data", data: terminal.buffer }));
    ws.on("message", message => { try { const data = JSON.parse(message.toString()); if (data.type === "input" && typeof data.data === "string") terminal.pty.write(data.data); if (data.type === "resize" && Number.isInteger(data.cols) && Number.isInteger(data.rows)) terminal.pty.resize(Math.max(2, Math.min(500, data.cols)), Math.max(2, Math.min(300, data.rows))); if (data.type === "close") terminal.pty.kill(); } catch { ws.close(1008); } });
    ws.on("close", () => terminal.clients.delete(ws));
  } catch (error) { ws.send(JSON.stringify({ type: "error", message: safeError(error) })); ws.close(); }
  finally { ws.resume(); }
});

return ()=>{for(const terminal of terminals.values())terminal.pty.kill();for(const client of sockets.clients)client.terminate();sockets.close();};
}
