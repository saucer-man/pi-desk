import { useEffect, useId, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { createEditor, DecoratorNode, $getRoot, $getNodeByKey, $createParagraphNode, $createTextNode, $createLineBreakNode, $getSelection, $isRangeSelection, $isTextNode, KEY_ENTER_COMMAND, KEY_DOWN_COMMAND, CLEAR_HISTORY_COMMAND, COMMAND_PRIORITY_HIGH, type LexicalEditor, type NodeKey, type SerializedLexicalNode } from "lexical";
import { registerPlainText } from "@lexical/plain-text";
import { registerHistory, createEmptyHistoryState } from "@lexical/history";
import { observeComposition } from "./dsh/keyboard-composition";
import { FileCode2, FolderOpen, LoaderCircle, X } from "./icons";
import { api } from "./store";
import css from "./ui.module.css";

// References stay ordinary Markdown in saved drafts, queues and model input.
class FileReference extends DecoratorNode<ReactNode> {
  __path: string;
  constructor(path="",key?:NodeKey){super(key);this.__path=path;}
  static getType(){return "file-reference";}
  static clone(node:FileReference){return new FileReference(node.__path,node.__key);}
  static importJSON(node:SerializedLexicalNode & Record<string,unknown>){if(typeof node.path!=="string")throw Error("无效的文件引用");return new FileReference(node.path);}
  exportJSON(){return {...super.exportJSON(),path:this.__path};}
  getTextContent(){return `[@${this.__path.split("/").at(-1)!.replace(/[\\[\]]/g,"\\$&")}](${this.__path.split("/").map(part=>encodeURIComponent(part).replace(/[()]/g,c=>"%"+c.charCodeAt(0).toString(16))).join("/")})`;}
  isInline(){return true;}
  createDOM(){const element=document.createElement("span");element.className=css.fileReference;element.contentEditable="false";return element;}
  updateDOM(){return false;}
  decorate(editor:LexicalEditor){return <><button type="button" className={css.fileReferenceName} title={this.__path} aria-label={`预览文件：${this.__path}`} onClick={()=>window.dispatchEvent(new CustomEvent("pi-request-files",{detail:{path:this.__path}}))}><FileCode2 size={14}/><span>{this.__path.split("/").at(-1)}</span></button><button type="button" className={css.fileReferenceRemove} aria-label={`移除文件引用：${this.__path}`} disabled={!editor.isEditable()} onClick={()=>{if(editor.isEditable())editor.update(()=>{$getNodeByKey(this.getKey())?.remove();});}}><X size={12}/></button></>;}
}

function $draftParagraph(text:string){
  const paragraph=$createParagraphNode();
  text.split("\n").forEach((line,index)=>{
    if(index)paragraph.append($createLineBreakNode());
    let offset=0;
    for(const match of line.matchAll(/\[@(?:\\.|[^\]\\\n])+\]\(([^()\s]+)\)/g)){
      let path:string;try{path=decodeURIComponent(match[1]);}catch{continue;}
      if(/^[a-z][\w+.-]*:/i.test(path))continue;
      if(match.index!>offset)paragraph.append($createTextNode(line.slice(offset,match.index)));
      paragraph.append(new FileReference(path));offset=match.index!+match[0].length;
    }
    if(offset<line.length)paragraph.append($createTextNode(line.slice(offset)));
  });
  return paragraph;
}

