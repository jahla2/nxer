import React,{useEffect,useMemo,useState} from "react";
import {api,HealthResponse} from "../api";

type ServiceState={
  name:string;
  response:HealthResponse|null;
  ok:boolean;
  error:string;
  latency:number|null;
};

export function StatusPage(){
  const [services,setServices]=useState<ServiceState[]>([]);
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
    setLoading(true);
    const results=await Promise.all([
      probe("Control API",api.controlHealth),
      probe("Go Gateway",api.gatewayHealth),
    ]);
    setServices(results);
    setCheckedAt(new Date());
    setLoading(false);
  }

  useEffect(()=>{void load()},[]);

  const allHealthy=useMemo(()=>services.length>0&&services.every(service=>service.ok),[services]);

  return <div className="page-stack">
    <div className="section-toolbar">
      <div>
        <p className="section-copy">Live browser-to-service checks through the same origin used by the console.</p>
        {checkedAt&&<small className="toolbar-meta">Last checked {checkedAt.toLocaleTimeString()}</small>}
      </div>
      <button onClick={load} disabled={loading}>{loading?"Checking…":"Check services"}</button>
    </div>

    <section className={"system-banner "+(allHealthy?"healthy":"attention")}>
      <span className="system-indicator"/>
      <div><strong>{loading?"Checking platform health…":allHealthy?"Core services operational":"Service attention required"}</strong><p>{loading?"Running live checks.":allHealthy?"Control-plane and data-plane health endpoints are responding.":"One or more health checks did not return OK."}</p></div>
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

    {!services.length&&!loading&&<div className="empty-state-card"><h2>No health data</h2><p>Run the checks again to inspect service availability.</p></div>}
  </div>;
}
