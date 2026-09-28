import React,{useEffect,useMemo,useState} from "react";
import {api,AdminJobRun,BackgroundJobStatus,HealthResponse,OperationsStatus} from "../api";

type ServiceState={
  name:string;
  response:HealthResponse|null;
  ok:boolean;
  error:string;
  latency:number|null;
};

type LatencyPoint={
  time:Date;
  control:number|null;
  gateway:number|null;
};

function relativeTime(value:string|null){
  if(!value)return "Never";
  const ts=new Date(value).getTime();
  if(Number.isNaN(ts))return "Unknown";
  const s=Math.max(0,Math.floor((Date.now()-ts)/1000));
  if(s<60)return "Just now";
  const m=Math.floor(s/60);
  if(m<60)return `${m}m ago`;
  const h=Math.floor(m/60);
  if(h<24)return `${h}h ago`;
  return `${Math.floor(h/24)}d ago`;
}

function formatJobName(value:string){
  return value.replace(/^nexora\./,"").split("_")
    .map(p=>p.charAt(0).toUpperCase()+p.slice(1)).join(" ");
}

function jobStatusClass(job:BackgroundJobStatus){
  if(job.status==="succeeded")return "active";
  if(job.status==="failed")return "revoked";
  if(job.status==="running")return "pending";
  return "";
}

// ── Chart primitives ────────────────────────────────────────────────────────

const CW=400; // viewBox width
const GRID=[0.25,0.5,0.75];

type BarStatus="success"|"error"|"warning"|"neutral";
interface BarDatum{label:string;value:number|null;status:BarStatus;tooltip:string}

const FILL:Record<BarStatus,string>={
  success:"#4adea3",error:"#ff7c8a",warning:"#f7b955",neutral:"#5b7cff"
};

function BarChart({data,height=72,emptyText="No data yet."}:{
  data:BarDatum[];height?:number;emptyText?:string;
}){
  const [hover,setHover]=useState<number|null>(null);
  if(!data.some(d=>d.value!==null))
    return <p className="chart-empty">{emptyText}</p>;
  const H=height;
  const n=data.length;
  const gap=3;
  const bw=Math.max((CW-gap*(n+1))/n,3);
  const max=Math.max(...data.map(d=>d.value??0),1);
  return(
    <div className="chart-wrap" onMouseLeave={()=>setHover(null)}>
      <svg viewBox={`0 0 ${CW} ${H}`} width="100%" height={H}
        style={{display:"block",overflow:"visible"}} preserveAspectRatio="none">
        {GRID.map(f=>(
          <line key={f} x1={0} y1={H-H*f} x2={CW} y2={H-H*f}
            stroke="rgba(139,167,255,0.07)" strokeWidth="0.8"/>
        ))}
        {data.map((d,i)=>{
          const bh=d.value===null?0:Math.max((d.value/max)*(H-6),3);
          const x=gap+i*(bw+gap);
          return(
            <rect key={i} x={x} y={H-bh} width={bw} height={bh} rx="2"
              fill={FILL[d.status]}
              opacity={d.value===null?0:hover===i?1:0.68}
              style={{transition:"opacity .15s"}}
              onMouseEnter={()=>setHover(i)}/>
          );
        })}
      </svg>
      {hover!==null&&data[hover]&&(
        <div className="chart-tooltip"
          style={{left:`clamp(10%,${((hover+0.5)/n)*100}%,90%)`}}>
          <span className="chart-tip-label">{data[hover].label}</span>
          <span className="chart-tip-value">{data[hover].tooltip}</span>
        </div>
      )}
    </div>
  );
}

function DualBarChart({data,height=80}:{data:LatencyPoint[];height?:number}){
  const [hover,setHover]=useState<number|null>(null);
  if(!data.some(d=>d.control!==null||d.gateway!==null))
    return(
      <p className="chart-empty">
        Click "Check services" to start recording latency history.
      </p>
    );
  const H=height;
  const n=data.length;
  const groupGap=4;
  const barGap=1;
  const groupW=(CW-groupGap*(n+1))/n;
  const bw=Math.max((groupW-barGap)/2,2);
  const max=Math.max(...data.flatMap(d=>[d.control??0,d.gateway??0]),1);
  return(
    <div className="chart-wrap" onMouseLeave={()=>setHover(null)}>
      <svg viewBox={`0 0 ${CW} ${H}`} width="100%" height={H}
        style={{display:"block",overflow:"visible"}} preserveAspectRatio="none">
        {GRID.map(f=>(
          <line key={f} x1={0} y1={H-H*f} x2={CW} y2={H-H*f}
            stroke="rgba(139,167,255,0.07)" strokeWidth="0.8"/>
        ))}
        {data.map((d,i)=>{
          const gx=groupGap+i*(groupW+groupGap);
          const op=hover===i?1:0.72;
          const ch=d.control!==null?Math.max((d.control/max)*(H-6),3):0;
          const gh=d.gateway!==null?Math.max((d.gateway/max)*(H-6),3):0;
          return(
            <g key={i} onMouseEnter={()=>setHover(i)} style={{cursor:"default"}}>
              {d.control!==null&&(
                <rect x={gx} y={H-ch} width={bw} height={ch} rx="1.5"
                  fill="#5b7cff" opacity={op} style={{transition:"opacity .15s"}}/>
              )}
              {d.gateway!==null&&(
                <rect x={gx+bw+barGap} y={H-gh} width={bw} height={gh} rx="1.5"
                  fill="#78adff" opacity={op} style={{transition:"opacity .15s"}}/>
              )}
            </g>
          );
        })}
      </svg>
      {hover!==null&&data[hover]&&(
        <div className="chart-tooltip"
          style={{left:`clamp(10%,${((hover+0.5)/n)*100}%,90%)`}}>
          <span className="chart-tip-label">
            {data[hover].time.toLocaleTimeString([],{hour:"2-digit",minute:"2-digit",second:"2-digit"})}
          </span>
          <span className="chart-tip-value">
            Control&nbsp;{data[hover].control!=null?`${data[hover].control}ms`:"—"}
            &nbsp;·&nbsp;Gateway&nbsp;{data[hover].gateway!=null?`${data[hover].gateway}ms`:"—"}
          </span>
        </div>
      )}
    </div>
  );
}

