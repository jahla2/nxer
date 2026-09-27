export type User={
  id:string;
  email:string;
  display_name:string;
  role:string;
  email_verified:boolean;
};

export type Project={id:string;name:string;status:string;created_at:string};
export type ApiKey={
  id:string;
  project_id:string;
  name:string;
  key_prefix:string;
  status:string;
  created_at:string;
  last_used_at?:string|null;
  expires_at?:string|null;
  revoked_at?:string|null;
};

const CONTROL="/api";
const GATEWAY="/v1";

function csrfToken():string{
  const entry=document.cookie.split("; ").find(value=>value.startsWith("nexora_csrf="));
  return entry ? decodeURIComponent(entry.split("=",2)[1]||"") : "";
}

async function rawJson<T>(url:string,init:RequestInit={},retryAuth=true):Promise<T>{
  const response=await fetch(url,{credentials:"same-origin",...init});
  if(response.status===401 && retryAuth && !url.includes("/auth/")){
    const refreshed=await fetch(`${CONTROL}/auth/refresh`,{method:"POST",credentials:"same-origin"});
    if(refreshed.ok) return rawJson<T>(url,init,false);
  }
  const body=await response.json().catch(()=>({}));
  if(!response.ok) throw new Error(body?.detail||body?.error?.message||`Request failed (${response.status})`);
  return body as T;
}

function mutationHeaders(extra:Record<string,string>={}):Record<string,string>{
  return {"Content-Type":"application/json","X-CSRF-Token":csrfToken(),...extra};
}

export const api={
  register:(display_name:string,email:string,password:string)=>rawJson<User>(`${CONTROL}/auth/register`,{
    method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({display_name,email,password})
  },false),
  login:(email:string,password:string)=>rawJson<User>(`${CONTROL}/auth/login`,{
    method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({email,password})
  },false),
  me:()=>rawJson<User>(`${CONTROL}/auth/me`),
  refresh:()=>rawJson<User>(`${CONTROL}/auth/refresh`,{method:"POST"},false),
  logout:()=>fetch(`${CONTROL}/auth/logout`,{
    method:"POST",credentials:"same-origin",headers:{"X-CSRF-Token":csrfToken()}
  }).then(async response=>{
    if(!response.ok){
      const body=await response.json().catch(()=>({}));
      throw new Error(body?.detail||`Request failed (${response.status})`);
    }
  }),

  controlHealth:()=>rawJson<{status:string}>(`${CONTROL}/health`),
  gatewayHealth:()=>rawJson<{status:string}>("/health"),

  projects:()=>rawJson<Project[]>(`${CONTROL}/projects`),
  createProject:(name:string)=>rawJson<Project>(`${CONTROL}/projects`,{
    method:"POST",headers:mutationHeaders(),body:JSON.stringify({name})
  }),

  keys:(projectId:string)=>rawJson<ApiKey[]>(`${CONTROL}/api-keys?project_id=${encodeURIComponent(projectId)}`),
  createKey:(projectId:string,name:string)=>rawJson<ApiKey&{api_key:string}>(`${CONTROL}/api-keys`,{
    method:"POST",headers:mutationHeaders(),body:JSON.stringify({project_id:projectId,name,allow_all_free_models:true})
  }),
  revokeKey:(id:string)=>rawJson<ApiKey>(`${CONTROL}/api-keys/${id}/revoke`,{
    method:"POST",headers:mutationHeaders()
  }),

  usage:()=>rawJson<any[]>(`${CONTROL}/usage`),
  requests:()=>rawJson<any[]>(`${CONTROL}/requests`),
  settings:()=>rawJson<any>(`${CONTROL}/settings`),

  models:(key:string)=>rawJson<{data:any[]}>(`${GATEWAY}/models`,{
    headers:{Authorization:`Bearer ${key}`}
  },false),
  chat:(key:string,model:string,prompt:string)=>rawJson<any>(`${GATEWAY}/chat/completions`,{
    method:"POST",
    headers:{
      Authorization:`Bearer ${key}`,
      "Content-Type":"application/json",
      "Idempotency-Key":crypto.randomUUID()
    },
    body:JSON.stringify({model,messages:[{role:"user",content:prompt}],stream:false})
  },false)
};
