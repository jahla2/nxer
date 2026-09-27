import React from "react";
import type {ApiKey,Project,User} from "../api";
import type {ConsoleModule} from "../routing";

function formatRelativeTime(value:string|null){
  if(!value)return "Never";
  const timestamp=new Date(value).getTime();
  if(Number.isNaN(timestamp))return "Unknown";
  const seconds=Math.max(0,Math.floor((Date.now()-timestamp)/1000));
  if(seconds<60)return "Just now";
  const minutes=Math.floor(seconds/60);
  if(minutes<60)return `${minutes}m ago`;
  const hours=Math.floor(minutes/60);
  if(hours<24)return `${hours}h ago`;
  const days=Math.floor(hours/24);
  return `${days}d ago`;
}

export function OverviewPage({
  user,
  projects,
  selectedProject,
  keys,
  onNavigate,
}:{
  user:User;
  projects:Project[];
  selectedProject:Project|null;
  keys:ApiKey[];
  onNavigate:(module:ConsoleModule)=>void;
}){
  const activeProjects=projects.filter(project=>project.status==="active");
  const activeKeys=keys.filter(key=>key.status==="active");
  const recentKeys=[...activeKeys]
    .sort((a,b)=>new Date(b.updated_at).getTime()-new Date(a.updated_at).getTime())
    .slice(0,4);

  return <div className="page-stack">
    <section className="overview-grid" aria-label="Workspace summary">
      <article className="stat-card">
        <span>Active projects</span>
        <strong>{activeProjects.length}</strong>
        <small>Isolated application workspaces</small>
      </article>
      <article className="stat-card">
        <span>Active API keys</span>
        <strong>{activeKeys.length}</strong>
        <small>{selectedProject?selectedProject.name:"Select a project"}</small>
      </article>
      <article className="stat-card">
        <span>Account</span>
        <strong className="stat-text">{user.display_name}</strong>
        <small>{user.email_verified?"Email verified":"Verification pending"}</small>
      </article>
      <article className="stat-card">
        <span>Gateway mode</span>
        <strong className="stat-text">Free models</strong>
        <small>OpenAI-compatible API</small>
      </article>
    </section>

    <section className="content-grid two-column">
      <article className="content-card">
        <div className="content-card-head">
          <div>
            <span className="card-eyebrow">Get started</span>
            <h2>Connect an application</h2>
          </div>
        </div>
        <div className="step-list">
          <button className="step-item" onClick={()=>onNavigate("Projects")}>
            <span className="step-number">1</span>
            <span><strong>Create a project</strong><small>Separate keys, limits and usage by application.</small></span>
            <span className="step-arrow" aria-hidden="true">→</span>
          </button>
          <button className="step-item" onClick={()=>onNavigate("API Keys")}>
            <span className="step-number">2</span>
            <span><strong>Issue an API key</strong><small>Secrets are displayed once and can be rotated any time.</small></span>
            <span className="step-arrow" aria-hidden="true">→</span>
          </button>
          <button className="step-item" onClick={()=>onNavigate("Playground")}>
            <span className="step-number">3</span>
            <span><strong>Test the gateway</strong><small>Send an OpenAI-compatible completion before wiring your app.</small></span>
            <span className="step-arrow" aria-hidden="true">→</span>
          </button>
        </div>
      </article>

      <article className="content-card">
        <div className="content-card-head">
          <div>
            <span className="card-eyebrow">Current project</span>
            <h2>{selectedProject?.name||"No project selected"}</h2>
          </div>
          {selectedProject&&<span className="status active">active</span>}
        </div>
        {!selectedProject?<div className="empty compact-empty">Choose a project from the top bar to view its credentials.</div>:recentKeys.length?<div className="compact-list">
          {recentKeys.map(item=><div className="compact-row" key={item.id}>
            <div>
              <strong>{item.name}</strong>
              <code>{item.key_prefix}…</code>
            </div>
            <div className="compact-row-meta">
              <span className="status active">active</span>
              <small>Used {formatRelativeTime(item.last_used_at)}</small>
            </div>
          </div>)}
        </div>:<div className="empty compact-empty">No active API keys in this project yet.</div>}
        <button className="text-action" onClick={()=>onNavigate("API Keys")}>Manage API keys <span aria-hidden="true">→</span></button>
      </article>
    </section>

    <section className="notice-card">
      <div className="notice-icon" aria-hidden="true">✓</div>
      <div>
        <strong>Provider credentials stay server-side.</strong>
        <p>Your applications use Nexora API keys and public Nexora model IDs. Upstream provider credentials and routes are not exposed in the console.</p>
      </div>
    </section>
  </div>;
}
