import React from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import {useToast} from "../providers/ToastProvider";

function tryPrettyJson(code:string){
  const trimmed=code.trim();
  if(!trimmed.startsWith("{")&&!trimmed.startsWith("["))return code;
  try{return JSON.stringify(JSON.parse(trimmed),null,2)}
  catch{return code}
}

function CodeFence({language,code}:{language:string;code:string}){
  const toast=useToast();
  const display=language==="json"?tryPrettyJson(code):code;

  async function copy(){
    try{
      await navigator.clipboard.writeText(display);
      toast.success("Copied to clipboard");
    }catch{
      toast.error("Copy failed","Select the code manually and copy it.");
    }
  }

  return <div className="docs-code md-code">
    <div className="docs-code-head">
      <span>{language||"text"}</span>
      <button type="button" onClick={copy} aria-label="Copy code">Copy</button>
    </div>
    <pre><code>{display}</code></pre>
  </div>;
}

export function MarkdownMessage({content}:{content:string}){
  return <div className="markdown-message">
    <ReactMarkdown
      remarkPlugins={[remarkGfm]}
      components={{
        code(props){
          const {children,className,node,...rest}=props as {children?:React.ReactNode;className?:string;node?:unknown};
          const inline=!(node&&(node as {position?:unknown}).position&&className);
          const text=String(children??"").replace(/\n$/,"");
          const match=/language-(\w+)/.exec(className||"");
          if(inline&&!match){
            return <code className="md-inline-code" {...rest}>{children}</code>;
          }
          return <CodeFence language={match?.[1]||""} code={text}/>;
        },
        pre({children}){
          return <>{children}</>;
        },
        a({href,children}){
          return <a href={href} target="_blank" rel="noopener noreferrer">{children}</a>;
        },
        table({children}){
          return <div className="data-table-wrap md-table-wrap"><table className="data-table">{children}</table></div>;
        },
      }}
    >{content}</ReactMarkdown>
  </div>;
}
