import {execFile} from "node:child_process";
import {promisify} from "node:util";
import {createHash} from "node:crypto";
import {mkdir,readFile,readdir,writeFile} from "node:fs/promises";
import {join} from "node:path";

const exec=promisify(execFile),folder="build/remote-helper/artifacts";
const sources=["go.mod","go.sum",...(await Promise.all(["internal/remotehelper","internal/remoteprotocol","cmd/pi-desk-remote-helper"].map(async dir=>(await readdir(dir)).filter(name=>name.endsWith(".go")).map(name=>join(dir,name))))).flat()].sort();
const hash=createHash("sha256");for(const path of sources){hash.update(path.replaceAll("\\","/"));hash.update(await readFile(path));}const buildHash=hash.digest("hex");
await mkdir(folder,{recursive:true});
const previous=await readFile(join(folder,"manifest.json"),"utf8").then(JSON.parse,()=>undefined);
const artifacts=[];
for(const os of ["linux","darwin"])for(const architecture of ["amd64","arm64"]){
  const name=`helper-${os}-${architecture}`,path=join(folder,name),old=previous?.artifacts?.find((a:any)=>a.name===name);
  const cached=await readFile(path).catch(()=>undefined);
  if(!cached||old?.buildHash!==buildHash||old.sha256!==createHash("sha256").update(cached).digest("hex")){
    console.log(`Building SSH helper ${os}/${architecture}`);
    await exec("go",["build","-trimpath","-buildvcs=false",`-ldflags=-s -w -X main.buildHash=${buildHash}`,"-o",path,"./cmd/pi-desk-remote-helper"],{env:{...process.env,GOOS:os,GOARCH:architecture,CGO_ENABLED:"0"},maxBuffer:4*1024*1024});
  }
  const data=await readFile(path);artifacts.push({name,os,architecture,protocol:1,buildHash,size:data.length,sha256:createHash("sha256").update(data).digest("hex")});
}
await writeFile(join(folder,"manifest.json"),JSON.stringify({version:1,artifacts},null,2)+"\n");
console.log("SSH helper artifacts verified (Linux/macOS, x64/ARM64)");
