import React,{useEffect,useMemo,useState} from "react";
import type {ApiKey,ApiKeyUpdateInput,ConsoleModel} from "../api";

function toLocalDateTime(value:string|null){
  if(!value)return "";
  const date=new Date(value);
  if(Number.isNaN(date.getTime()))return "";
  const offset=date.getTimezoneOffset()*60_000;
  return new Date(date.getTime()-offset).toISOString().slice(0,16);
}

function toISOStringOrNull(value:string){
  if(!value)return null;
  const date=new Date(value);
  if(Number.isNaN(date.getTime()))return null;
  return date.toISOString();
}

function formatContext(value:number|null){
  if(!value)return "Context not published";
  if(value>=1_000_000)return `${(value/1_000_000).toFixed(value%1_000_000===0?0:1)}M context`;
  if(value>=1_000)return `${Math.round(value/1_000)}K context`;
  return `${value} context`;
}

function parseOptionalPositiveInteger(value:string){
  if(!value.trim())return null;
  const parsed=Number(value);
  return Number.isInteger(parsed)&&parsed>0?parsed:null;
}

export function ApiKeyPolicyForm({
  apiKey,
  models,
  busy,
  onCancel,
  onSave,
}:{
  apiKey?:ApiKey;
  models:ConsoleModel[];
  busy:boolean;
  onCancel:()=>void;
  onSave:(payload:ApiKeyUpdateInput)=>Promise<void>;
}){
  const [name,setName]=useState(apiKey?.name||"Development key");
  const [allowAll,setAllowAll]=useState(apiKey?.allow_all_free_models??true);
  const [selectedModels,setSelectedModels]=useState<string[]>(apiKey?.model_ids||[]);
  const [defaultModel,setDefaultModel]=useState(apiKey?.default_model_id||"");
  const [rpm,setRpm]=useState(apiKey?.requests_per_minute?.toString()||"");
  const [daily,setDaily]=useState(apiKey?.requests_per_day?.toString()||"");
  const [concurrent,setConcurrent]=useState(apiKey?.max_concurrent?.toString()||"");
  const [expiresAt,setExpiresAt]=useState(toLocalDateTime(apiKey?.expires_at||null));
  const [validationError,setValidationError]=useState("");

  useEffect(()=>{
    setName(apiKey?.name||"Development key");
    setAllowAll(apiKey?.allow_all_free_models??true);
    setSelectedModels(apiKey?.model_ids||[]);
    setDefaultModel(apiKey?.default_model_id||"");
    setRpm(apiKey?.requests_per_minute?.toString()||"");
    setDaily(apiKey?.requests_per_day?.toString()||"");
    setConcurrent(apiKey?.max_concurrent?.toString()||"");
    setExpiresAt(toLocalDateTime(apiKey?.expires_at||null));
    setValidationError("");
  },[apiKey]);

  const selectableModels=useMemo(()=>{
    if(allowAll)return models;
    const selected=new Set(selectedModels);
    return models.filter(model=>selected.has(model.id));
  },[allowAll,models,selectedModels]);

  const selectedCount=allowAll?models.length:selectedModels.length;

  function toggleModel(modelId:string){
    setSelectedModels(current=>{
      if(current.includes(modelId)){
        if(defaultModel===modelId)setDefaultModel("");
        return current.filter(id=>id!==modelId);
      }
      return [...current,modelId];
    });
  }

  function chooseDefault(modelId:string){
    setDefaultModel(modelId);
    if(!allowAll&&modelId){
      setSelectedModels(current=>current.includes(modelId)?current:[...current,modelId]);
    }
  }

  async function submit(event:React.FormEvent){
    event.preventDefault();
    setValidationError("");

    const trimmedName=name.trim();
    if(!trimmedName){
      setValidationError("API key name is required.");
      return;
    }
    if(!allowAll&&selectedModels.length===0){
      setValidationError("Select at least one model when using a restricted model scope.");
      return;
    }

    const expiry=toISOStringOrNull(expiresAt);
    if(expiresAt&&!expiry){
      setValidationError("Enter a valid expiration date.");
      return;
    }
    if(expiry&&new Date(expiry).getTime()<=Date.now()){
      setValidationError("Expiration must be in the future.");
      return;
    }

    for(const [label,value] of [["Requests per minute",rpm],["Requests per day",daily],["Max concurrent",concurrent]] as const){
      if(value.trim()&&parseOptionalPositiveInteger(value)===null){
        setValidationError(`${label} must be a positive whole number.`);
        return;
      }
    }

    await onSave({
      name:trimmedName,
      allow_all_free_models:allowAll,
      default_model_id:defaultModel||null,
      model_ids:allowAll?[]:selectedModels,
      requests_per_minute:parseOptionalPositiveInteger(rpm),
      requests_per_day:parseOptionalPositiveInteger(daily),
      max_concurrent:parseOptionalPositiveInteger(concurrent),
      expires_at:expiry,
    });
  }

  return <form className="dialog-form key-policy-form" onSubmit={submit}>
    <section className="policy-section">
      <div className="policy-section-head">
        <div><span className="card-eyebrow">Identity</span><h4>Key details</h4></div>
      </div>
      <label>Key name<input autoFocus value={name} onChange={event=>setName(event.target.value)} maxLength={120} required/></label>
    </section>

    <section className="policy-section">
      <div className="policy-section-head">
        <div><span className="card-eyebrow">Model access</span><h4>Choose what this key can call</h4></div>
        <span className="policy-summary">{allowAll?"All free models":`${selectedCount} selected`}</span>
      </div>

      <div className="scope-options" role="radiogroup" aria-label="Model access policy">
        <label className={"scope-option "+(allowAll?"selected":"")}>
          <input type="radio" name="scope" checked={allowAll} onChange={()=>setAllowAll(true)}/>
          <span><strong>All free models</strong><small>Automatically allow the current and future free-model catalog.</small></span>
        </label>
        <label className={"scope-option "+(!allowAll?"selected":"")}>
          <input type="radio" name="scope" checked={!allowAll} onChange={()=>setAllowAll(false)}/>
          <span><strong>Selected models only</strong><small>Restrict this key to an explicit allowlist.</small></span>
        </label>
      </div>

      {!allowAll&&<div className="model-policy-list">
        {models.length?models.map(model=><label className={"model-policy-option "+(selectedModels.includes(model.id)?"selected":"")} key={model.id}>
          <input type="checkbox" checked={selectedModels.includes(model.id)} onChange={()=>toggleModel(model.id)}/>
          <span className="model-policy-copy">
            <strong>{model.display_name}</strong>
            <code>{model.public_id}</code>
            <small>{formatContext(model.context_length)}</small>
          </span>
        </label>):<div className="inline-warning">No active free models are currently available for restricted scopes.</div>}
      </div>}

      <label>
        Default model
        <select value={defaultModel} onChange={event=>chooseDefault(event.target.value)}>
          <option value="">No default model</option>
          {selectableModels.map(model=><option value={model.id} key={model.id}>{model.display_name} · {model.public_id}</option>)}
        </select>
        <span className="field-hint">Optional. The gateway still accepts an explicit allowed model in each request.</span>
      </label>
    </section>

    <section className="policy-section">
      <div className="policy-section-head">
        <div><span className="card-eyebrow">Limits</span><h4>Override platform defaults</h4></div>
      </div>
      <div className="form-grid">
        <label>Requests / minute<input type="number" min="1" step="1" inputMode="numeric" value={rpm} onChange={event=>setRpm(event.target.value)} placeholder="Platform default"/></label>
        <label>Requests / day<input type="number" min="1" step="1" inputMode="numeric" value={daily} onChange={event=>setDaily(event.target.value)} placeholder="Platform default"/></label>
        <label>Max concurrent<input type="number" min="1" step="1" inputMode="numeric" value={concurrent} onChange={event=>setConcurrent(event.target.value)} placeholder="Platform default"/></label>
      </div>
      <span className="field-hint">Leave a field blank to inherit the gateway platform default.</span>
    </section>

    <section className="policy-section">
      <div className="policy-section-head">
        <div><span className="card-eyebrow">Expiration</span><h4>Limit credential lifetime</h4></div>
      </div>
      <label>
        Expires at
        <input type="datetime-local" value={expiresAt} onChange={event=>setExpiresAt(event.target.value)}/>
        <span className="field-hint">Leave blank for no expiration. Expired keys are rejected by the gateway.</span>
      </label>
    </section>

    {validationError&&<p className="error">{validationError}</p>}

    <div className="dialog-actions sticky-dialog-actions">
      <button type="button" onClick={onCancel}>Cancel</button>
      <button className="primary" disabled={busy||(!allowAll&&selectedModels.length===0)} type="submit">
        {busy?"Saving…":apiKey?"Save changes":"Create API key"}
      </button>
    </div>
  </form>;
}
