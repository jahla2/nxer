import React,{useEffect,useState} from "react";
import {createRoot} from "react-dom/client";
import {api,Project,ApiKey} from "./api";
import "./styles.css";

const modules=["Overview","Projects","API Keys","Models","Playground","Usage","Requests","Settings","Status"];
function App(){
 const [active,setActive]=useState("Overview"),[admin,setAdmin]=useState(localStorage.getItem("nexora_admin")||"");
 const [key,setKey]=useState(localStorage.getItem("nexora_key")||""),[projects,setProjects]=useState<Project[]>([]);
 const [projectId,setProjectId]=useState(""),[keys,setKeys]=useState<ApiKey[]>([]),[data,setData]=useState<any>(null),[error,setError]=useState("");
 const [prompt,setPrompt]=useState("Say hello from Nexora"),[model,setModel]=useState("auto-free"),[busy,setBusy]=useState(false);
 const save=(name:string,value:string,setter:(v:string)=>void)=>{localStorage.setItem(name,value);setter(value)};
 async function run(fn:()=>Promise<any>){setBusy(true);setError("");try{setData(await fn())}catch(e){setError(e instanceof Error?e.message:String(e))}finally{setBusy(false)}}
 async function loadProjects(){if(!admin)return;try{const p=await api.projects(admin);setProjects(p);if(p.length&&!projectId)setProjectId(p[0].id)}catch(e){setError(e instanceof Error?e.message:String(e))}}
 useEffect(()=>{loadProjects()},[admin]);
 useEffect(()=>{if(admin&&projectId)api.keys(admin,projectId).then(setKeys).catch(()=>setKeys([]))},[admin,projectId]);
 return <main className="shell"><header><div><p className="eyebrow">NEXORA AI</p><h1>Local Developer Console</h1><p className="subtitle">React → Nginx → FastAPI / Go Gateway → PostgreSQL, Redis and OpenRouter.</p></div><span className="badge">0.1.0-alpha</span></header>
 <section className="credentials"><label>Local admin token<input type="password" value={admin} onChange={e=>save("nexora_admin",e.target.value,setAdmin)} placeholder="CONTROL_ADMIN_TOKEN"/></label><label>Nexora API key<input type="password" value={key} onChange={e=>save("nexora_key",e.target.value,setKey)} placeholder="nxa_live_..."/></label></section>
 <nav>{modules.map(x=><button key={x} className={active===x?"active":""} onClick={()=>{setActive(x);setData(null);setError("")}}>{x}</button>)}</nav>
 <section className="panel"><h2>{active}</h2>
 {active==="Overview"&&<p>Use Projects to create a local project, API Keys to issue a key, Models to verify free models, then Playground to send a real completion.</p>}
 {active==="Projects"&&<><button onClick={()=>{const name=prompt("Project name","Local Project");if(name)run(()=>api.createProject(admin,name).then(async x=>{await loadProjects();return x}))}}>Create project</button><div className="rows">{projects.map(p=><button className={projectId===p.id?"selected":""} onClick={()=>setProjectId(p.id)} key={p.id}>{p.name}<small>{p.status}</small></button>)}</div></>}
 {active==="API Keys"&&<><select value={projectId} onChange={e=>setProjectId(e.target.value)}>{projects.map(p=><option value={p.id} key={p.id}>{p.name}</option>)}</select> <button disabled={!projectId} onClick={()=>run(()=>api.createKey(admin,projectId,"Local key").then(x=>{save("nexora_key",x.api_key,setKey);return x}))}>Create key</button><div className="rows">{keys.map(k=><div className="row" key={k.id}><code>{k.key_prefix}...</code><span>{k.status}</span><button onClick={()=>run(()=>api.revokeKey(admin,k.id))}>Revoke</button></div>)}</div></>}
 {active==="Models"&&<button onClick={()=>run(()=>api.models(key))}>Load free models</button>}
 {active==="Playground"&&<><label>Model<input value={model} onChange={e=>setModel(e.target.value)}/></label><label>Prompt<textarea value={prompt} onChange={e=>setPrompt(e.target.value)}/></label><button disabled={busy} onClick={()=>run(()=>api.chat(key,model,prompt))}>Send request</button></>}
 {active==="Usage"&&<button onClick={()=>run(()=>api.usage(admin))}>Load usage</button>}
 {active==="Requests"&&<button onClick={()=>run(()=>api.requests(admin))}>Load requests</button>}
 {active==="Settings"&&<button onClick={()=>run(()=>api.settings(admin))}>Load settings</button>}
 {active==="Status"&&<button onClick={()=>run(async()=>({control:await api.controlHealth(),gateway:await api.gatewayHealth()}))}>Check services</button>}
 {busy&&<p>Loading…</p>}{error&&<p className="error">{error}</p>}{data&&<pre className="output">{JSON.stringify(data,null,2)}</pre>}
 </section></main>
}
createRoot(document.getElementById("root")!).render(<React.StrictMode><App/></React.StrictMode>);
