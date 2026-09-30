import React,{useEffect,useMemo,useState} from "react";
import {api,ConsoleModel} from "../api";

function formatContext(value?:number|null){
  if(!value)return "—";
  if(value>=1_000_000)return `${(value/1_000_000).toFixed(value%1_000_000===0?0:1)}M tokens`;
  if(value>=1_000)return `${Math.round(value/1_000)}K tokens`;
  return `${value} tokens`;
}

export function ModelsPage({
  onUseModel,
}:{
  onUseModel:(modelId:string)=>void;
}){
  const [models,setModels]=useState<ConsoleModel[]>([]);
  const [loading,setLoading]=useState(true);
  const [error,setError]=useState("");

  useEffect(()=>{
    setLoading(true);setError("");
    api.catalogModels()
      .then(data=>{setModels(data);})
      .catch(err=>{setError(err instanceof Error?err.message:String(err));setModels([]);})
      .finally(()=>setLoading(false));
  },[]);

  const count=useMemo(()=>models.length,[models]);

  if(loading)return <div className="page-stack"><div className="empty-state-card subtle-empty">
    <div className="empty-state-icon" aria-hidden="true">M</div>
    <h2>Loading models…</h2>
    <p>Fetching available free models from the catalog.</p>
  </div></div>;

  return <div className="page-stack">
    {error&&<p className="error">{error}</p>}

    {models.length?<section className="page-stack compact-gap">
      <div className="list-summary">
        <span>{count} free model{count!==1?"s":""} available</span>
        <span className="free-badge-label">Free · No API key required</span>
      </div>
      <div className="model-grid">
        {models.map(model=><article className="model-card" key={String(model.id)}>
          <div className="model-card-head">
            <div>
              <span className="model-owner">nexora</span>
              <h3>{model.display_name||model.public_id}</h3>
              <code>{model.public_id}</code>
            </div>
            <span className="status active free-badge">free</span>
          </div>
          <div className="model-facts">
            <span><small>Context</small><strong>{formatContext(model.context_length)}</strong></span>
          </div>
          <div className="capability-list">
            {Object.entries(model.capabilities||{}).filter(([,enabled])=>enabled).map(([cap])=><span key={cap}>{cap}</span>)}
            {!Object.values(model.capabilities||{}).some(Boolean)&&<span>text</span>}
          </div>
          <button onClick={()=>onUseModel(model.public_id)}>Use in Playground</button>
        </article>)}
      </div>
    </section>:<div className="empty-state-card">
      <div className="empty-state-icon" aria-hidden="true">M</div>
      <h2>No free models available</h2>
      <p>The catalog sync may still be running. Check Status for catalog health.</p>
    </div>}
  </div>;
}
