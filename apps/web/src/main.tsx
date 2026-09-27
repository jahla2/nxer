import React,{useEffect,useMemo,useState} from "react";
import {createRoot} from "react-dom/client";
import {api,Project,ApiKey,User} from "./api";
import {AppShell,ConsoleModule} from "./components/AppShell";
import "./styles.css";

function Dialog({title,children,onClose}:{title:string;children:React.ReactNode;onClose:()=>void}){
 return <div className="dialog-backdrop" role="presentation" onMouseDown={e=>{if(e.currentTarget===e.target)onClose()}}>
   <section className="dialog" role="dialog" aria-modal="true" aria-label={title}>
     <div className="dialog-head"><h3>{title}</h3><button className="icon-button" onClick={onClose} aria-label="Close">×</button></div>
     {children}
   </section>
 </div>;
}

function App(){
 const [user,setUser]=useState<User|null|undefined>(undefined);
 const [authMode,setAuthMode]=useState<"login"|"register">("login");
 const [email,setEmail]=useState(""),[password,setPassword]=useState(""),[displayName,setDisplayName]=useState("");
 const [active,setActive]=useState<ConsoleModule>("Overview"),[key,setKey]=useState(""),[projects,setProjects]=useState<Project[]>([]);
 const [projectId,setProjectId]=useState(""),[keys,setKeys]=useState<ApiKey[]>([]),[data,setData]=useState<any>(null),[error,setError]=useState("");
 const [chatPrompt,setChatPrompt]=useState("Say hello from Nexora"),[model,setModel]=useState("auto-free"),[busy,setBusy]=useState(false);

 const [projectDialog,setProjectDialog]=useState<{mode:"create"|"rename";project?:Project}|null>(null);
 const [projectName,setProjectName]=useState("");
 const [keyDialog,setKeyDialog]=useState<{mode:"create"|"edit";apiKey?:ApiKey}|null>(null);
 const [keyName,setKeyName]=useState("");
 const [rpm,setRpm]=useState(""),[daily,setDaily]=useState(""),[concurrent,setConcurrent]=useState("");
 const [secretDialog,setSecretDialog]=useState<{label:string;secret:string}|null>(null);
 const [confirmDialog,setConfirmDialog]=useState<{title:string;message:string;action:()=>Promise<void>}|null>(null);

 const selectedProject=useMemo(()=>projects.find(p=>p.id===projectId)||null,[projects,projectId]);

 async function run(fn:()=>Promise<any>){
   setBusy(true);setError("");
   try{setData(await fn())}catch(e){setError(e instanceof Error?e.message:String(e))}
   finally{setBusy(false)}
 }

 async function loadProjects(){
   if(!user)return;
   const p=await api.projects();
   setProjects(p);
   const activeProject=p.find(item=>item.status==="active");
   if(!p.some(item=>item.id===projectId && item.status==="active")){
     setProjectId(activeProject?.id||"");
   }
 }

 async function loadKeys(pid=projectId){
   if(!user||!pid){setKeys([]);return}
   setKeys(await api.keys(pid));
 }

 useEffect(()=>{api.me().catch(()=>api.refresh()).then(setUser).catch(()=>setUser(null))},[]);
 useEffect(()=>{if(user)loadProjects().catch(e=>setError(e instanceof Error?e.message:String(e)))},[user]);
 useEffect(()=>{loadKeys().catch(()=>setKeys([]))},[user,projectId]);

 async function submitAuth(e:React.FormEvent){
   e.preventDefault();setBusy(true);setError("");
   try{
     const next=authMode==="register"
       ? await api.register(displayName,email,password)
       : await api.login(email,password);
     setUser(next);setPassword("");
   }catch(err){setError(err instanceof Error?err.message:String(err))}
   finally{setBusy(false)}
 }

 async function logout(){
   try{await api.logout()}finally{
     setUser(null);setProjects([]);setKeys([]);setKey("");setData(null);
   }
 }

 function openCreateProject(){setProjectName("");setProjectDialog({mode:"create"})}
 function openRenameProject(project:Project){setProjectName(project.name);setProjectDialog({mode:"rename",project})}

 async function submitProject(e:React.FormEvent){
   e.preventDefault();
   if(!projectDialog)return;
   setBusy(true);setError("");
   try{
     if(projectDialog.mode==="create"){
       const created=await api.createProject(projectName);
       await loadProjects();
       setProjectId(created.id);
     }else if(projectDialog.project){
       await api.renameProject(projectDialog.project.id,projectName);
       await loadProjects();
     }
     setProjectDialog(null);
   }catch(err){setError(err instanceof Error?err.message:String(err))}
   finally{setBusy(false)}
 }

 function confirmArchive(project:Project){
   setConfirmDialog({
     title:"Archive project",
     message:`Archive "${project.name}"? All active API keys in this project will be revoked.`,
     action:async()=>{
       await api.archiveProject(project.id);
       await loadProjects();
       if(project.id===projectId)setKeys([]);
     }
   });
 }

 function openCreateKey(){
   setKeyName("Development key");setRpm("");setDaily("");setConcurrent("");
   setKeyDialog({mode:"create"});
 }

 function openEditKey(apiKey:ApiKey){
   setKeyName(apiKey.name);
   setRpm(apiKey.requests_per_minute?.toString()||"");
   setDaily(apiKey.requests_per_day?.toString()||"");
   setConcurrent(apiKey.max_concurrent?.toString()||"");
   setKeyDialog({mode:"edit",apiKey});
 }

 async function submitKey(e:React.FormEvent){
   e.preventDefault();
   if(!keyDialog||!projectId)return;
   setBusy(true);setError("");
   try{
     if(keyDialog.mode==="create"){
       const created=await api.createKey(projectId,keyName);
       setSecretDialog({label:`New API key · ${created.name}`,secret:created.api_key});
       await loadKeys();
     }else if(keyDialog.apiKey){
       await api.updateKey(keyDialog.apiKey.id,{
         name:keyName,
         requests_per_minute:rpm?Number(rpm):null,
         requests_per_day:daily?Number(daily):null,
         max_concurrent:concurrent?Number(concurrent):null,
       });
       await loadKeys();
     }
     setKeyDialog(null);
   }catch(err){setError(err instanceof Error?err.message:String(err))}
   finally{setBusy(false)}
 }

 function confirmRotate(apiKey:ApiKey){
   setConfirmDialog({
     title:"Rotate API key",
     message:`Rotate "${apiKey.name}"? The current key will stop working immediately.`,
     action:async()=>{
       const rotated=await api.rotateKey(apiKey.id);
       setSecretDialog({label:`Rotated API key · ${rotated.name}`,secret:rotated.api_key});
       await loadKeys();
     }
   });
 }

 function confirmRevoke(apiKey:ApiKey){
   setConfirmDialog({
     title:"Revoke API key",
     message:`Revoke "${apiKey.name}"? This cannot be undone.`,
     action:async()=>{
       await api.revokeKey(apiKey.id);
       await loadKeys();
     }
   });
 }

 async function runConfirmedAction(){
   if(!confirmDialog)return;
   const action=confirmDialog.action;
   setConfirmDialog(null);setBusy(true);setError("");
   try{await action()}catch(err){setError(err instanceof Error?err.message:String(err))}
   finally{setBusy(false)}
 }

 if(user===undefined){
   return <main className="shell"><section className="panel"><p>Loading session…</p></section></main>;
 }

 if(!user){
   return <main className="shell auth-shell">
     <section className="panel auth-panel">
       <p className="eyebrow">NEXORA AI</p>
       <h1>{authMode==="login"?"Sign in":"Create account"}</h1>
       <p className="subtitle">Access your Nexora developer console.</p>
       <form onSubmit={submitAuth}>
         {authMode==="register"&&<label>Name<input value={displayName} onChange={e=>setDisplayName(e.target.value)} minLength={2} required/></label>}
         <label>Email<input type="email" value={email} onChange={e=>setEmail(e.target.value)} required/></label>
         <label>Password<input type="password" value={password} onChange={e=>setPassword(e.target.value)} minLength={authMode==="register"?12:1} required/></label>
         {error&&<p className="error">{error}</p>}
         <button disabled={busy} type="submit">{busy?"Please wait…":authMode==="login"?"Sign in":"Create account"}</button>
       </form>
       <button className="link-button" onClick={()=>{setAuthMode(authMode==="login"?"register":"login");setError("")}}>
         {authMode==="login"?"Need an account? Register":"Already registered? Sign in"}
       </button>
     </section>
   </main>;
 }

 return <AppShell
   user={user}
   active={active}
   onNavigate={module=>{setActive(module);setData(null);setError("")}}
   projects={projects}
   projectId={projectId}
   onProjectChange={setProjectId}
   onLogout={logout}
 >
   {error&&<p className="error global-error">{error}</p>}

   <section className="panel page-panel">
     {active==="Overview"&&<div className="overview-grid">
       <div className="stat-card"><span>Signed in</span><strong>{user.display_name}</strong></div>
       <div className="stat-card"><span>Projects</span><strong>{projects.filter(p=>p.status==="active").length}</strong></div>
       <div className="stat-card"><span>Selected project</span><strong>{selectedProject?.name||"None"}</strong></div>
       <div className="stat-card"><span>Active keys</span><strong>{keys.filter(k=>k.status==="active").length}</strong></div>
     </div>}

     {active==="Projects"&&<>
       <div className="section-toolbar"><div><p className="section-copy">Create, rename or archive isolated API projects.</p></div><button className="primary" onClick={openCreateProject}>Create project</button></div>
       <div className="table-list">
         {projects.map(project=><div className={`table-row ${projectId===project.id?"selected-row":""}`} key={project.id}>
           <button className="row-main" disabled={project.status!=="active"} onClick={()=>setProjectId(project.id)}>
             <strong>{project.name}</strong><span className={`status ${project.status}`}>{project.status}</span>
           </button>
           <div className="row-actions">
             <button disabled={project.status!=="active"} onClick={()=>openRenameProject(project)}>Rename</button>
             <button className="danger-link" disabled={project.status!=="active"} onClick={()=>confirmArchive(project)}>Archive</button>
           </div>
         </div>)}
       </div>
     </>}

     {active==="API Keys"&&<>
       <div className="section-toolbar">
         <select value={projectId} onChange={e=>setProjectId(e.target.value)}>
           <option value="">Select project</option>
           {projects.filter(p=>p.status==="active").map(p=><option value={p.id} key={p.id}>{p.name}</option>)}
         </select>
         <button className="primary" disabled={!projectId} onClick={openCreateKey}>Create API key</button>
       </div>
       {!projectId?<p className="empty">Select an active project to manage its API keys.</p>:<div className="table-list">
         {keys.map(item=><div className="table-row key-row" key={item.id}>
           <div className="key-meta">
             <div><strong>{item.name}</strong><span className={`status ${item.status}`}>{item.status}</span></div>
             <code>{item.key_prefix}…</code>
             <small>RPM {item.requests_per_minute??"default"} · Daily {item.requests_per_day??"default"} · Concurrent {item.max_concurrent??"default"} · Last used {item.last_used_at?new Date(item.last_used_at).toLocaleString():"Never"}</small>
           </div>
           <div className="row-actions">
             <button disabled={item.status!=="active"} onClick={()=>openEditKey(item)}>Edit</button>
             <button disabled={item.status!=="active"} onClick={()=>confirmRotate(item)}>Rotate</button>
             <button className="danger-link" disabled={item.status!=="active"} onClick={()=>confirmRevoke(item)}>Revoke</button>
           </div>
         </div>)}
         {!keys.length&&<p className="empty">No API keys yet.</p>}
       </div>}
     </>}

     {active==="Models"&&<><label>Temporary API key<input type="password" value={key} onChange={e=>setKey(e.target.value)} placeholder="Paste a key or use the one-time secret after creation"/></label><button onClick={()=>run(()=>api.models(key))} disabled={!key}>Load free models</button></>}
     {active==="Playground"&&<><label>Temporary API key<input type="password" value={key} onChange={e=>setKey(e.target.value)} placeholder="Paste a key or use the one-time secret after creation"/></label><label>Model<input value={model} onChange={e=>setModel(e.target.value)}/></label><label>Prompt<textarea value={chatPrompt} onChange={e=>setChatPrompt(e.target.value)}/></label><button disabled={busy||!key} onClick={()=>run(()=>api.chat(key,model,chatPrompt))}>Send request</button></>}
     {active==="Usage"&&<button onClick={()=>run(()=>api.usage())}>Load usage</button>}
     {active==="Requests"&&<button onClick={()=>run(()=>api.requests())}>Load requests</button>}
     {active==="Settings"&&<button onClick={()=>run(()=>api.settings())}>Load settings</button>}
     {active==="Status"&&<button onClick={()=>run(async()=>({control:await api.controlHealth(),gateway:await api.gatewayHealth()}))}>Check services</button>}
     {busy&&<p>Loading…</p>}{data&&<pre className="output">{JSON.stringify(data,null,2)}</pre>}
   </section>

   {projectDialog&&<Dialog title={projectDialog.mode==="create"?"Create project":"Rename project"} onClose={()=>setProjectDialog(null)}>
     <form className="dialog-form" onSubmit={submitProject}>
       <label>Project name<input autoFocus value={projectName} onChange={e=>setProjectName(e.target.value)} maxLength={120} required/></label>
       <div className="dialog-actions"><button type="button" onClick={()=>setProjectDialog(null)}>Cancel</button><button className="primary" disabled={busy} type="submit">Save</button></div>
     </form>
   </Dialog>}

   {keyDialog&&<Dialog title={keyDialog.mode==="create"?"Create API key":"Edit API key"} onClose={()=>setKeyDialog(null)}>
     <form className="dialog-form" onSubmit={submitKey}>
       <label>Key name<input autoFocus value={keyName} onChange={e=>setKeyName(e.target.value)} maxLength={120} required/></label>
       {keyDialog.mode==="edit"&&<div className="form-grid">
         <label>Requests / minute<input type="number" min="1" value={rpm} onChange={e=>setRpm(e.target.value)} placeholder="Default"/></label>
         <label>Requests / day<input type="number" min="1" value={daily} onChange={e=>setDaily(e.target.value)} placeholder="Default"/></label>
         <label>Max concurrent<input type="number" min="1" value={concurrent} onChange={e=>setConcurrent(e.target.value)} placeholder="Default"/></label>
       </div>}
       <div className="dialog-actions"><button type="button" onClick={()=>setKeyDialog(null)}>Cancel</button><button className="primary" disabled={busy} type="submit">Save</button></div>
     </form>
   </Dialog>}

   {secretDialog&&<Dialog title={secretDialog.label} onClose={()=>setSecretDialog(null)}>
     <div className="secret-box"><p>This secret is displayed once. Store it securely now.</p><code>{secretDialog.secret}</code></div>
     <div className="dialog-actions">
       <button onClick={()=>navigator.clipboard.writeText(secretDialog.secret)}>Copy</button>
       <button onClick={()=>{setKey(secretDialog.secret);setSecretDialog(null);setActive("Playground")}}>Use in Playground</button>
       <button className="primary" onClick={()=>setSecretDialog(null)}>Done</button>
     </div>
   </Dialog>}

   {confirmDialog&&<Dialog title={confirmDialog.title} onClose={()=>setConfirmDialog(null)}>
     <p>{confirmDialog.message}</p>
     <div className="dialog-actions"><button onClick={()=>setConfirmDialog(null)}>Cancel</button><button className="danger" disabled={busy} onClick={runConfirmedAction}>Confirm</button></div>
   </Dialog>}
 </AppShell>;
}

createRoot(document.getElementById("root")!).render(<React.StrictMode><App/></React.StrictMode>);
