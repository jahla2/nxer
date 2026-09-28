export type User={
  id:string;
  email:string;
  display_name:string;
  role:string;
  email_verified:boolean;
};

export type Project={
  id:string;
  name:string;
  status:string;
  created_at:string;
  updated_at:string;
};

export type ApiKey={
  id:string;
  project_id:string;
  name:string;
  key_prefix:string;
  status:string;
  default_model_id:string|null;
  model_ids:string[];
  allow_all_free_models:boolean;
  requests_per_minute:number|null;
  requests_per_day:number|null;
  max_concurrent:number|null;
  created_at:string;
  updated_at:string;
  last_used_at:string|null;
  expires_at:string|null;
  revoked_at:string|null;
  rotated_from_id:string|null;
};

export type ApiKeyCreated=ApiKey&{api_key:string};

export type ApiKeyCreateInput={
  project_id:string;
  name:string;
  allow_all_free_models:boolean;
  default_model_id:string|null;
  model_ids:string[];
  requests_per_minute:number|null;
  requests_per_day:number|null;
  max_concurrent:number|null;
  expires_at:string|null;
};

export type ApiKeyUpdateInput=Omit<ApiKeyCreateInput,"project_id">;

export type ConsoleModel={
  id:string;
  public_id:string;
  display_name:string;
  context_length:number|null;
  capabilities:Record<string,boolean>;
};

export type NexoraModel={
  id:string;
  object:string;
  owned_by:string;
  display_name?:string;
  context_length?:number;
  capabilities?:Record<string,boolean>;
  status?:string;
  free:boolean;
};

export type ModelsResponse={
  object?:string;
  data:NexoraModel[];
};

export type UsageDaily={
  usage_date:string;
  api_key_id:string;
  model_id:string|null;
  requests:number;
  prompt_tokens:number;
  completion_tokens:number;
};

export type RequestEvent={
  request_id:string;
  api_key_id:string;
  model_id:string|null;
  status:number;
  ttft_ms:number|null;
  latency_ms:number|null;
  prompt_tokens:number|null;
  completion_tokens:number|null;
  created_at:string;
};

export type ConsoleSettings={
  profile:{
    id:string;
    email:string;
    display_name:string;
    role:string;
    email_verified:boolean;
  };
  session:{
    access_ttl_minutes:number;
    refresh_ttl_days:number;
  };
};

export type HealthResponse={
  status:string;
  service?:string;
};

export type CatalogStatus={
  status:string;
  last_attempt_at:string|null;
  last_success_at:string|null;
  model_count:number;
  active_model_count:number;
  stale:boolean;
  stale_after_minutes:number;
  cache_available:boolean;
  cache_ttl_seconds:number|null;
};

export type BackgroundJobStatus={
  job_name:string;
  status:string;
  started_at:string|null;
  completed_at:string|null;
  duration_ms:number|null;
  error_message:string|null;
};

export type OperationsStatus={
  status:string;
  catalog:CatalogStatus;
  jobs:BackgroundJobStatus[];
};

export type AdminJobRun={
  id:string;
  job_name:string;
  task_id:string|null;
  status:string;
  started_at:string;
  completed_at:string|null;
  duration_ms:number|null;
  result:Record<string,unknown>;
  error_message:string|null;
  worker_name:string|null;
};

export type AuditLogEntry={
  id:string;
  actor_user_id:string|null;
  action:string;
  resource_type:string;
  resource_id:string|null;
  metadata:Record<string,unknown>;
  created_at:string;
};

export type PasswordResetRequested={
  message:string;
  reset_token?:string|null;
};

export type EmailVerificationRequested={
  message:string;
  verification_token?:string|null;
};

export type ChatCompletion={
  id:string;
  object?:string;
  created?:number;
  model:string;
  choices:Array<{
    index?:number;
    finish_reason?:string|null;
    message?:{
      role?:string;
      content?:string;
    };
  }>;
  usage?:{
    prompt_tokens?:number;
    completion_tokens?:number;
    total_tokens?:number;
  };
};

export type PlaygroundSession={
  id:string;
  project_id:string;
  title:string;
  selected_model_id:string|null;
  selected_model_public_id:string|null;
  selected_model_display_name:string|null;
  preview:string|null;
  created_at:string;
  updated_at:string;
};

export type PlaygroundMessage={
  id:string;
  session_id:string;
  role:"user"|"assistant";
  content:string;
  model_id:string|null;
  model_public_id:string|null;
  model_display_name:string|null;
  request_id:string|null;
  status:number|null;
  ttft_ms:number|null;
  latency_ms:number|null;
  prompt_tokens:number|null;
  completion_tokens:number|null;
  created_at:string;
};

export type PlaygroundConversation={
  session:PlaygroundSession;
  messages:PlaygroundMessage[];
};