type Mention={key:string;start:number;end:number;query:string};
export function Editor({value,onChange,onSend,onFiles,workspaceId,fileRequest=0,disabled=false,placeholder}:{value:string;onChange:(text:string)=>void;onSend:()=>void;onFiles:(files:File[])=>void;workspaceId?:string;fileRequest?:number;disabled?:boolean;placeholder:string}) {
  const root=useRef<HTMLDivElement>(null),list=useRef<HTMLDivElement>(null);const change=useRef(onChange);change.current=onChange;const send=useRef(onSend);send.current=onSend;
  const [decorators,setDecorators]=useState<Record<string,ReactNode>>({}),[mention,setMention]=useState<Mention>(),[matches,setMatches]=useState<string[]>([]),[selected,setSelected]=useState(0),[loading,setLoading]=useState(false),[error,setError]=useState("");
  const id=useId(),dismissed=useRef(""),requested=useRef(fileRequest);
  const editor=useRef<ReturnType<typeof createEditor>>();if(!editor.current)editor.current=createEditor({namespace:"pi-desk-dsh",nodes:[FileReference],onError:error=>{throw error;}});
  const open=Boolean(mention&&workspaceId&&!disabled),signature=mention?`${mention.key}:${mention.start}:${mention.end}:${mention.query}`:"";
  const close=()=>{dismissed.current=signature;setMention(undefined);};
  const choose=(path:string)=>{
    if(!mention||disabled)return;
    editor.current!.update(()=>{
      const node=$getNodeByKey(mention.key);if(!$isTextNode(node)||node.getTextContent().slice(mention.start,mention.end)!=="@"+mention.query)return;
      const selection=node.select(mention.start,mention.end);selection.insertNodes([new FileReference(path),$createTextNode(" ")]);
    });
    setMention(undefined);
  };
  const picker=useRef({open,choose,close,matches,selected,loading,error});picker.current={open,choose,close,matches,selected,loading,error};
  useEffect(()=>{
    const e=editor.current!;e.setRootElement(root.current);const composition=observeComposition(document),plain=registerPlainText(e),history=registerHistory(e,createEmptyHistoryState(),300),decorated=e.registerDecoratorListener(setDecorators);
    const update=e.registerUpdateListener(({editorState,tags})=>{
      editorState.read(()=>{
        if(!tags.has("external"))change.current($getRoot().getTextContent());
        const selection=$getSelection(),node=$isRangeSelection(selection)&&selection.isCollapsed()?selection.anchor.getNode():undefined;
        const offset=$isRangeSelection(selection)?selection.anchor.offset:0,match=$isTextNode(node)&&!node.isToken()&&document.activeElement===root.current?node.getTextContent().slice(0,offset).match(/(?:^|\s)@([^@\n]{0,200})$/):null;
        const next=match?{key:node!.getKey(),start:offset-match[1].length-1,end:offset,query:match[1]}:undefined;
        const key=next?`${next.key}:${next.start}:${next.end}:${next.query}`:"";
        if(key!==dismissed.current){dismissed.current="";setMention(current=>current?.key===next?.key&&current?.start===next?.start&&current?.end===next?.end&&current?.query===next?.query?current:next);}
      });
    });
    const enter=e.registerCommand(KEY_ENTER_COMMAND,event=>{
      if(!event||composition.guards(event)||e.isComposing())return false;
      event.preventDefault();const p=picker.current;
      if(p.open&&!event.shiftKey){if(!p.loading&&!p.error&&p.matches[p.selected])p.choose(p.matches[p.selected]);return true;}
      if(event.shiftKey||(localStorage.getItem("pi-desk.enter")==="ctrl-enter"&&!event.ctrlKey&&!event.metaKey)){e.update(()=>{const selection=$getSelection();if($isRangeSelection(selection))selection.insertLineBreak();});}else send.current();return true;
    },COMMAND_PRIORITY_HIGH);
    const keys=e.registerCommand(KEY_DOWN_COMMAND,event=>{
      const p=picker.current;if(!p.open||event.isComposing||e.isComposing())return false;
      if(event.key==="Escape"){event.preventDefault();p.close();return true;}
      if(event.key==="Tab"){if(!event.shiftKey&&!p.loading&&!p.error&&p.matches[p.selected]){event.preventDefault();p.choose(p.matches[p.selected]);return true;}p.close();return false;}
      if(!["ArrowDown","ArrowUp"].includes(event.key))return false;
      event.preventDefault();setSelected(index=>p.matches.length?(index+(event.key==="ArrowDown"?1:-1)+p.matches.length)%p.matches.length:0);return true;
    },COMMAND_PRIORITY_HIGH);
    return()=>{plain();history();decorated();update();enter();keys();composition.dispose();e.setRootElement(null);};
  },[]);
  useEffect(()=>{const e=editor.current!;e.setEditable(!disabled);setDecorators({...e.getDecorators()});},[disabled]);
  useEffect(()=>{const e=editor.current!;if(e.getEditorState().read(()=>$getRoot().getTextContent())===value)return;e.update(()=>{$getRoot().clear().append($draftParagraph(value));},{tag:"external",onUpdate:()=>e.dispatchCommand(CLEAR_HISTORY_COMMAND,undefined)});setMention(undefined);},[value]);
  useEffect(()=>{
    if(requested.current===fileRequest)return;requested.current=fileRequest;
    if(!fileRequest||!workspaceId||disabled)return;
    const frame=requestAnimationFrame(()=>editor.current!.focus(()=>editor.current!.update(()=>{const r=$getRoot();r.selectEnd().insertText((r.getTextContent()&&!/\s$/.test(r.getTextContent())?" ":"")+"@");})));return()=>cancelAnimationFrame(frame);
  },[fileRequest]);
  useEffect(()=>{
    setMatches([]);setSelected(0);setError("");if(!open){setLoading(false);return;}
    setLoading(true);const controller=new AbortController();const timer=setTimeout(()=>{
      void api<{matches:string[]}>(`/files/${workspaceId}?search=${encodeURIComponent(mention!.query)}`,undefined,"GET",controller.signal).then(result=>{
        const query=mention!.query.toLowerCase(),rank=(path:string)=>{const name=path.split("/").at(-1)!.toLowerCase();return name===query?0:name.startsWith(query)?1:name.includes(query)?2:path.toLowerCase().includes(query)?3:4;};
        if(!controller.signal.aborted)setMatches(result.matches.map(path=>path.replaceAll("\\","/")).sort((a,b)=>rank(a)-rank(b)||a.localeCompare(b)).slice(0,30));
      }).catch(error=>{if(!controller.signal.aborted)setError(error.message);}).finally(()=>{if(!controller.signal.aborted)setLoading(false);});
    },150);return()=>{clearTimeout(timer);controller.abort();};
  },[open,workspaceId,mention?.query]);
  useEffect(()=>{list.current?.querySelector('[aria-selected="true"]')?.scrollIntoView({block:"nearest"});},[selected]);
  return <div className={css.editorWrap} onBlur={event=>{if(!event.currentTarget.contains(event.relatedTarget as Node))close();}}>
    {open&&<section className={css.filePicker} aria-label="引用项目文件"><header><FolderOpen size={14}/><span>项目文件</span><small>输入名称或路径</small></header><div className={css.filePickerList} ref={list} role="listbox" id={id} aria-label="项目文件搜索结果" aria-busy={loading}>{!loading&&!error&&matches.map((path,index)=><button type="button" role="option" id={`${id}-${index}`} aria-selected={selected===index} key={path} title={path} onMouseDown={event=>event.preventDefault()} onPointerMove={()=>setSelected(index)} onClick={()=>choose(path)}><FileCode2 size={18}/><span><strong>{path.split("/").at(-1)}</strong><small>{path.includes("/")?path.slice(0,path.lastIndexOf("/")):"项目根目录"}</small></span>{selected===index&&<kbd>↵</kbd>}</button>)}{(loading||error||!matches.length)&&<p role={error?"alert":"status"}>{loading?<><LoaderCircle size={14}/>正在搜索文件…</>:error?`无法搜索：${error}`:"未找到匹配文件，试试文件名或路径。"}</p>}</div><footer><span>↑ ↓ 选择 · Enter 插入</span><span>Esc 关闭</span></footer></section>}
    <div ref={root} className={css.editor} role="textbox" aria-label="任务输入" aria-multiline="true" aria-disabled={disabled} aria-autocomplete="list" aria-controls={open?id:undefined} aria-expanded={open} aria-activedescendant={open&&!loading&&matches[selected]?`${id}-${selected}`:undefined} contentEditable={!disabled} suppressContentEditableWarning onPaste={e=>{const files=Array.from(e.clipboardData.files);if(files.length&&!disabled){e.preventDefault();onFiles(files);}}}/>
    {!value&&<div className={css.placeholder} aria-hidden="true">{placeholder}</div>}
    {Object.entries(decorators).map(([key,node])=>{const element=editor.current!.getElementByKey(key);return element?createPortal(node,element,key):null;})}
  </div>;
}
