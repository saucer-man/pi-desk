/// <reference types="highlight.js" />
import { useMemo } from "react";
import hljs from "highlight.js/lib/core";
import javascript from "highlight.js/lib/languages/javascript";
import typescript from "highlight.js/lib/languages/typescript";
import json from "highlight.js/lib/languages/json";
import xml from "highlight.js/lib/languages/xml";
import cssLanguage from "highlight.js/lib/languages/css";
import python from "highlight.js/lib/languages/python";
import bash from "highlight.js/lib/languages/bash";
import diffLanguage from "highlight.js/lib/languages/diff";
import yaml from "highlight.js/lib/languages/yaml";
import sql from "highlight.js/lib/languages/sql";
import go from "highlight.js/lib/languages/go";
import rust from "highlight.js/lib/languages/rust";
import ini from "highlight.js/lib/languages/ini";
import css from "./App.module.css";

for (const [name, grammar] of Object.entries({javascript,typescript,json,xml,css:cssLanguage,python,bash,diff:diffLanguage,yaml,sql,go,rust,ini})) hljs.registerLanguage(name,grammar);
const languages: Record<string,string> = {js:"javascript",jsx:"javascript",ts:"typescript",tsx:"typescript",html:"xml",vue:"xml",svg:"xml",py:"python",sh:"bash",yml:"yaml",rs:"rust",toml:"ini"};

export default function Code({text,path="",diff=false}:{text:string;path?:string;diff?:boolean}) {
  const source=text.replace(/\r\n?/g,"\n");
  const {lines,numbers,large}=useMemo(()=>{
    // ponytail: large files use native plain text; add virtualization if numbered large-file views are needed.
    const lines=source.length>200_000?[]:source.replace(/\n$/,"").split("\n");
    const large=source.length>200_000||lines.length>10_000;
    let before=0,after=0,inHunk=false;
    const numbers=large?[]:lines.map((line,index)=>{
      if(!diff)return String(index+1);
      const hunk=/^@@ -(\d+)(?:,\d+)? \+(\d+)/.exec(line);
      if(hunk){before=Number(hunk[1]);after=Number(hunk[2]);inHunk=true;return "";}
      if(line.startsWith("diff "))inHunk=false;
      if(!inHunk)return "";
      if(line.startsWith("-"))return String(before++);
      if(line.startsWith("+"))return String(after++);
      if(line.startsWith(" ")){before++;return String(after++);}
      return "";
    });
    return {lines,numbers,large};
  },[source,diff]);
  const highlighted = useMemo(()=>{
    const extension=path.split(".").at(-1)?.toLowerCase() || ""; const language=diff?"diff":languages[extension]||extension;
    if(large || !hljs.getLanguage(language)) return;
    try {
      const open:string[]=[];
      // Keep multiline tokens balanced when each source line is rendered separately.
      return hljs.highlight(source,{language,ignoreIllegals:true}).value.replace(/<span\b[^>]*>|<\/span>|\n/g,tag=>{
        if(tag==="\n") return "</span>".repeat(open.length)+"\n"+open.join("");
        if(tag==="</span>")open.pop();else open.push(tag);return tag;
      }).split("\n");
    } catch { return; }
  },[source,path,diff,large]);
  if(large)return <pre className={`${css["code-preview"]} ${css["plain-code"]}`} aria-label={diff?"差异内容":"文件内容"} title="大文件使用纯文本预览，不显示行号与语法高亮"><code>{source}</code></pre>;
  return <pre className={css["code-preview"]} aria-label={diff?"差异内容":"文件内容"}><code>{lines.map((line,index)=><span className={`${css["code-line"]} ${diff?(line.startsWith("+")?css["line-add"]:line.startsWith("-")?css["line-remove"]:line.startsWith("@@")?css["line-hunk"]:""):""}`} data-line={numbers[index]} key={index}>{highlighted?<span className={css["code-text"]} dangerouslySetInnerHTML={{__html:highlighted[index] || " "}}/>:<span className={css["code-text"]}>{line || " "}</span>}</span>)}</code></pre>;
}
