import React,{useEffect,useMemo,useState} from "react";
import {api,BackgroundJobStatus,HealthResponse,OperationsStatus} from "../api";

type ServiceState={
  name:string;
  response:HealthResponse|null;
  ok:boolean;
  error:string;
  latency:number|null;
};

function relativeTime(value:string|null){
  if(!value)return "Never";
  const timestamp=new Date(value).getTime();
  if(Number.isNaN(timestamp))return "Unknown";
  const seconds=Math.max(0,Math.floor((Date.now()-timestamp)/1000));
  if(seconds<60)return "Just now";
  const minutes=Math.floor(seconds/60);
  if(minutes<60)return `${minutes}m ago`;
  const hours=Math.floor(minutes/60);
  if(hours<24)return `${hours}h ago`;
  return `${Math.floor(hours/24)}d ago`;
}

function formatJobName(value:string){
  return value
    .replace(/^nexora\./,"")
    .split("_")
    .map(part=>part.charAt(0).toUpperCase()+part.slice(1))
    .join(" ");
}

function jobStatusClass(job:BackgroundJobStatus){
  if(job.status==="succeeded")return "active";
  if(job.status==="failed")return "revoked";
  if(job.status==="running")return "pending";
  return "";
}

export function StatusPage(){
  const [services,setServices]=useState<ServiceState[]>([]);
  const [operations,setOperations]=useState<OperationsStatus|null>(null);
  const [operationsError,setOperationsError]=useState("");
  const [loading,setLoading]=useState(true);
  const [checkedAt,setCheckedAt]=useState<Date|null>(null);

  async function probe(name:string,fn:()=>Promise<HealthResponse>):Promise<ServiceState>{
    const started=performance.now();
    try{
      const response=await fn();
      return {name,response,ok:response.status==="ok",error:"",latency:Math.round(performance.now()-started)};
    }catch(err){
      return {name,response:null,ok:false,error:err instanceof Error?err.message:String(err),latency:Math.round(performance.now()-started)};
    }
  }

  async function load(){
    setLoading(true);setOperationsError("");
    const [control,gateway,opsResult]=await Promise.all([
      probe("Control API",api.controlHealth),
      probe("Go Gateway",api.gatewayHealth),
      api.operationsStatus()
        .then(value=>({value,error:""}))
        .catch(err=>({value:null,error:err instanceof Error?err.message:String(err)})),
    ]);
    setServices([control,gateway]);
    setOperations(opsResult.value);
    setOperationsError(opsResult.error);
    setCheckedAt(new Date());
    setLoading(false);
  }

  useEffect(()=>{void load()},[]);

  const allHealthy=useMemo(()=>{
    const coreHealthy=services.length>0&&services.every(service=>service.ok);
    return coreHealthy&&operations?.status==="ok";
  },[services,operations]);

  return <div className="page-stack">
    <div className="section-toolbar">
      <div>
        <p className="section-copy">Live service checks, catalog freshness and background worker health.</p>
        {checkedAt&&<small className="toolbar-meta">Last checked {checkedAt.toLocaleTimeString()}</small>}
      </div>
      <button onClick={load} disabled={loading}>{loading?"Checking…":"Check services"}</button>
    </div>

    <section className={"system-banner "+(allHealthy?"healthy":"attention")}>
      <span className="system-indicator"/>
      <div>
        <strong>{loading?"Checking platform health…":allHealthy?"Core services operational":"Service attention required"}</strong>
        <p>{loading?"Running live checks.":allHealthy?"Control plane, data plane and background operations are reporting healthy.":"Review service, catalog or background-job details below."}</p>
      </div>
    </section>

    <div className="service-grid">
      {services.map(service=><article className="service-card" key={service.name}>
        <div className="service-card-head">
          <div><span className={"service-dot "+(service.ok?"online":"offline")}/><strong>{service.name}</strong></div>
          <span className={"status "+(service.ok?"active":"revoked")}>{service.ok?"Operational":"Unavailable"}</span>
        </div>
        <dl className="definition-list compact-definition">
          <div><dt>Reported service</dt><dd>{service.response?.service||service.name}</dd></div>
          <div><dt>HTTP check</dt><dd>{service.response?.status||"Failed"}</dd></div>
          <div><dt>Browser latency</dt><dd>{service.latency==null?"—":`${service.latency} ms`}</dd></div>
        </dl>
        {service.error&&<p className="service-error">{service.error}</p>}
      </article>)}
    </div>

    {operationsError&&<p className="error">{operationsError}</p>}

    {operations&&<section className="operations-grid">
      <article className="content-card catalog-status-card">
        <div className="content-card-head">
          <div><span className="card-eyebrow">Model catalog</span><h2>Catalog freshness</h2></div>
          <span className={"status "+(!operations.catalog.stale&&operations.catalog.status==="succeeded"?"active":"pending")}>
            {operations.catalog.stale?"stale":operations.catalog.status}
          </span>
        </div>
        <dl className="definition-list compact-definition">
          <div><dt>Active models</dt><dd>{operations.catalog.active_model_count}</dd></div>
          <div><dt>Last successful sync</dt><dd>{relativeTime(operations.catalog.last_success_at)}</dd></div>
          <div><dt>Freshness window</dt><dd>{operations.catalog.stale_after_minutes} minutes</dd></div>
          <div><dt>Redis catalog cache</dt><dd>{operations.catalog.cache_available?`Ready · ${operations.catalog.cache_ttl_seconds}s TTL`:"Unavailable / cold"}</dd></div>
        </dl>
        {operations.catalog.stale&&<p className="service-error">The catalog has not completed a successful sync inside the expected freshness window.</p>}
      </article>

      <article className="content-card">
        <div className="content-card-head">
          <div><span className="card-eyebrow">Celery</span><h2>Background jobs</h2></div>
          <span className={"status "+(operations.status==="ok"?"active":"pending")}>{operations.status}</span>
        </div>
        <div className="job-health-list">
          {operations.jobs.map(job=><div className="job-health-row" key={job.job_name}>
            <span className={"service-dot "+(job.status==="succeeded"?"online":job.status==="failed"?"offline":"idle")}/>
            <div>
              <strong>{formatJobName(job.job_name)}</strong>
              <small>{job.started_at?`Last run ${relativeTime(job.started_at)}`:"No run recorded yet"}</small>
              {job.error_message&&<small className="job-error" title={job.error_message}>{job.error_message}</small>}
            </div>
            <div className="job-health-meta">
              <span className={"status "+jobStatusClass(job)}>{job.status}</span>
              <small>{job.duration_ms==null?"—":`${job.duration_ms} ms`}</small>
            </div>
          </div>)}
        </div>
      </article>
    </section>}

    {!services.length&&!loading&&<div className="empty-state-card"><h2>No health data</h2><p>Run the checks again to inspect service availability.</p></div>}
  </div>;
}
