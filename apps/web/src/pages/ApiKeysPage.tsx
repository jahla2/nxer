import React from "react";
import type {ApiKey,Project} from "../api";

function formatExpiry(value:string|null){
  if(!value)return "No expiry";
  const date=new Date(value);
  return Number.isNaN(date.getTime())?"Unknown expiry":date.toLocaleDateString();
}

export function ApiKeysPage({
  projects,
  projectId,
  keys,
  onProjectChange,
  onCreate,
  onEdit,
  onRotate,
  onRevoke,
}:{
  projects:Project[];
  projectId:string;
  keys:ApiKey[];
  onProjectChange:(projectId:string)=>void;
  onCreate:()=>void;
  onEdit:(apiKey:ApiKey)=>void;
  onRotate:(apiKey:ApiKey)=>void;
  onRevoke:(apiKey:ApiKey)=>void;
}){
  const activeProjects=projects.filter(project=>project.status==="active");
  const activeKeys=keys.filter(item=>item.status==="active").length;

  return <div className="page-stack">
    <div className="section-toolbar">
      <div className="inline-control">
        <label>
          Project
          <select value={projectId} onChange={event=>onProjectChange(event.target.value)}>
            <option value="">Select project</option>
            {activeProjects.map(project=><option value={project.id} key={project.id}>{project.name}</option>)}
          </select>
        </label>
      </div>
      <button className="primary" disabled={!projectId} onClick={onCreate}>Create API key</button>
    </div>

    {!projectId?<div className="empty-state-card">
      <div className="empty-state-icon" aria-hidden="true">K</div>
      <h2>Select a project</h2>
      <p>API keys are always scoped to a project.</p>
    </div>:keys.length?<div className="page-stack compact-gap">
      <div className="list-summary"><span>{activeKeys} active</span><span>{keys.length-activeKeys} inactive</span></div>
      <div className="table-list">
        {keys.map(item=><article className="table-row key-row" key={item.id}>
          <div className="key-meta">
            <div><strong>{item.name}</strong><span className={"status "+item.status}>{item.status}</span></div>
            <code>{item.key_prefix}…</code>
            <div className="metadata-grid">
              <span><small>RPM</small><strong>{item.requests_per_minute??"Default"}</strong></span>
              <span><small>Daily</small><strong>{item.requests_per_day??"Default"}</strong></span>
              <span><small>Concurrent</small><strong>{item.max_concurrent??"Default"}</strong></span>
              <span><small>Expiry</small><strong>{formatExpiry(item.expires_at)}</strong></span>
            </div>
            <small>Last used {item.last_used_at?new Date(item.last_used_at).toLocaleString():"Never"}</small>
          </div>
          <div className="row-actions">
            <button disabled={item.status!=="active"} onClick={()=>onEdit(item)}>Edit</button>
            <button disabled={item.status!=="active"} onClick={()=>onRotate(item)}>Rotate</button>
            <button className="danger-link" disabled={item.status!=="active"} onClick={()=>onRevoke(item)}>Revoke</button>
          </div>
        </article>)}
      </div>
    </div>:<div className="empty-state-card">
      <div className="empty-state-icon" aria-hidden="true">K</div>
      <h2>No API keys yet</h2>
      <p>Create a key for this project. The raw secret is displayed only once.</p>
      <button className="primary" onClick={onCreate}>Create API key</button>
    </div>}
  </div>;
}
