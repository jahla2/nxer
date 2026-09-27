import React,{useEffect,useMemo,useState} from "react";
import {createRoot} from "react-dom/client";
import {api,Project,ApiKey,User} from "./api";
import {AppShell,ConsoleModule} from "./components/AppShell";
import {OverviewPage} from "./pages/OverviewPage";
import {ProjectsPage} from "./pages/ProjectsPage";
import {ApiKeysPage} from "./pages/ApiKeysPage";
import {ModelsPage} from "./pages/ModelsPage";
import {PlaygroundPage} from "./pages/PlaygroundPage";
import {UsagePage} from "./pages/UsagePage";
import {RequestsPage} from "./pages/RequestsPage";
import {SettingsPage} from "./pages/SettingsPage";
import {StatusPage} from "./pages/StatusPage";
import "./styles.css";

function Dialog({title,children,onClose}:{title:string;children:React.ReactNode;onClose:()=>void}){
 return <div className="dialog-backdrop" role="presentation" onMouseDown={event=>{if(event.currentTarget===event.target)onClose()}}>
   <section className="dialog" role="dialog" aria-modal="true" aria-label={title}>
     <div className="dialog-head"><h3>{title}</h3><button className="icon-button" onClick={onClose} aria-label="Close">×</button></div>
     {children}
   </section>
 </div>;
}

