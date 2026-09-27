export type Project={id:string;name:string;status:string;created_at:string};
export type ApiKey={id:string;project_id:string;name:string;key_prefix:string;status:string;created_at:string};
const CONTROL="/api";
const GATEWAY="/v1";

async function json<T>(url:string,init:RequestInit={}):Promise<T>{
  const response=await fetch(url,init);
  const body=await response.json().catch(()=>({}));
  if(!response.ok) throw new Error(body?.detail||body?.error?.message||`Request failed (${response.status})`);
  return body as T;
}
const adminHeaders=(token:string)=>({"Authorization":`Bearer ${token}`,"Content-Type":"application/json"});

export const api={
  controlHealth:()=>json<{status:string}>("/api/health"),
  gatewayHealth:()=>json<{status:string}>("/health"),
  projects:(token:string)=>json<Project[]>(`${CONTROL}/projects`,{headers:adminHeaders(token)}),
  createProject:(token:string,name:string)=>json<Project>(`${CONTROL}/projects`,{method:"POST",headers:adminHeaders(token),body:JSON.stringify({name})}),
  keys:(token:string,projectId:string)=>json<ApiKey[]>(`${CONTROL}/api-keys?project_id=${encodeURIComponent(projectId)}`,{headers:adminHeaders(token)}),
  createKey:(token:string,projectId:string,name:string)=>json<ApiKey&{api_key:string}>(`${CONTROL}/api-keys`,{method:"POST",headers:adminHeaders(token),body:JSON.stringify({project_id:projectId,name,allow_all_free_models:true})}),
  revokeKey:(token:string,id:string)=>json<ApiKey>(`${CONTROL}/api-keys/${id}/revoke`,{method:"POST",headers:adminHeaders(token)}),
  usage:(token:string)=>json<any[]>(`${CONTROL}/usage`,{headers:adminHeaders(token)}),
  requests:(token:string)=>json<any[]>(`${CONTROL}/requests`,{headers:adminHeaders(token)}),
  settings:(token:string)=>json<any[]>(`${CONTROL}/settings`,{headers:adminHeaders(token)}),
  models:(key:string)=>json<{data:any[]}>(`${GATEWAY}/models`,{headers:{Authorization:`Bearer ${key}`}}),
  chat:(key:string,model:string,prompt:string)=>json<any>(`${GATEWAY}/chat/completions`,{method:"POST",headers:{Authorization:`Bearer ${key}`,"Content-Type":"application/json","Idempotency-Key":crypto.randomUUID()},body:JSON.stringify({model,messages:[{role:"user",content:prompt}],stream:false})})
};
