import React,{useCallback,useEffect,useMemo,useState} from "react";
import {Navigate,Route,Routes,useLocation,useNavigate} from "react-router-dom";
import {api,ApiKey,ApiKeyUpdateInput,ConsoleModel,Project,User} from "./api";
import {ApiKeyPolicyForm} from "./components/ApiKeyPolicyForm";
import {AppShell} from "./components/AppShell";
import {AuthScreen} from "./components/AuthScreen";
import {Dialog} from "./components/Dialog";
import {useSession} from "./providers/SessionProvider";
import {useToast} from "./providers/ToastProvider";
import {AUTH_PATHS,AuthRouteMode,ConsoleModule,moduleForPath,pathForModule} from "./routing";
import {AdminPage} from "./pages/AdminPage";
import {ApiKeysPage} from "./pages/ApiKeysPage";
import {DocsPage} from "./pages/DocsPage";
import {ModelsPage} from "./pages/ModelsPage";
import {OverviewPage} from "./pages/OverviewPage";
import {PlaygroundPage} from "./pages/PlaygroundPage";
import {ProjectsPage} from "./pages/ProjectsPage";
import {RequestsPage} from "./pages/RequestsPage";
import {SettingsPage} from "./pages/SettingsPage";
import {StatusPage} from "./pages/StatusPage";
import {UsagePage} from "./pages/UsagePage";

function SessionLoading(){
  return <main className="session-loading" id="main-content">
    <div className="loading-card" role="status"><span className="spinner"/>Loading session…</div>
  </main>;
}

function LegacyLandingRedirect(){
  const {user}=useSession();
  const location=useLocation();
  const params=new URLSearchParams(location.search);
  const verification=params.get("verify_email");
  const reset=params.get("reset_password");

  if(verification){
    return <Navigate to={AUTH_PATHS.verify+"?token="+encodeURIComponent(verification)} replace/>;
  }
  if(reset){
    return <Navigate to={AUTH_PATHS.reset+"?token="+encodeURIComponent(reset)} replace/>;
  }
  return <Navigate to={user?"/overview":AUTH_PATHS.login} replace/>;
}

function AuthRoute({mode}:{mode:AuthRouteMode}){
  const {user,setUser}=useSession();
  const navigate=useNavigate();
  const location=useLocation();
  const params=new URLSearchParams(location.search);
  const token=params.get("token")||"";

  if(user&&(mode==="login"||mode==="register")){
    return <Navigate to="/overview" replace/>;
  }

  function navigateAuth(next:AuthRouteMode,nextToken?:string){
    const query=nextToken?"?token="+encodeURIComponent(nextToken):"";
    navigate(AUTH_PATHS[next]+query);
  }

  return <AuthScreen
    mode={mode}
    sessionUser={user}
    resetToken={mode==="reset"?token:""}
    verifyToken={mode==="verify"?token:""}
    onAuthenticated={next=>{
      setUser(next);
      if(mode!=="verify")navigate("/overview",{replace:true});
    }}
    onPasswordResetComplete={()=>{
      setUser(null);
    }}
    onNavigate={navigateAuth}
    onActionComplete={()=>{
      navigate(user?"/settings":AUTH_PATHS.login,{replace:true});
    }}
  />;
}

