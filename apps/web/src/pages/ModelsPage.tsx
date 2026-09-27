import React,{useEffect,useMemo,useState} from "react";
import {api,NexoraModel} from "../api";

function formatContext(value?:number){
  if(!value)return "Not published";
  if(value>=1_000_000)return `${(value/1_000_000).toFixed(value%1_000_000===0?0:1)}M tokens`;
  if(value>=1_000)return `${Math.round(value/1_000)}K tokens`;
  return `${value} tokens`;
}

export function ModelsPage({
  apiKey,
  onApiKeyChange,
  onUseModel,
}:{
  apiKey:string;
  onApiKeyChange:(value:string)=>void;
  onUseModel:(modelId:string)=>void;
}){
  const [models,setModels]=useState<NexoraModel[]>([]);
  const [loading,setLoading]=useState(false);
  const [error,setError]=useState("");

  async function loadModels(){
    if(!apiKey)return;
    setLoading(true);setError("");
    try{
      const response=await api.models(apiKey);
      setModels(response.data||[]);
    }catch(err){
      setError(err instanceof Error?err.message:String(err));
      setModels([]);
    }finally{
      setLoading(false);
    }
  }

  useEffect(()=>{setModels([]);setError("")},[apiKey]);

  const activeCount=useMemo(()=>models.filter(model=>model.status!=="inactive").length,[models]);

  return <div className="page-stack">
    <section className="credential-card">
      <div>
        <span className="card-eyebrow">Session credential</span>
        <h2>Browse the model catalog</h2>
        <p>The gateway requires a Nexora API key. The browser keeps this key only in memory for the current console session.</p>
      </div>
      <div className="credential-actions">
        <label>
          API key
          <input type="password" autoComplete="off" value={apiKey} onChange={event=>onApiKeyChange(event.target.value)} placeholder="nxa_live_…"/>
        </label>
        <button className="primary" disabled={!apiKey||loading} onClick={loadModels}>{loading?"Loading…":"Load models"}</button>
      </div>
    </section>

    {error&&<p className="error">{error}</p>}

    {models.length?<section className="page-stack compact-gap">
      <div className="list-summary"><span>{activeCount} models available</span><span>Provider-neutral Nexora IDs</span></div>
      <div className="model-grid">
        {models.map(model=><article className="model-card" key={model.id}>
          <div className="model-card-head">
            <div>
              <span className="model-owner">{model.owned_by||"nexora"}</span>
              <h3>{model.display_name||model.id}</h3>
              <code>{model.id}</code>
            </div>
            <span className="status active">{model.free?"free":"available"}</span>
          </div>
          <div className="model-facts">
            <span><small>Context</small><strong>{formatContext(model.context_length)}</strong></span>
            <span><small>Status</small><strong>{model.status||"active"}</strong></span>
          </div>
          <div className="capability-list">
            {Object.entries(model.capabilities||{}).filter(([,enabled])=>enabled).map(([capability])=><span key={capability}>{capability}</span>)}
            {!Object.values(model.capabilities||{}).some(Boolean)&&<span>text</span>}
          </div>
          <button onClick={()=>onUseModel(model.id)}>Use in Playground</button>
        </article>)}
      </div>
    </section>:!loading&&apiKey&&!error?<div className="empty-state-card">
      <div className="empty-state-icon" aria-hidden="true">M</div>
      <h2>No models returned</h2>
      <p>Refresh the catalog or verify that this key can access free models.</p>
    </div>:!apiKey?<div className="empty-state-card subtle-empty">
      <div className="empty-state-icon" aria-hidden="true">M</div>
      <h2>Enter a session API key</h2>
      <p>You can paste a key here or use the one-time secret after creating or rotating a key.</p>
    </div>:null}
  </div>;
}
