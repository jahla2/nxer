import React,{useEffect,useState} from "react";
import {createRoot} from "react-dom/client";
import {api,Project,ApiKey,User} from "./api";
import "./styles.css";

const modules=["Overview","Projects","API Keys","Models","Playground","Usage","Requests","Settings","Status"];

function App(){
 const [user,setUser]=useState<User|null|undefined>(undefined);
 const [authMode,setAuthMode]=useState<"login"|"register">("login");
 const [email,setEmail]=useState(""),[password,setPassword]=useState(""),[displayName,setDisplayName]=useState("");
 const [active,setActive]=useState("Overview"),[key,setKey]=useState(""),[projects,setProjects]=useState<Project[]>([]);
 const [projectId,setProjectId]=useState(""),[keys,setKeys]=useState<ApiKey[]>([]),[data,setData]=useState<any>(null),[error,setError]=useState("");
 const [chatPrompt,setChatPrompt]=useState("Say hello from Nexora"),[model,setModel]=useState("auto-free"),[busy,setBusy]=useState(false);

 async function run(fn:()=>Promise<any>){
   setBusy(true);setError("");
   try{setData(await fn())}catch(e){setError(e instanceof Error?e.message:String(e))}
   finally{setBusy(false)}
 }

 async function loadProjects(){
   if(!user)return;
   try{
     const p=await api.projects();
     setProjects(p);
     if(p.length&&!projectId)setProjectId(p[0].id);
   }catch(e){setError(e instanceof Error?e.message:String(e))}
 }

 useEffect(()=>{
   api.me().then(setUser).catch(()=>setUser(null));
 },[]);

 useEffect(()=>{if(user)loadProjects()},[user]);
 useEffect(()=>{
   if(user&&projectId)api.keys(projectId).then(setKeys).catch(()=>setKeys([]));
 },[user,projectId]);

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

 return <main className="shell">
   <header>
     <div><p className="eyebrow">NEXORA AI</p><h1>Developer Console</h1><p className="subtitle">React → Nginx → FastAPI / Go Gateway → PostgreSQL, Redis and OpenRouter.</p></div>
     <div className="user-summary"><div><strong>{user.display_name}</strong><small>{user.email}</small></div><button onClick={logout}>Logout</button></div>
   </header>
   <section className="credentials"><label>Playground API key<input type="password" value={key} onChange={e=>setKey(e.target.value)} placeholder="Shown once after key creation or paste an existing key"/></label></section>
   <nav>{modules.map(x=><button key={x} className={active===x?"active":""} onClick={()=>{setActive(x);setData(null);setError("")}}>{x}</button>)}</nav>
   <section className="panel"><h2>{active}</h2>
     {active==="Overview"&&<p>Signed in as {user.display_name}. Create a project or use your default project, issue an API key, then test a free model.</p>}
     {active==="Projects"&&<><button onClick={()=>{const name=prompt("Project name","New Project");if(name)run(()=>api.createProject(name).then(async x=>{await loadProjects();return x}))}}>Create project</button><div className="rows">{projects.map(p=><button className={projectId===p.id?"selected":""} onClick={()=>setProjectId(p.id)} key={p.id}>{p.name}<small>{p.status}</small></button>)}</div></>}
     {active==="API Keys"&&<><select value={projectId} onChange={e=>setProjectId(e.target.value)}>{projects.map(p=><option value={p.id} key={p.id}>{p.name}</option>)}</select> <button disabled={!projectId} onClick={()=>run(()=>api.createKey(projectId,"Local key").then(x=>{setKey(x.api_key);return x}))}>Create key</button><div className="rows">{keys.map(k=><div className="row" key={k.id}><code>{k.key_prefix}...</code><span>{k.status}</span><button onClick={()=>run(()=>api.revokeKey(k.id).then(async x=>{if(projectId)setKeys(await api.keys(projectId));return x}))}>Revoke</button></div>)}</div></>}
     {active==="Models"&&<button onClick={()=>run(()=>api.models(key))}>Load free models</button>}
     {active==="Playground"&&<><label>Model<input value={model} onChange={e=>setModel(e.target.value)}/></label><label>Prompt<textarea value={chatPrompt} onChange={e=>setChatPrompt(e.target.value)}/></label><button disabled={busy||!key} onClick={()=>run(()=>api.chat(key,model,chatPrompt))}>Send request</button></>}
     {active==="Usage"&&<button onClick={()=>run(()=>api.usage())}>Load usage</button>}
     {active==="Requests"&&<button onClick={()=>run(()=>api.requests())}>Load requests</button>}
     {active==="Settings"&&<button onClick={()=>run(()=>api.settings())}>Load settings</button>}
     {active==="Status"&&<button onClick={()=>run(async()=>({control:await api.controlHealth(),gateway:await api.gatewayHealth()}))}>Check services</button>}
     {busy&&<p>Loading…</p>}{error&&<p className="error">{error}</p>}{data&&<pre className="output">{JSON.stringify(data,null,2)}</pre>}
   </section>
 </main>;
}

createRoot(document.getElementById("root")!).render(<React.StrictMode><App/></React.StrictMode>);