function AuthenticatedConsole({user}:{user:User}){
  const navigate=useNavigate();
  const location=useLocation();
  const {setUser,logout}=useSession();
  const toast=useToast();

  const [key,setKey]=useState("");
  const [projects,setProjects]=useState<Project[]>([]);
  const [projectId,setProjectId]=useState("");
  const [keys,setKeys]=useState<ApiKey[]>([]);
  const [catalogModels,setCatalogModels]=useState<ConsoleModel[]>([]);
  const [error,setError]=useState("");
  const [model,setModel]=useState("auto-free");
  const [busy,setBusy]=useState(false);

  const [projectDialog,setProjectDialog]=useState<{mode:"create"|"rename";project?:Project}|null>(null);
  const [projectName,setProjectName]=useState("");
  const [keyDialog,setKeyDialog]=useState<{mode:"create"|"edit";apiKey?:ApiKey}|null>(null);
  const [secretDialog,setSecretDialog]=useState<{label:string;secret:string}|null>(null);
  const [confirmDialog,setConfirmDialog]=useState<{title:string;message:string;action:()=>Promise<void>}|null>(null);

  const active=moduleForPath(location.pathname);
  const selectedProject=useMemo(()=>projects.find(project=>project.id===projectId)||null,[projects,projectId]);

  const reportError=useCallback((err:unknown,title="Request failed")=>{
    const message=err instanceof Error?err.message:String(err);
    setError(message);
    toast.error(title,message);
  },[toast]);

  const loadProjects=useCallback(async()=>{
    const nextProjects=await api.projects();
    setProjects(nextProjects);
    setProjectId(current=>{
      if(nextProjects.some(item=>item.id===current&&item.status==="active"))return current;
      return nextProjects.find(item=>item.status==="active")?.id||"";
    });
    return nextProjects;
  },[]);

  const loadCatalogModels=useCallback(async()=>{
    const models=await api.catalogModels();
    setCatalogModels(models);
    return models;
  },[]);

  const loadKeys=useCallback(async(pid:string)=>{
    if(!pid){
      setKeys([]);
      return [];
    }
    const rows=await api.keys(pid);
    setKeys(rows);
    return rows;
  },[]);

  useEffect(()=>{
    let cancelled=false;
    Promise.all([api.projects(),api.catalogModels()])
      .then(([nextProjects,models])=>{
        if(cancelled)return;
        setProjects(nextProjects);
        setCatalogModels(models);
        setProjectId(current=>{
          if(nextProjects.some(item=>item.id===current&&item.status==="active"))return current;
          return nextProjects.find(item=>item.status==="active")?.id||"";
        });
      })
      .catch(err=>{if(!cancelled)reportError(err,"Console data failed to load")});
    return()=>{cancelled=true};
  },[user.id,reportError]);

  useEffect(()=>{
    let cancelled=false;
    if(!projectId){
      setKeys([]);
      return()=>{cancelled=true};
    }
    api.keys(projectId)
      .then(rows=>{if(!cancelled)setKeys(rows)})
      .catch(err=>{if(!cancelled)reportError(err,"API keys failed to load")});
    return()=>{cancelled=true};
  },[projectId,reportError]);

  function navigateModule(module:ConsoleModule){
    setError("");
    navigate(pathForModule(module));
  }

  async function signOut(){
    try{
      await logout();
      toast.info("Signed out","Your Nexora session has ended.");
    }catch(err){
      toast.error("Sign-out request failed","The local session was still cleared.");
      console.error(err);
    }finally{
      navigate(AUTH_PATHS.login,{replace:true});
    }
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
        toast.success("Project created",created.name);
      }else if(projectDialog.project){
        const updated=await api.renameProject(projectDialog.project.id,projectName);
        await loadProjects();
        toast.success("Project renamed",updated.name);
      }
      setProjectDialog(null);
    }catch(err){
      reportError(err,"Project update failed");
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
        toast.success("Project archived",project.name);
      },
    });
  }

  function refreshCatalogForPolicy(){
    loadCatalogModels().catch(err=>reportError(err,"Model catalog failed to load"));
  }

  function openCreateKey(){
    refreshCatalogForPolicy();
    setKeyDialog({mode:"create"});
  }

  function openEditKey(apiKey:ApiKey){
    refreshCatalogForPolicy();
    setKeyDialog({mode:"edit",apiKey});
  }

  async function saveKeyPolicy(payload:ApiKeyUpdateInput){
    if(!keyDialog||!projectId)return;
    setBusy(true);setError("");
    try{
      if(keyDialog.mode==="create"){
        const created=await api.createKey({project_id:projectId,...payload});
        setSecretDialog({label:`New API key · ${created.name}`,secret:created.api_key});
        toast.success("API key created",created.name);
      }else if(keyDialog.apiKey){
        const updated=await api.updateKey(keyDialog.apiKey.id,payload);
        toast.success("API key policy updated",updated.name);
      }
      await loadKeys(projectId);
      setKeyDialog(null);
    }catch(err){
      reportError(err,"API key update failed");
      throw err;
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
        await loadKeys(projectId);
        toast.success("API key rotated",rotated.name);
      },
    });
  }

  function confirmRevoke(apiKey:ApiKey){
    setConfirmDialog({
      title:"Revoke API key",
      message:`Revoke "${apiKey.name}"? This cannot be undone.`,
      action:async()=>{
        await api.revokeKey(apiKey.id);
        await loadKeys(projectId);
        toast.success("API key revoked",apiKey.name);
      },
    });
  }

  async function runConfirmedAction(){
    if(!confirmDialog)return;
    const action=confirmDialog.action;
    setConfirmDialog(null);setBusy(true);setError("");
    try{
      await action();
    }catch(err){
      reportError(err,"Operation failed");
    }finally{
      setBusy(false);
    }
  }

  async function copySecret(){
    if(!secretDialog)return;
    try{
      await navigator.clipboard.writeText(secretDialog.secret);
      toast.success("API key copied","Store it securely. Nexora cannot show this raw key again.");
    }catch{
      toast.error("Copy failed","Select the API key manually and copy it.");
    }
  }

  return <AppShell
    user={user}
    active={active}
    onNavigate={navigateModule}
    projects={projects}
    projectId={projectId}
    onProjectChange={setProjectId}
    onLogout={signOut}
  >
    {error&&<p className="error global-error" role="alert">{error}</p>}

    <section className="console-page">
      <Routes>
        <Route path="/overview" element={<OverviewPage user={user} projects={projects} selectedProject={selectedProject} keys={keys} onNavigate={navigateModule}/>}/>
        <Route path="/projects" element={<ProjectsPage projects={projects} projectId={projectId} onSelect={setProjectId} onCreate={openCreateProject} onRename={openRenameProject} onArchive={confirmArchive}/>}/>
        <Route path="/api-keys" element={<ApiKeysPage projects={projects} projectId={projectId} keys={keys} models={catalogModels} onProjectChange={setProjectId} onCreate={openCreateKey} onEdit={openEditKey} onRotate={confirmRotate} onRevoke={confirmRevoke}/>}/>
        <Route path="/models" element={<ModelsPage apiKey={key} onApiKeyChange={setKey} onUseModel={modelId=>{setModel(modelId);navigateModule("Playground")}}/>}/>
        <Route path="/playground" element={<PlaygroundPage projectId={projectId} models={catalogModels} initialModelPublicId={model}/>}/>
        <Route path="/usage" element={<UsagePage/>}/>
        <Route path="/requests" element={<RequestsPage/>}/>
        <Route path="/status" element={<StatusPage/>}/>
        <Route path="/docs" element={<DocsPage/>}/>
        <Route path="/settings" element={<SettingsPage onUserUpdated={setUser}/>}/>
        <Route path="/admin" element={user.role==="admin"?<AdminPage/>:<Navigate to="/status" replace/>}/>
        <Route path="*" element={<Navigate to="/overview" replace/>}/>
      </Routes>
    </section>

    {projectDialog&&<Dialog title={projectDialog.mode==="create"?"Create project":"Rename project"} onClose={()=>setProjectDialog(null)}>
      <form className="dialog-form" onSubmit={submitProject}>
        <label>Project name<input data-autofocus value={projectName} onChange={event=>setProjectName(event.target.value)} maxLength={120} required/></label>
        <div className="dialog-actions"><button type="button" onClick={()=>setProjectDialog(null)}>Cancel</button><button className="primary" disabled={busy} type="submit">{busy?"Saving…":"Save"}</button></div>
      </form>
    </Dialog>}

    {keyDialog&&<Dialog title={keyDialog.mode==="create"?"Create API key":"API key policy"} onClose={()=>setKeyDialog(null)} wide>
      <ApiKeyPolicyForm
        apiKey={keyDialog.apiKey}
        models={catalogModels}
        busy={busy}
        onCancel={()=>setKeyDialog(null)}
        onSave={saveKeyPolicy}
      />
    </Dialog>}

    {secretDialog&&<Dialog title={secretDialog.label} onClose={()=>setSecretDialog(null)}>
      <div className="secret-box">
        <div className="secret-warning"><strong>Copy this secret now.</strong><p>For security, Nexora stores only a hash and cannot show the raw key again.</p></div>
        <code>{secretDialog.secret}</code>
      </div>
      <div className="dialog-actions">
        <button data-autofocus onClick={copySecret}>Copy key</button>
        <button onClick={()=>{setSecretDialog(null);navigateModule("Playground")}}>Open Playground</button>
        <button className="primary" onClick={()=>setSecretDialog(null)}>Done</button>
      </div>
    </Dialog>}

    {confirmDialog&&<Dialog title={confirmDialog.title} onClose={()=>setConfirmDialog(null)}>
      <p className="confirm-copy">{confirmDialog.message}</p>
      <div className="dialog-actions"><button data-autofocus onClick={()=>setConfirmDialog(null)}>Cancel</button><button className="danger" disabled={busy} onClick={runConfirmedAction}>{busy?"Working…":"Confirm"}</button></div>
    </Dialog>}
  </AppShell>;
}

function ProtectedConsole(){
  const {user}=useSession();
  if(!user)return <Navigate to={AUTH_PATHS.login} replace/>;
  return <AuthenticatedConsole user={user}/>;
}

export function App(){
  const {loading}=useSession();

  if(loading)return <SessionLoading/>;

  return <Routes>
    <Route path="/" element={<LegacyLandingRedirect/>}/>
    <Route path="/login" element={<AuthRoute mode="login"/>}/>
    <Route path="/register" element={<AuthRoute mode="register"/>}/>
    <Route path="/forgot-password" element={<AuthRoute mode="forgot"/>}/>
    <Route path="/reset-password" element={<AuthRoute mode="reset"/>}/>
    <Route path="/verify-email" element={<AuthRoute mode="verify"/>}/>
    <Route path="*" element={<ProtectedConsole/>}/>
  </Routes>;
}
