import React from "react";
import type {Project} from "../api";

export function ProjectsPage({
  projects,
  projectId,
  onSelect,
  onCreate,
  onRename,
  onArchive,
}:{
  projects:Project[];
  projectId:string;
  onSelect:(projectId:string)=>void;
  onCreate:()=>void;
  onRename:(project:Project)=>void;
  onArchive:(project:Project)=>void;
}){
  const activeCount=projects.filter(project=>project.status==="active").length;

  return <div className="page-stack">
    <div className="section-toolbar">
      <div>
        <p className="section-copy">Create isolated workspaces for API keys, limits and usage.</p>
        <small className="toolbar-meta">{activeCount} active · {projects.length} total</small>
      </div>
      <button className="primary" onClick={onCreate}>Create project</button>
    </div>

    {projects.length?<div className="table-list">
      {projects.map(project=><article className={"table-row project-row "+(projectId===project.id?"selected-row":"")} key={project.id}>
        <button className="row-main" disabled={project.status!=="active"} onClick={()=>onSelect(project.id)}>
          <span className="project-avatar" aria-hidden="true">{project.name.slice(0,1).toUpperCase()}</span>
          <span className="row-main-copy">
            <span><strong>{project.name}</strong><span className={"status "+project.status}>{project.status}</span></span>
            <small>Created {new Date(project.created_at).toLocaleDateString()}</small>
          </span>
        </button>
        <div className="row-actions">
          <button disabled={project.status!=="active"} onClick={()=>onRename(project)}>Rename</button>
          <button className="danger-link" disabled={project.status!=="active"} onClick={()=>onArchive(project)}>Archive</button>
        </div>
      </article>)}
    </div>:<div className="empty-state-card">
      <div className="empty-state-icon" aria-hidden="true">P</div>
      <h2>No projects yet</h2>
      <p>Create your first project to isolate credentials and usage.</p>
      <button className="primary" onClick={onCreate}>Create project</button>
    </div>}
  </div>;
}
