import React,{useMemo,useState} from "react";
import {api,ChatCompletion} from "../api";

export function PlaygroundPage({
  apiKey,
  onApiKeyChange,
  model,
  onModelChange,
  prompt,
  onPromptChange,
}:{
  apiKey:string;
  onApiKeyChange:(value:string)=>void;
  model:string;
  onModelChange:(value:string)=>void;
  prompt:string;
  onPromptChange:(value:string)=>void;
}){
  const [result,setResult]=useState<ChatCompletion|null>(null);
  const [loading,setLoading]=useState(false);
  const [error,setError]=useState("");
  const [elapsedMs,setElapsedMs]=useState<number|null>(null);

  const answer=useMemo(()=>result?.choices?.[0]?.message?.content||"",[result]);

  async function send(){
    if(!apiKey||!model||!prompt.trim())return;
    setLoading(true);setError("");setResult(null);setElapsedMs(null);
    const started=performance.now();
    try{
      const response=await api.chat(apiKey,model,prompt.trim());
      setElapsedMs(Math.round(performance.now()-started));
      setResult(response);
    }catch(err){
      setElapsedMs(Math.round(performance.now()-started));
      setError(err instanceof Error?err.message:String(err));
    }finally{
      setLoading(false);
    }
  }

  return <div className="playground-layout">
    <section className="content-card playground-form">
      <div className="content-card-head">
        <div>
          <span className="card-eyebrow">Request</span>
          <h2>Chat completion</h2>
        </div>
        <span className="method-badge">POST /v1/chat/completions</span>
      </div>

      <div className="form-stack">
        <label>Session API key<input type="password" autoComplete="off" value={apiKey} onChange={event=>onApiKeyChange(event.target.value)} placeholder="nxa_live_…"/></label>
        <label>Model<input value={model} onChange={event=>onModelChange(event.target.value)} placeholder="auto-free"/></label>
        <label>Prompt<textarea value={prompt} onChange={event=>onPromptChange(event.target.value)} placeholder="Ask Nexora something…"/></label>
      </div>

      <div className="playground-actions">
        <span>{prompt.length} characters</span>
        <button className="primary" disabled={loading||!apiKey||!model||!prompt.trim()} onClick={send}>{loading?"Sending…":"Send request"}</button>
      </div>
    </section>

    <section className="content-card playground-response" aria-live="polite">
      <div className="content-card-head">
        <div>
          <span className="card-eyebrow">Response</span>
          <h2>{result?"Completion received":"Waiting for request"}</h2>
        </div>
        {elapsedMs!==null&&<span className="latency-pill">{elapsedMs} ms client round trip</span>}
      </div>

      {error&&<p className="error">{error}</p>}

      {result?<div className="response-stack">
        <div className="assistant-message">{answer||"The provider returned an empty assistant message."}</div>
        <div className="response-meta-grid">
          <span><small>Model</small><strong>{result.model}</strong></span>
          <span><small>Prompt tokens</small><strong>{result.usage?.prompt_tokens??"—"}</strong></span>
          <span><small>Completion tokens</small><strong>{result.usage?.completion_tokens??"—"}</strong></span>
          <span><small>Total tokens</small><strong>{result.usage?.total_tokens??"—"}</strong></span>
        </div>
        <details className="response-details">
          <summary>View response metadata</summary>
          <pre className="output compact-output">{JSON.stringify({id:result.id,model:result.model,finish_reason:result.choices?.[0]?.finish_reason,usage:result.usage},null,2)}</pre>
        </details>
      </div>:!error?<div className="response-placeholder">
        <div className="response-placeholder-icon" aria-hidden="true">›_</div>
        <p>Enter a key, choose a Nexora model and send a prompt to inspect the normalized response.</p>
      </div>:null}
    </section>
  </div>;
}
