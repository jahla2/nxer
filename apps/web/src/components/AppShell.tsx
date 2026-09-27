import React,{useEffect,useMemo,useRef,useState} from "react";
import type {Project,User} from "../api";
import {useFocusTrap} from "../hooks/useFocusTrap";
import type {ConsoleModule} from "../routing";

type NavGroup="Workspace"|"Observe"|"Account";

type NavItem={
  id:ConsoleModule;
  label:string;
  description:string;
  icon:"overview"|"projects"|"keys"|"models"|"playground"|"docs"|"usage"|"requests"|"settings"|"status"|"admin";
  group:NavGroup;
};

export const NAV_ITEMS:NavItem[]=[
  {id:"Overview",label:"Overview",description:"Project activity, credentials and gateway health at a glance.",icon:"overview",group:"Workspace"},
  {id:"Projects",label:"Projects",description:"Create and manage isolated application projects.",icon:"projects",group:"Workspace"},
  {id:"API Keys",label:"API Keys",description:"Issue, rotate, revoke and scope developer credentials.",icon:"keys",group:"Workspace"},
  {id:"Models",label:"Models",description:"Browse the public Nexora free-model catalog.",icon:"models",group:"Workspace"},
  {id:"Playground",label:"Playground",description:"Test OpenAI-compatible requests against the gateway.",icon:"playground",group:"Workspace"},
  {id:"Docs",label:"Docs",description:"Integrate Nexora with OpenAI-compatible clients and direct HTTP requests.",icon:"docs",group:"Workspace"},
  {id:"Usage",label:"Usage",description:"Review request and token consumption across your keys.",icon:"usage",group:"Observe"},
  {id:"Requests",label:"Requests",description:"Inspect recent request status and performance telemetry.",icon:"requests",group:"Observe"},
  {id:"Status",label:"Status",description:"Check control-plane, gateway, catalog and worker availability.",icon:"status",group:"Observe"},
  {id:"Settings",label:"Settings",description:"Review profile and session configuration.",icon:"settings",group:"Account"},
  {id:"Admin",label:"Admin",description:"Inspect background jobs and audit activity.",icon:"admin",group:"Account"},
];

function Icon({name}:{name:NavItem["icon"]}){
  const common={width:18,height:18,viewBox:"0 0 24 24",fill:"none",stroke:"currentColor",strokeWidth:1.8,strokeLinecap:"round" as const,strokeLinejoin:"round" as const,"aria-hidden":true};
  if(name==="overview")return <svg {...common}><rect x="3" y="3" width="7" height="7" rx="2"/><rect x="14" y="3" width="7" height="7" rx="2"/><rect x="3" y="14" width="7" height="7" rx="2"/><rect x="14" y="14" width="7" height="7" rx="2"/></svg>;
  if(name==="projects")return <svg {...common}><path d="M3 7.5h6l2 2H21v9.5a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/><path d="M3 7.5V6a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v1.5"/></svg>;
  if(name==="keys")return <svg {...common}><circle cx="8" cy="15" r="4"/><path d="m11 12 8-8"/><path d="m15 8 2 2"/><path d="m17 6 2 2"/></svg>;
  if(name==="models")return <svg {...common}><path d="m12 3 8 4.5-8 4.5-8-4.5z"/><path d="m4 12 8 4.5 8-4.5"/><path d="m4 16.5 8 4.5 8-4.5"/></svg>;
  if(name==="playground")return <svg {...common}><path d="m8 9-4 3 4 3"/><path d="m16 9 4 3-4 3"/><path d="m14 5-4 14"/></svg>;
  if(name==="docs")return <svg {...common}><path d="M6 3h9l3 3v15H6z"/><path d="M15 3v4h4"/><path d="M9 11h6"/><path d="M9 15h6"/></svg>;
  if(name==="usage")return <svg {...common}><path d="M4 19V9"/><path d="M10 19V5"/><path d="M16 19v-7"/><path d="M22 19V3"/></svg>;
  if(name==="requests")return <svg {...common}><path d="M8 6h13"/><path d="M8 12h13"/><path d="M8 18h13"/><circle cx="3" cy="6" r=".8" fill="currentColor" stroke="none"/><circle cx="3" cy="12" r=".8" fill="currentColor" stroke="none"/><circle cx="3" cy="18" r=".8" fill="currentColor" stroke="none"/></svg>;
  if(name==="settings")return <svg {...common}><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.7 1.7 0 0 0 .34 1.88l.06.06-2.83 2.83-.06-.06A1.7 1.7 0 0 0 15 19.4a1.7 1.7 0 0 0-1 .6 1.7 1.7 0 0 0-.4 1.1V21H9.6v-.1A1.7 1.7 0 0 0 8 19.4a1.7 1.7 0 0 0-1.88.34l-.06.06-2.83-2.83.06-.06A1.7 1.7 0 0 0 3.6 15a1.7 1.7 0 0 0-.6-1 1.7 1.7 0 0 0-1.1-.4H1.8V9.6h.1A1.7 1.7 0 0 0 3.6 8a1.7 1.7 0 0 0-.34-1.88l-.06-.06 2.83-2.83.06.06A1.7 1.7 0 0 0 8 3.6a1.7 1.7 0 0 0 1-.6 1.7 1.7 0 0 0 .4-1.1V1.8h4v.1A1.7 1.7 0 0 0 15 3.6a1.7 1.7 0 0 0 1.88-.34l.06-.06 2.83 2.83-.06.06A1.7 1.7 0 0 0 19.4 8a1.7 1.7 0 0 0 .6 1 1.7 1.7 0 0 0 1.1.4h.1v4h-.1A1.7 1.7 0 0 0 19.4 15z"/></svg>;
  if(name==="admin")return <svg {...common}><path d="M12 3 20 6v5c0 5-3.4 8.3-8 10-4.6-1.7-8-5-8-10V6z"/><path d="M9 12.5 11 14l4-4"/></svg>;
  return <svg {...common}><path d="M4 12a8 8 0 1 1 16 0 8 8 0 0 1-16 0z"/><path d="M8.5 12.5 11 15l4.5-6"/></svg>;
}