// ── Main Page ───────────────────────────────────────────────────────────────

export function StatusPage(){
  const [services,setServices]=useState<ServiceState[]>([]);
  const [operations,setOperations]=useState<OperationsStatus|null>(null);
  const [operationsError,setOperationsError]=useState("");
  const [loading,setLoading]=useState(true);
  const [checkedAt,setCheckedAt]=useState<Date|null>(null);
  const [latencyHistory,setLatencyHistory]=useState<LatencyPoint[]>([]);
  const [jobRuns,setJobRuns]=useState<AdminJobRun[]|null>(null);

  async function probe(name:string,fn:()=>Promise<HealthResponse>):Promise<ServiceState>{
    const started=performance.now();
    try{
      const response=await fn();
      return{name,response,ok:response.status==="ok",error:"",latency:Math.round(performance.now()-started)};
    }catch(err){
      return{name,response:null,ok:false,error:err instanceof Error?err.message:String(err),latency:Math.round(performance.now()-started)};
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
    const now=new Date();
    setCheckedAt(now);
    setLatencyHistory(prev=>[...prev.slice(-19),{time:now,control:control.latency,gateway:gateway.latency}]);
    if(jobRuns===null){
      api.adminJobs(60)
        .then(runs=>setJobRuns(runs))
        .catch(()=>setJobRuns([]));
    }
    setLoading(false);
  }

  useEffect(()=>{void load();},[]);

  const allHealthy=useMemo(()=>{
    const coreHealthy=services.length>0&&services.every(s=>s.ok);
    return coreHealthy&&operations?.status==="ok";
  },[services,operations]);

  const jobRunsByName=useMemo(()=>{
    if(!jobRuns?.length)return new Map<string,AdminJobRun[]>();
    const map=new Map<string,AdminJobRun[]>();
    for(const run of jobRuns){
      const list=map.get(run.job_name)??[];
      list.push(run);
      map.set(run.job_name,list);
    }
    for(const[name,list]of map){
      map.set(name,[...list]
        .sort((a,b)=>new Date(a.started_at).getTime()-new Date(b.started_at).getTime())
        .slice(-15));
    }
    return map;
  },[jobRuns]);

  function runStatus(run:AdminJobRun):BarStatus{
    if(run.status==="succeeded")return "success";
    if(run.status==="failed")return "error";
    if(run.status==="running")return "warning";
    return "neutral";
  }

  function avgDuration(runs:AdminJobRun[]){
    const timed=runs.filter(r=>r.duration_ms!=null);
    if(!timed.length)return null;
    return Math.round(timed.reduce((s,r)=>s+(r.duration_ms??0),0)/timed.length);
  }

  // Compute peak latency across history for display
  const peakControl=latencyHistory.length?Math.max(...latencyHistory.map(p=>p.control??0)):null;
  const peakGateway=latencyHistory.length?Math.max(...latencyHistory.map(p=>p.gateway??0)):null;
  const lastControl=latencyHistory.length?latencyHistory[latencyHistory.length-1].control:null;
  const lastGateway=latencyHistory.length?latencyHistory[latencyHistory.length-1].gateway:null;

  return(
    <div className="page-stack">

      {/* ── Toolbar ── */}
      <div className="section-toolbar">
        <div>
          <p className="section-copy">Live service checks, catalog freshness and background worker health.</p>
          {checkedAt&&<small className="toolbar-meta">Last checked {checkedAt.toLocaleTimeString()}</small>}
        </div>
        <button onClick={load} disabled={loading}>{loading?"Checking…":"Check services"}</button>
      </div>

      {/* ── System banner ── */}
      <section className={"system-banner "+(allHealthy?"healthy":"attention")}>
        <span className="system-indicator"/>
        <div>
          <strong>{loading?"Checking platform health…":allHealthy?"Core services operational":"Service attention required"}</strong>
          <p>{loading?"Running live checks.":allHealthy?"Control plane, data plane and background operations are reporting healthy.":"Review service, catalog or background-job details below."}</p>
        </div>
      </section>

      {/* ── Service cards ── */}
      <div className="service-grid">
        {services.map(service=>(
          <article className="service-card" key={service.name}>
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
          </article>
        ))}
      </div>

      {/* ── Monitoring section ── */}
      <div className="monitoring-section">

        {/* Service Latency Trend */}
        <article className="content-card">
          <div className="content-card-head">
            <div>
              <span className="card-eyebrow">Performance</span>
              <h2>Service Latency Trend</h2>
            </div>
            <div className="chart-legend">
              <span className="chart-legend-item">
                <span className="chart-legend-dot" style={{background:"#5b7cff"}}/>
                Control API
              </span>
              <span className="chart-legend-item">
                <span className="chart-legend-dot" style={{background:"#78adff"}}/>
                Go Gateway
              </span>
            </div>
          </div>
          <div className="chart-stat-row">
            <div className="chart-stat-item">
              <span className="chart-stat-label">Current · Control</span>
              <span className="chart-stat-value">{lastControl!=null?`${lastControl}ms`:"—"}</span>
            </div>
            <div className="chart-stat-item">
              <span className="chart-stat-label">Current · Gateway</span>
              <span className="chart-stat-value">{lastGateway!=null?`${lastGateway}ms`:"—"}</span>
            </div>
            <div className="chart-stat-item">
              <span className="chart-stat-label">Peak · Control</span>
              <span className="chart-stat-value chart-stat-muted">{peakControl?`${peakControl}ms`:"—"}</span>
            </div>
            <div className="chart-stat-item">
              <span className="chart-stat-label">Peak · Gateway</span>
              <span className="chart-stat-value chart-stat-muted">{peakGateway?`${peakGateway}ms`:"—"}</span>
            </div>
            <div className="chart-stat-item">
              <span className="chart-stat-label">Checks recorded</span>
              <span className="chart-stat-value">{latencyHistory.length} / 20</span>
            </div>
          </div>
          <DualBarChart data={latencyHistory} height={88}/>
          {latencyHistory.length>1&&(
            <div className="chart-x-labels">
              <span>{latencyHistory[0].time.toLocaleTimeString([],{hour:"2-digit",minute:"2-digit"})}</span>
              <span>{latencyHistory[latencyHistory.length-1].time.toLocaleTimeString([],{hour:"2-digit",minute:"2-digit"})}</span>
            </div>
          )}
        </article>

        {/* Job Duration History (admin only — hidden if not available) */}
        {jobRunsByName.size>0&&(
          <article className="content-card">
            <div className="content-card-head">
              <div>
                <span className="card-eyebrow">Workers</span>
                <h2>Job Duration History</h2>
              </div>
              <div className="chart-legend">
                <span className="chart-legend-item"><span className="chart-legend-dot" style={{background:"#4adea3"}}/>Succeeded</span>
                <span className="chart-legend-item"><span className="chart-legend-dot" style={{background:"#ff7c8a"}}/>Failed</span>
                <span className="chart-legend-item"><span className="chart-legend-dot" style={{background:"#f7b955"}}/>Running</span>
              </div>
            </div>
            <div className="job-charts-grid">
              {Array.from(jobRunsByName.entries()).map(([name,runs])=>{
                const avg=avgDuration(runs);
                const succeeded=runs.filter(r=>r.status==="succeeded").length;
                const failed=runs.filter(r=>r.status==="failed").length;
                return(
                  <div key={name} className="job-chart-item">
                    <div className="job-chart-header">
                      <strong>{formatJobName(name)}</strong>
                      <span className="job-chart-stat">
                        {avg!=null?`avg ${avg}ms · `:""}
                        {runs.length} runs
                        {failed>0&&<span className="job-chart-fail"> · {failed} failed</span>}
                      </span>
                    </div>
                    <BarChart
                      height={44}
                      data={runs.map(r=>({
                        label:relativeTime(r.started_at),
                        value:r.duration_ms,
                        status:runStatus(r),
                        tooltip:r.duration_ms!=null?`${r.duration_ms}ms · ${r.status}`:r.status,
                      }))}
                    />
                  </div>
                );
              })}
            </div>
          </article>
        )}
      </div>

      {/* ── Operations ── */}
      {operationsError&&<p className="error">{operationsError}</p>}
      {operations&&(
        <section className="operations-grid">
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
              {operations.jobs.map(job=>(
                <div className="job-health-row" key={job.job_name}>
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
                </div>
              ))}
            </div>
          </article>
        </section>
      )}

      {!services.length&&!loading&&(
        <div className="empty-state-card">
          <h2>No health data</h2>
          <p>Run the checks again to inspect service availability.</p>
        </div>
      )}
    </div>
  );
}