function App(){
 const [user,setUser]=useState<User|null|undefined>(undefined);
 const [authMode,setAuthMode]=useState<"login"|"register">("login");
 const [email,setEmail]=useState("");
 const [password,setPassword]=useState("");
 const [displayName,setDisplayName]=useState("");

 const [active,setActive]=useState<ConsoleModule>("Overview");
 const [key,setKey]=useState("");
 const [projects,setProjects]=useState<Project[]>([]);
 const [projectId,setProjectId]=useState("");
 const [keys,setKeys]=useState<ApiKey[]>([]);
 const [error,setError]=useState("");
 const [chatPrompt,setChatPrompt]=useState("Say hello from Nexora");
 const [model,setModel]=useState("auto-free");
 const [busy,setBusy]=useState(false);

 const [projectDialog,setProjectDialog]=useState<{mode:"create"|"rename";project?:Project}|null>(null);
 const [projectName,setProjectName]=useState("");
 const [keyDialog,setKeyDialog]=useState<{mode:"create"|"edit";apiKey?:ApiKey}|null>(null);
 const [keyName,setKeyName]=useState("");
 const [rpm,setRpm]=useState("");
 const [daily,setDaily]=useState("");
 const [concurrent,setConcurrent]=useState("");
 const [secretDialog,setSecretDialog]=useState<{label:string;secret:string}|null>(null);
 const [confirmDialog,setConfirmDialog]=useState<{title:string;message:string;action:()=>Promise<void>}|null>(null);

 const selectedProject=useMemo(()=>projects.find(project=>project.id===projectId)||null,[projects,projectId]);

 async function loadProjects(){
   if(!user)return;
   const nextProjects=await api.projects();
   setProjects(nextProjects);
   const activeProject=nextProjects.find(item=>item.status==="active");
   if(!nextProjects.some(item=>item.id===projectId&&item.status==="active")){
     setProjectId(activeProject?.id||"");
   }
 }

 async function loadKeys(pid=projectId){
   if(!user||!pid){setKeys([]);return}
   setKeys(await api.keys(pid));
 }

 useEffect(()=>{api.me().catch(()=>api.refresh()).then(setUser).catch(()=>setUser(null))},[]);
 useEffect(()=>{if(user)loadProjects().catch(err=>setError(err instanceof Error?err.message:String(err)))},[user]);
 useEffect(()=>{loadKeys().catch(()=>setKeys([]))},[user,projectId]);

 async function submitAuth(event:React.FormEvent){
   event.preventDefault();setBusy(true);setError("");
   try{
     const next=authMode==="register"
       ?await api.register(displayName,email,password)
       :await api.login(email,password);
     setUser(next);
     setPassword("");
     setActive("Overview");
   }catch(err){
     setError(err instanceof Error?err.message:String(err));
   }finally{
     setBusy(false);
   }
 }

 async function logout(){
   try{await api.logout()}
   finally{
     setUser(null);
     setProjects([]);
     setKeys([]);
     setProjectId("");
     setKey("");
     setError("");
     setActive("Overview");
   }
 }

 function navigate(module:ConsoleModule){
   setActive(module);
   setError("");
 }

 function openCreateProject(){
   setProjectName("");
   setProjectDialog({mode:"create"});
 }

 function openRenameProject(project:Project){
   setProjectName(project.name);
   setProjectDialog({mode:"rename",project});
 }

 async function submitProject(event:React.FormEvent){
   event.preventDefault();
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
   }catch(err){
     setError(err instanceof Error?err.message:String(err));
   }finally{
     setBusy(false);
   }
 }

 function confirmArchive(project:Project){
   setConfirmDialog({
     title:"Archive project",
     message:`Archive "${project.name}"? All active API keys in this project will be revoked.`,
     action:async()=>{
       await api.archiveProject(project.id);
       await loadProjects();
       if(project.id===projectId)setKeys([]);
     },
   });
 }

 function openCreateKey(){
   setKeyName("Development key");
   setRpm("");
   setDaily("");
   setConcurrent("");
   setKeyDialog({mode:"create"});
 }

 function openEditKey(apiKey:ApiKey){
   setKeyName(apiKey.name);
   setRpm(apiKey.requests_per_minute?.toString()||"");
   setDaily(apiKey.requests_per_day?.toString()||"");
   setConcurrent(apiKey.max_concurrent?.toString()||"");
   setKeyDialog({mode:"edit",apiKey});
 }

 async function submitKey(event:React.FormEvent){
   event.preventDefault();
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
   }catch(err){
     setError(err instanceof Error?err.message:String(err));
   }finally{
     setBusy(false);
   }
 }

 function confirmRotate(apiKey:ApiKey){
   setConfirmDialog({
     title:"Rotate API key",
     message:`Rotate "${apiKey.name}"? The current key will stop working immediately.`,
     action:async()=>{
       const rotated=await api.rotateKey(apiKey.id);
       setSecretDialog({label:`Rotated API key · ${rotated.name}`,secret:rotated.api_key});
       await loadKeys();
     },
   });
 }

 function confirmRevoke(apiKey:ApiKey){
   setConfirmDialog({
     title:"Revoke API key",
     message:`Revoke "${apiKey.name}"? This cannot be undone.`,
     action:async()=>{
       await api.revokeKey(apiKey.id);
       await loadKeys();
     },
   });
 }

 async function runConfirmedAction(){
   if(!confirmDialog)return;
   const action=confirmDialog.action;
   setConfirmDialog(null);setBusy(true);setError("");
   try{await action()}
   catch(err){setError(err instanceof Error?err.message:String(err))}
   finally{setBusy(false)}
 }

 function renderPage(){
   switch(active){
     case "Overview":
       return <OverviewPage user={user!} projects={projects} selectedProject={selectedProject} keys={keys} onNavigate={navigate}/>;
     case "Projects":
       return <ProjectsPage projects={projects} projectId={projectId} onSelect={setProjectId} onCreate={openCreateProject} onRename={openRenameProject} onArchive={confirmArchive}/>;
     case "API Keys":
       return <ApiKeysPage projects={projects} projectId={projectId} keys={keys} onProjectChange={setProjectId} onCreate={openCreateKey} onEdit={openEditKey} onRotate={confirmRotate} onRevoke={confirmRevoke}/>;
     case "Models":
       return <ModelsPage apiKey={key} onApiKeyChange={setKey} onUseModel={modelId=>{setModel(modelId);navigate("Playground")}}/>;
     case "Playground":
       return <PlaygroundPage apiKey={key} onApiKeyChange={setKey} model={model} onModelChange={setModel} prompt={chatPrompt} onPromptChange={setChatPrompt}/>;
     case "Usage":
       return <UsagePage/>;
     case "Requests":
       return <RequestsPage/>;
     case "Settings":
       return <SettingsPage/>;
     case "Status":
       return <StatusPage/>;
   }
 }

 if(user===undefined){
   return <main className="session-loading"><div className="loading-card"><span className="spinner"/>Loading session…</div></main>;
 }

 if(!user){
   return <main className="shell auth-shell">
     <section className="panel auth-panel">
       <p className="eyebrow">NEXORA AI</p>
       <h1>{authMode==="login"?"Sign in":"Create account"}</h1>
       <p className="subtitle">Access your Nexora developer console.</p>
       <form onSubmit={submitAuth}>
         {authMode==="register"&&<label>Name<input value={displayName} onChange={event=>setDisplayName(event.target.value)} minLength={2} autoComplete="name" required/></label>}
         <label>Email<input type="email" value={email} onChange={event=>setEmail(event.target.value)} autoComplete="email" required/></label>
         <label>Password<input type="password" value={password} onChange={event=>setPassword(event.target.value)} minLength={authMode==="register"?12:1} autoComplete={authMode==="register"?"new-password":"current-password"} required/></label>
         {authMode==="register"&&<p className="form-hint">Use at least 12 characters. Session credentials are stored in secure cookies.</p>}
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
   onNavigate={navigate}
   projects={projects}
   projectId={projectId}
   onProjectChange={setProjectId}
   onLogout={logout}
 >
   {error&&<p className="error global-error">{error}</p>}
   <section className="console-page">{renderPage()}</section>

   {projectDialog&&<Dialog title={projectDialog.mode==="create"?"Create project":"Rename project"} onClose={()=>setProjectDialog(null)}>
     <form className="dialog-form" onSubmit={submitProject}>
       <label>Project name<input autoFocus value={projectName} onChange={event=>setProjectName(event.target.value)} maxLength={120} required/></label>
       <div className="dialog-actions"><button type="button" onClick={()=>setProjectDialog(null)}>Cancel</button><button className="primary" disabled={busy} type="submit">{busy?"Saving…":"Save"}</button></div>
     </form>
   </Dialog>}

   {keyDialog&&<Dialog title={keyDialog.mode==="create"?"Create API key":"Edit API key"} onClose={()=>setKeyDialog(null)}>
     <form className="dialog-form" onSubmit={submitKey}>
       <label>Key name<input autoFocus value={keyName} onChange={event=>setKeyName(event.target.value)} maxLength={120} required/></label>
       {keyDialog.mode==="edit"&&<div className="form-grid">
         <label>Requests / minute<input type="number" min="1" value={rpm} onChange={event=>setRpm(event.target.value)} placeholder="Default"/></label>
         <label>Requests / day<input type="number" min="1" value={daily} onChange={event=>setDaily(event.target.value)} placeholder="Default"/></label>
         <label>Max concurrent<input type="number" min="1" value={concurrent} onChange={event=>setConcurrent(event.target.value)} placeholder="Default"/></label>
       </div>}
       <div className="dialog-actions"><button type="button" onClick={()=>setKeyDialog(null)}>Cancel</button><button className="primary" disabled={busy} type="submit">{busy?"Saving…":"Save"}</button></div>
     </form>
   </Dialog>}

   {secretDialog&&<Dialog title={secretDialog.label} onClose={()=>setSecretDialog(null)}>
     <div className="secret-box">
       <div className="secret-warning"><strong>Copy this secret now.</strong><p>For security, Nexora stores only a hash and cannot show the raw key again.</p></div>
       <code>{secretDialog.secret}</code>
     </div>
     <div className="dialog-actions">
       <button onClick={()=>navigator.clipboard.writeText(secretDialog.secret)}>Copy key</button>
       <button onClick={()=>{setKey(secretDialog.secret);setSecretDialog(null);navigate("Playground")}}>Use in Playground</button>
       <button className="primary" onClick={()=>setSecretDialog(null)}>Done</button>
     </div>
   </Dialog>}

   {confirmDialog&&<Dialog title={confirmDialog.title} onClose={()=>setConfirmDialog(null)}>
     <p className="confirm-copy">{confirmDialog.message}</p>
     <div className="dialog-actions"><button onClick={()=>setConfirmDialog(null)}>Cancel</button><button className="danger" disabled={busy} onClick={runConfirmedAction}>{busy?"Working…":"Confirm"}</button></div>
   </Dialog>}
 </AppShell>;
}

createRoot(document.getElementById("root")!).render(<React.StrictMode><App/></React.StrictMode>);
