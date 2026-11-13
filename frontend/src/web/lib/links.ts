import MarkdownIt from "markdown-it";

export type LinkTarget = { kind: "file"; path: string } | { kind: "web"; url: string };
export function resolveLink(value: string, base: string): LinkTarget | undefined {
  value=value.trim();if(!value||value.startsWith("#")||/[\0-\x1f\x7f]/.test(value))return;
  try {
    if(/^https?:\/\//i.test(value)||value.startsWith("//")){const url=new URL(value.startsWith("//")?"https:"+value:value);if(!url.username&&!url.password)return {kind:"web",url:url.href};return;}
    if(/^[a-z][\w+.-]*:/i.test(value)&&!/^file:/i.test(value)&&!/^[a-z]:[/\\]/i.test(value))return;
    value=value.replace(/:\d+(?::\d+)?$/,"").replace(/\\/g,"/").replace(/%(?![a-f\d]{2})/gi,"%25");
    if(value.startsWith("~/"))return {kind:"file",path:decodeURIComponent(value)};
    const folder=encodeURI(base.replace(/\\/g,"/").replace(/\/?$/,"/")).replaceAll("#","%23").replaceAll("?","%3F");
    const root=folder.startsWith("//")?"file:"+folder:"file://"+(folder.startsWith("/")?"":"/")+folder;
    const target=/^file:/i.test(value)?value:/^[a-z]:\//i.test(value)?"file:///"+value:value;
    const url=new URL(target,root);if(url.protocol!=="file:")return;
    return {kind:"file",path:(url.hostname&&url.hostname!=="localhost"?"//"+url.hostname:"")+decodeURIComponent(url.pathname).replace(/^\/([a-z]:\/)/i,"$1")};
  } catch { return; }
}

const markdown=new MarkdownIt({html:false,linkify:true,breaks:true});
const escape=markdown.utils.escapeHtml;
const fileAnchor=(path:string,label:string)=>'<a href="#" role="button" data-file-path="'+escape(path)+'" title="'+escape(path)+'">'+label+"</a>";
const isFile=(value:string)=>/^(?:file:|[a-z]:[/\\]|[/\\]|~[/\\]|\.{1,2}[/\\])|\.(?:txt|md|markdown|mdown|html?|css|scss|less|[cm]?jsx?|tsx?|jsonc?|ya?ml|toml|ini|conf|xml|csv|tsv|py|go|rs|java|[ch]|[ch]pp|cs|rb|php|sh|bash|zsh|ps1|bat|cmd|sql|vue|svelte|png|jpe?g|webp|gif|svg|ico|pdf|docx|xlsx|pptx|zip)(?:#L\d+|:\d+(?::\d+)?)?$/i.test(value)||/^(?:README|LICENSE|Dockerfile|Makefile)$/i.test(value);
const nativePaths=/(?<![\w])(?:file:\/\/[^\s<>"\x60]+|[a-z]:[/\\][^\r\n<>"\x60]*?\.[a-z\d]{1,12}|\/(?:[\w.~%-]+\/)+[^\s<>"\x60]*\.[a-z\d]{1,12})(?=$|[\s，。；、"'\x60),;])/gi;
markdown.inline.ruler.before("text","native_path",(state,silent)=>{
  if(state.linkLevel)return false;nativePaths.lastIndex=0;const match=nativePaths.exec(state.src.slice(state.pos));if(!match)return false;
  const prefix=state.src.slice(state.pos,state.pos+match.index);if(/[*_[\]<>!&\x60\n\\]/.test(prefix)||/(?:[a-z][\w+.-]*:\/{1,2}|www\.)\S*$/i.test(state.src.slice(0,state.pos+match.index)))return false;
  const target=resolveLink(match[0],String(state.env.base||"/"));if(target?.kind!=="file")return false;
  if(!silent){if(prefix)state.push("text","",0).content=prefix;const open=state.push("link_open","a",1);open.attrs=[["href",match[0]]];state.push("text","",0).content=match[0];state.push("link_close","a",-1);}state.pos+=match.index+match[0].length;return true;
});
const code=markdown.renderer.rules.code_inline!;
markdown.renderer.rules.code_inline=(tokens,i,options,env,self)=>{const html=code(tokens,i,options,env,self),target=isFile(tokens[i].content)?resolveLink(tokens[i].content,String(env?.base||"/")):undefined;return !env?.inLink&&target?.kind==="file"?fileAnchor(target.path,html):html;};
markdown.renderer.rules.link_open=(tokens,i,options,env,self)=>{const token=tokens[i],target=resolveLink(String(token.attrGet("href")||""),String(env?.base||"/"));if(env)env.inLink=true;if(target?.kind==="file"){token.attrSet("href","#");token.attrSet("role","button");token.attrSet("data-file-path",target.path);token.attrSet("title",target.path);}else if(target?.kind==="web"){token.attrSet("href",target.url);token.attrSet("title",target.url);token.attrSet("rel","noopener noreferrer");}return self.renderToken(tokens,i,options);};
// Local Markdown links often contain unescaped Windows separators or spaces.
// Keep the library parser for ordinary Markdown and web links.
markdown.inline.ruler.before("link","local_file_link",(state,silent)=>{
  const match=/^\[([^\]\n]+)\]\(([^)\n]+)\)/.exec(state.src.slice(state.pos));if(!match)return false;
  const target=resolveLink(match[2],String(state.env.base||"/"));if(target?.kind!=="file")return false;
  if(!silent){const open=state.push("link_open","a",1);open.attrs=[["href",match[2]]];state.push("text","",0).content=match[1];state.push("link_close","a",-1);}state.pos+=match[0].length;return true;
});
markdown.renderer.rules.link_close=(_tokens,_i,_options,env)=>{if(env)env.inLink=false;return "</a>";};
markdown.renderer.rules.image=(tokens,i,_options,env)=>{const label=escape(tokens[i].content||"图片"),target=resolveLink(String(tokens[i].attrGet("src")||""),String(env?.base||"/"));return !env?.inLink&&target?.kind==="file"?fileAnchor(target.path,label):label;};
export function renderMarkdown(text:string,base:string){return markdown.render(text,{base,inLink:false});}
export function markdownText(text:string){
  return markdown.parse(text,{}).map(block=>block.type==="inline"?(block.children||[]).map(token=>["text","code_inline","image"].includes(token.type)?token.content:["softbreak","hardbreak"].includes(token.type)?" ":"").join(""):block.type==="fence"||block.type==="code_block"?block.content:"").join(" ").replace(/\s+/g," ").trim();
}