function initials(name:string,email:string){
  const source=(name||email).trim();
  if(!source)return "NX";
  const parts=source.split(/\s+/).filter(Boolean);
  if(parts.length===1)return parts[0].slice(0,2).toUpperCase();
  return (parts[0][0]+parts[parts.length-1][0]).toUpperCase();
}

export function AppShell({
  user,
  active,
  onNavigate,
  projects,
  projectId,
  onProjectChange,
  onLogout,
  children,
}:{
  user:User;
  active:ConsoleModule;
  onNavigate:(module:ConsoleModule)=>void;
  projects:Project[];
  projectId:string;
  onProjectChange:(projectId:string)=>void;
  onLogout:()=>Promise<void>|void;
  children:React.ReactNode;
}){
  const [mobileOpen,setMobileOpen]=useState(false);
  const [userMenuOpen,setUserMenuOpen]=useState(false);
  const sidebarRef=useRef<HTMLElement|null>(null);
  const userMenuRef=useRef<HTMLDivElement|null>(null);
  const headingRef=useRef<HTMLHeadingElement|null>(null);

  const activeProject=useMemo(()=>projects.find(project=>project.id===projectId&&project.status==="active")||null,[projects,projectId]);
  const page=NAV_ITEMS.find(item=>item.id===active)??NAV_ITEMS[0];
  const activeProjects=projects.filter(project=>project.status==="active");
  const visibleItems=NAV_ITEMS.filter(item=>item.id!=="Admin"||user.role==="admin");
  const groups:NavGroup[]=["Workspace","Observe","Account"];

  useFocusTrap(sidebarRef,mobileOpen,()=>setMobileOpen(false));

  useEffect(()=>{
    document.body.classList.toggle("nav-open",mobileOpen);
    return()=>document.body.classList.remove("nav-open");
  },[mobileOpen]);

  useEffect(()=>{
    function onPointerDown(event:MouseEvent){
      if(userMenuOpen&&userMenuRef.current&&!userMenuRef.current.contains(event.target as Node)){
        setUserMenuOpen(false);
      }
    }
    function onKeyDown(event:KeyboardEvent){
      if(event.key==="Escape")setUserMenuOpen(false);
    }
    document.addEventListener("mousedown",onPointerDown);
    document.addEventListener("keydown",onKeyDown);
    return()=>{
      document.removeEventListener("mousedown",onPointerDown);
      document.removeEventListener("keydown",onKeyDown);
    };
  },[userMenuOpen]);

  useEffect(()=>{
    window.setTimeout(()=>headingRef.current?.focus({preventScroll:true}),0);
  },[active]);

  function navigate(module:ConsoleModule){
    onNavigate(module);
    setMobileOpen(false);
    setUserMenuOpen(false);
  }

  return <div className="app-layout">
    <a className="skip-link" href="#main-content">Skip to main content</a>

    <aside
      ref={sidebarRef}
      className={"app-sidebar "+(mobileOpen?"is-open":"")}
      aria-label="Primary navigation"
      tabIndex={mobileOpen?-1:undefined}
    >
      <div className="brand-row">
        <div className="brand-mark" aria-hidden="true">N</div>
        <div className="brand-copy">
          <strong>Nexora</strong>
          <span>AI Gateway</span>
        </div>
        <button className="sidebar-close" onClick={()=>setMobileOpen(false)} aria-label="Close navigation">×</button>
      </div>

      <div className="sidebar-project">
        <span className="sidebar-kicker">Current project</span>
        <strong>{activeProject?.name||"No project selected"}</strong>
      </div>

      <nav className="sidebar-nav">
        {groups.map(group=><div className="nav-group" key={group}>
          <span className="nav-group-label">{group}</span>
          {visibleItems.filter(item=>item.group===group).map(item=><button
            key={item.id}
            className={"nav-item "+(active===item.id?"active":"")}
            onClick={()=>navigate(item.id)}
            aria-current={active===item.id?"page":undefined}
            title={item.label}
          >
            <span className="nav-icon"><Icon name={item.icon}/></span>
            <span className="sidebar-label">{item.label}</span>
          </button>)}
        </div>)}
      </nav>

      <div className="sidebar-footer">
        <div className="environment-pill"><span className="environment-dot"/>Alpha</div>
        <span className="sidebar-version">v0.1.0</span>
      </div>
    </aside>

    {mobileOpen&&<button className="sidebar-scrim" aria-label="Close navigation" onClick={()=>setMobileOpen(false)}/>}

    <div className="app-stage">
      <header className="topbar">
        <div className="topbar-leading">
          <button className="menu-trigger" onClick={()=>setMobileOpen(true)} aria-label="Open navigation" aria-expanded={mobileOpen}>
            <span/><span/><span/>
          </button>
          <div className="page-heading">
            <h1 ref={headingRef} tabIndex={-1}>{page.label}</h1>
            <p>{page.description}</p>
          </div>
        </div>

        <div className="topbar-actions">
          <label className="project-switcher">
            <span>Project</span>
            <select value={projectId} onChange={event=>onProjectChange(event.target.value)} aria-label="Current project">
              <option value="">Select project</option>
              {activeProjects.map(project=><option key={project.id} value={project.id}>{project.name}</option>)}
            </select>
          </label>

          <div className="user-menu" ref={userMenuRef}>
            <button
              className="user-menu-trigger"
              onClick={()=>setUserMenuOpen(open=>!open)}
              aria-haspopup="menu"
              aria-expanded={userMenuOpen}
              aria-controls="user-menu-popover"
            >
              <span className="avatar">{initials(user.display_name,user.email)}</span>
              <span className="user-menu-copy">
                <strong>{user.display_name}</strong>
                <small>{user.email}</small>
              </span>
              <span className="chevron" aria-hidden="true">⌄</span>
            </button>
            {userMenuOpen&&<div className="user-popover" role="menu" id="user-menu-popover">
              <div className="user-popover-head">
                <span className="avatar avatar-lg">{initials(user.display_name,user.email)}</span>
                <div><strong>{user.display_name}</strong><small>{user.email}</small></div>
              </div>
              <div className="user-popover-meta">
                <span>{user.role}</span>
                <span className={user.email_verified?"verified":"unverified"}>{user.email_verified?"Email verified":"Email not verified"}</span>
              </div>
              <div className="user-popover-actions">
                <button role="menuitem" onClick={()=>navigate("Settings")}>Settings</button>
                <button role="menuitem" className="danger-menu" onClick={()=>{setUserMenuOpen(false);void onLogout()}}>Log out</button>
              </div>
            </div>}
          </div>
        </div>
      </header>

      <main className="app-content" id="main-content" tabIndex={-1}>
        <div className="content-container">{children}</div>
      </main>
    </div>
  </div>;
}