export type PlaygroundTurn={
  session:PlaygroundSession;
  user_message:PlaygroundMessage;
  assistant_message:PlaygroundMessage;
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
  requestPasswordReset:(email:string)=>rawJson<PasswordResetRequested>(`${CONTROL}/auth/password-reset/request`,{
    method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({email})
  },false),
  confirmPasswordReset:(token:string,new_password:string)=>rawJson<void>(`${CONTROL}/auth/password-reset/confirm`,{
    method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({token,new_password})
  },false),
  requestEmailVerification:()=>rawJson<EmailVerificationRequested>(`${CONTROL}/auth/email-verification/request`,{
    method:"POST",headers:mutationHeaders()
  }),
  confirmEmailVerification:(token:string)=>rawJson<User>(`${CONTROL}/auth/email-verification/confirm`,{
    method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({token})
  },false),

  controlHealth:()=>rawJson<HealthResponse>(`${CONTROL}/health`),
  gatewayHealth:()=>rawJson<HealthResponse>("/health"),
  operationsStatus:()=>rawJson<OperationsStatus>(`${CONTROL}/operations/status`),
  adminJobs:(limit=100)=>rawJson<AdminJobRun[]>(`${CONTROL}/admin/jobs?limit=${encodeURIComponent(String(limit))}`),
  adminAudit:(limit=100)=>rawJson<AuditLogEntry[]>(`${CONTROL}/admin/audit?limit=${encodeURIComponent(String(limit))}`),

  projects:()=>rawJson<Project[]>(`${CONTROL}/projects`),
  createProject:(name:string)=>rawJson<Project>(`${CONTROL}/projects`,{
    method:"POST",headers:mutationHeaders(),body:JSON.stringify({name})
  }),
  renameProject:(id:string,name:string)=>rawJson<Project>(`${CONTROL}/projects/${id}`,{
    method:"PATCH",headers:mutationHeaders(),body:JSON.stringify({name})
  }),
  archiveProject:(id:string)=>rawJson<Project>(`${CONTROL}/projects/${id}/archive`,{
    method:"POST",headers:mutationHeaders()
  }),

  catalogModels:()=>rawJson<ConsoleModel[]>(`${CONTROL}/catalog/models`),

  keys:(projectId:string)=>rawJson<ApiKey[]>(`${CONTROL}/api-keys?project_id=${encodeURIComponent(projectId)}`),
  createKey:(payload:ApiKeyCreateInput)=>rawJson<ApiKeyCreated>(`${CONTROL}/api-keys`,{
    method:"POST",headers:mutationHeaders(),body:JSON.stringify(payload)
  }),
  updateKey:(id:string,payload:Partial<ApiKeyUpdateInput>)=>rawJson<ApiKey>(`${CONTROL}/api-keys/${id}`,{
    method:"PATCH",headers:mutationHeaders(),body:JSON.stringify(payload)
  }),
  rotateKey:(id:string)=>rawJson<ApiKeyCreated>(`${CONTROL}/api-keys/${id}/rotate`,{
    method:"POST",headers:mutationHeaders()
  }),
  revokeKey:(id:string)=>rawJson<ApiKey>(`${CONTROL}/api-keys/${id}/revoke`,{
    method:"POST",headers:mutationHeaders()
  }),

  usage:()=>rawJson<UsageDaily[]>(`${CONTROL}/usage`),
  requests:()=>rawJson<RequestEvent[]>(`${CONTROL}/requests`),
  settings:()=>rawJson<ConsoleSettings>(`${CONTROL}/settings`),

  playgroundSessions:(projectId:string)=>rawJson<PlaygroundSession[]>(`${CONTROL}/playground/sessions?project_id=${encodeURIComponent(projectId)}`),
  createPlaygroundSession:(projectId:string,modelId:string|null)=>rawJson<PlaygroundSession>(`${CONTROL}/playground/sessions`,{
    method:"POST",headers:mutationHeaders(),body:JSON.stringify({project_id:projectId,model_id:modelId})
  }),
  playgroundSession:(sessionId:string)=>rawJson<PlaygroundConversation>(`${CONTROL}/playground/sessions/${encodeURIComponent(sessionId)}`),
  renamePlaygroundSession:(sessionId:string,title:string)=>rawJson<PlaygroundSession>(`${CONTROL}/playground/sessions/${encodeURIComponent(sessionId)}`,{
    method:"PATCH",headers:mutationHeaders(),body:JSON.stringify({title})
  }),
  deletePlaygroundSession:(sessionId:string)=>rawJson<void>(`${CONTROL}/playground/sessions/${encodeURIComponent(sessionId)}`,{
    method:"DELETE",headers:{"X-CSRF-Token":csrfToken()}
  }),
  sendPlaygroundMessage:(sessionId:string,content:string,modelId:string)=>rawJson<PlaygroundTurn>(`${CONTROL}/playground/sessions/${encodeURIComponent(sessionId)}/messages`,{
    method:"POST",headers:mutationHeaders(),body:JSON.stringify({content,model_id:modelId})
  }),

  models:(key:string)=>rawJson<ModelsResponse>(`${GATEWAY}/models`,{
    headers:{Authorization:`Bearer ${key}`}
  },false),
  chat:(key:string,model:string,prompt:string)=>rawJson<ChatCompletion>(`${GATEWAY}/chat/completions`,{
    method:"POST",
    headers:{
      Authorization:`Bearer ${key}`,
      "Content-Type":"application/json",
      "Idempotency-Key":crypto.randomUUID()
    },
    body:JSON.stringify({model,messages:[{role:"user",content:prompt}],stream:false})
  },false)
};
