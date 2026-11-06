import { useEffect, useRef } from "react";
import { createEditor, $getRoot, $createParagraphNode, $createTextNode, $createLineBreakNode, $getSelection, $isRangeSelection, KEY_ENTER_COMMAND, COMMAND_PRIORITY_HIGH } from "lexical";
import { registerPlainText } from "@lexical/plain-text";
import { registerHistory, createEmptyHistoryState } from "@lexical/history";
import { observeComposition } from "./dsh/keyboard-composition";
import css from "./ui.module.css";

export function Editor({value,onChange,onSend,onFiles,disabled=false,placeholder}:{value:string;onChange:(text:string)=>void;onSend:()=>void;onFiles:(files:File[])=>void;disabled?:boolean;placeholder:string}) {
  const root=useRef<HTMLDivElement>(null);const change=useRef(onChange);change.current=onChange;const send=useRef(onSend);send.current=onSend;
  const editor=useRef<ReturnType<typeof createEditor>>();if(!editor.current)editor.current=createEditor({namespace:"pi-desk-dsh",onError:error=>{throw error;}});
  useEffect(()=>{const e=editor.current!;e.setRootElement(root.current);const composition=observeComposition(document);const plain=registerPlainText(e);const history=registerHistory(e,createEmptyHistoryState(),300);const update=e.registerUpdateListener(({editorState,tags})=>{if(!tags.has("external"))change.current(editorState.read(()=>$getRoot().getTextContent()));});const enter=e.registerCommand(KEY_ENTER_COMMAND,event=>{if(!event||composition.guards(event)||e.isComposing())return false;event.preventDefault();if(event.shiftKey||(localStorage.getItem("pi-desk.enter")==="ctrl-enter"&&!event.ctrlKey&&!event.metaKey)){e.update(()=>{const selection=$getSelection();if($isRangeSelection(selection))selection.insertLineBreak();});}else send.current();return true;},COMMAND_PRIORITY_HIGH);return()=>{plain();history();update();enter();composition.dispose();e.setRootElement(null);};},[]);
  useEffect(()=>{const e=editor.current!;e.setEditable(!disabled);},[disabled]);
  useEffect(()=>{const e=editor.current!;if(e.getEditorState().read(()=>$getRoot().getTextContent())===value)return;e.update(()=>{const p=$createParagraphNode();value.split("\n").forEach((line,i)=>{if(i)p.append($createLineBreakNode());if(line)p.append($createTextNode(line));});$getRoot().clear().append(p);},{tag:"external"});},[value]);
  return <div className={css.editorWrap}><div ref={root} className={css.editor} role="textbox" aria-label="任务输入" aria-multiline="true" aria-disabled={disabled} contentEditable={!disabled} suppressContentEditableWarning onPaste={e=>{const files=Array.from(e.clipboardData.files);if(files.length){e.preventDefault();onFiles(files);}}}/>{!value&&<div className={css.placeholder} aria-hidden="true">{placeholder}</div>}</div>;
}
