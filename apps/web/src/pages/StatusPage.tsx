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

function fmtMs(value:number|null,fallback="—"):string{
  if(value===null||value===undefined)return fallback;
  return `${value} ms`;
}

// ── Chart primitives ────────────────────────────────────────────────────────

const CW=420;
const Y_AXIS=38; // px reserved on left for y-axis labels
const GRID_FRACS=[0.25,0.5,0.75,1.0];

type BarStatus="success"|"error"|"warning"|"neutral";
interface BarDatum{label:string;value:number|null;status:BarStatus;tooltip:string}

const FILL:Record<BarStatus,string>={
  success:"#4adea3",error:"#ff7c8a",warning:"#f7b955",neutral:"#5b7cff"
};

function yLabel(val:number):string{
  if(val>=1000)return `${(val/1000).toFixed(1)}s`;
  return `${val}`;
}

function BarChart({data,height=72,emptyText="No data yet.",unit="ms"}:{
  data:BarDatum[];height?:number;emptyText?:string;unit?:string;
}){
  const [hover,setHover]=useState<number|null>(null);
  if(!data.some(d=>d.value!==null))
    return <p className="chart-empty">{emptyText}</p>;
  const H=height;
  const n=data.length;
  const gap=2;
  const bw=Math.max((CW-Y_AXIS-gap*(n+1))/n,3);
  const max=Math.max(...data.map(d=>d.value??0),1);
  return(
    <div className="chart-wrap" onMouseLeave={()=>setHover(null)}>
      <svg viewBox={`0 0 ${CW} ${H}`} width="100%" height={H}
        style={{display:"block",overflow:"visible"}} preserveAspectRatio="none">
        {/* Y-axis line */}
        <line x1={Y_AXIS} y1={0} x2={Y_AXIS} y2={H} stroke="rgba(139,167,255,0.12)" strokeWidth="0.8"/>
        {/* Grid lines + Y labels */}
        {GRID_FRACS.map(f=>{
          const y=H-H*f;
          const labelVal=Math.round(max*f);
          return(
            <g key={f}>
              <line x1={Y_AXIS} y1={y} x2={CW} y2={y} stroke="rgba(139,167,255,0.07)" strokeWidth="0.8"/>
              <text x={Y_AXIS-4} y={y+3} textAnchor="end" fontSize="7.5"
                fill="rgba(139,167,255,0.40)" fontFamily="inherit" fontVariantNumeric="tabular-nums">
                {yLabel(labelVal)}{unit&&f===1.0?unit:""}
              </text>
            </g>
          );
        })}
        {/* 0 label */}
        <text x={Y_AXIS-4} y={H-1} textAnchor="end" fontSize="7.5"
          fill="rgba(139,167,255,0.28)" fontFamily="inherit">0</text>
        {/* Bars */}
        {data.map((d,i)=>{
          const bh=d.value===null?0:Math.max((d.value/max)*(H-4),3);
          const x=Y_AXIS+gap+i*(bw+gap);
          return(
            <rect key={i} x={x} y={H-bh} width={bw} height={bh} rx="2"
              fill={FILL[d.status]}
              opacity={d.value===null?0:hover===i?1:0.65}
              style={{transition:"opacity .15s,y .15s,height .15s"}}
              onMouseEnter={()=>setHover(i)}/>
          );
        })}
      </svg>
      {hover!==null&&data[hover]&&(
        <div className="chart-tooltip"
          style={{left:`clamp(5%,${Y_AXIS/CW*100+(((hover+0.5)/n)*(100-Y_AXIS/CW*100))}%,92%)`}}>
          <span className="chart-tip-label">{data[hover].label}</span>
          <span className="chart-tip-value">{data[hover].tooltip}</span>
        </div>
      )}
    </div>
  );
}

function DualBarChart({data,height=100}:{data:LatencyPoint[];height?:number}){
  const [hover,setHover]=useState<number|null>(null);
  const hasData=data.some(d=>d.control!==null||d.gateway!==null);
  if(!hasData)
    return(
      <div className="chart-empty-block">
        <span className="chart-empty-icon">↑</span>
        <p>Click <strong>Check services</strong> to start recording latency.</p>
      </div>
    );
  const H=height;
  const n=data.length;
  const groupGap=3;
  const barGap=1;
  const groupW=(CW-Y_AXIS-groupGap*(n+1))/n;
  const bw=Math.max((groupW-barGap)/2,2);
  const max=Math.max(...data.flatMap(d=>[d.control??0,d.gateway??0]),1);

  // X-axis: show label every ~5 bars, always first+last
  const showXAt=new Set<number>();
  showXAt.add(0);showXAt.add(n-1);
  if(n>4)for(let i=Math.round(n/4);i<n-1;i+=Math.round(n/4))showXAt.add(i);

  return(
    <div className="chart-wrap" onMouseLeave={()=>setHover(null)}>
      <svg viewBox={`0 0 ${CW} ${H+16}`} width="100%" height={H+16}
        style={{display:"block",overflow:"visible"}} preserveAspectRatio="none">
        {/* Y-axis */}
        <line x1={Y_AXIS} y1={0} x2={Y_AXIS} y2={H} stroke="rgba(139,167,255,0.12)" strokeWidth="0.8"/>
        {/* Grid + Y labels */}
        {GRID_FRACS.map(f=>{
          const y=H-H*f;
          return(
            <g key={f}>
              <line x1={Y_AXIS} y1={y} x2={CW} y2={y} stroke="rgba(139,167,255,0.07)" strokeWidth="0.8"/>
              <text x={Y_AXIS-4} y={y+3} textAnchor="end" fontSize="7.5"
                fill="rgba(139,167,255,0.40)" fontFamily="inherit" fontVariantNumeric="tabular-nums">
                {yLabel(Math.round(max*f))}{f===1.0?"ms":""}
              </text>
            </g>
          );
        })}
        <text x={Y_AXIS-4} y={H-1} textAnchor="end" fontSize="7.5"
          fill="rgba(139,167,255,0.28)" fontFamily="inherit">0</text>
        {/* Bars */}
        {data.map((d,i)=>{
          const gx=Y_AXIS+groupGap+i*(groupW+groupGap);
          const op=hover===i?1:0.68;
          const ch=d.control!==null?Math.max((d.control/max)*(H-4),3):0;
          const gh=d.gateway!==null?Math.max((d.gateway/max)*(H-4),3):0;
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
        {/* X-axis time labels */}
        {data.map((d,i)=>{
          if(!showXAt.has(i))return null;
          const gx=Y_AXIS+groupGap+i*(groupW+groupGap)+groupW/2;
          return(
            <text key={`xl${i}`} x={gx} y={H+13} textAnchor="middle" fontSize="7.5"
              fill="rgba(139,167,255,0.35)" fontFamily="inherit">
              {d.time.toLocaleTimeString([],{hour:"2-digit",minute:"2-digit"})}
            </text>
          );
        })}
      </svg>
      {hover!==null&&data[hover]&&(
        <div className="chart-tooltip"
          style={{left:`clamp(5%,${Y_AXIS/CW*100+(((hover+0.5)/n)*(100-Y_AXIS/CW*100))}%,92%)`}}>
          <span className="chart-tip-label">
            {data[hover].time.toLocaleTimeString([],{hour:"2-digit",minute:"2-digit",second:"2-digit"})}
          </span>
          <span className="chart-tip-value">
            <span className="tip-dot" style={{background:"#5b7cff"}}/>
            Control&nbsp;{data[hover].control!=null?`${data[hover].control} ms`:"—"}
          </span>
          <span className="chart-tip-value">
            <span className="tip-dot" style={{background:"#78adff"}}/>
            Nexr Server&nbsp;{data[hover].gateway!=null?`${data[hover].gateway} ms`:"—"}
          </span>
        </div>
      )}
    </div>
  );
}

// ── Analysis helpers ─────────────────────────────────────────────────────────

function latencyTag(values:number[]){
  if(!values.length)return null;
  const avg=values.reduce((a,b)=>a+b,0)/values.length;
  const max=Math.max(...values);
  const spread=max-Math.min(...values);
  if(avg<100&&spread<50)return{label:"Stable",cls:"tag-stable"};
  if(avg<300)return{label:"Nominal",cls:"tag-nominal"};
  if(avg<1000)return{label:"Elevated",cls:"tag-warning"};
  return{label:"High Latency",cls:"tag-danger"};
}

// ── Main Page ────────────────────────────────────────────────────────────────

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
      probe("Core Service · Nexr Server 1.0",api.gatewayHealth),
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
      const list=map.get(run.job_name)??[];list.push(run);
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

  function avgDuration(runs:AdminJobRun[]):number|null{
    const timed=runs.filter(r=>r.duration_ms!=null);
    if(!timed.length)return null;
    return Math.round(timed.reduce((s,r)=>s+(r.duration_ms??0),0)/timed.length);
  }

  const controlVals=latencyHistory.map(p=>p.control).filter((v):v is number=>v!==null);
  const gatewayVals=latencyHistory.map(p=>p.gateway).filter((v):v is number=>v!==null);
  const avgControl=controlVals.length?Math.round(controlVals.reduce((a,b)=>a+b,0)/controlVals.length):null;
  const avgGateway=gatewayVals.length?Math.round(gatewayVals.reduce((a,b)=>a+b,0)/gatewayVals.length):null;
  const peakControl=controlVals.length?Math.max(...controlVals):null;
  const peakGateway=gatewayVals.length?Math.max(...gatewayVals):null;
  const lastControl=latencyHistory.length?latencyHistory[latencyHistory.length-1].control:null;
  const lastGateway=latencyHistory.length?latencyHistory[latencyHistory.length-1].gateway:null;
  const ctrlTag=latencyTag(controlVals);
  const gwTag=latencyTag(gatewayVals);

  return(
    <div className="page-stack">

      {/* ── Toolbar ── */}
      <div className="section-toolbar">
        <div>
          <p className="section-copy">Live health checks, catalog freshness, and background worker status.</p>
          {checkedAt&&<small className="toolbar-meta">Last checked {checkedAt.toLocaleTimeString([], {hour:"2-digit",minute:"2-digit",second:"2-digit"})}</small>}
        </div>
        <button onClick={load} disabled={loading}>{loading?"Checking…":"Check services"}</button>
      </div>

      {/* ── System banner ── */}
      <section className={"system-banner "+(allHealthy?"healthy":"attention")}>
        <span className="system-indicator"/>
        <div>
          <strong>{loading?"Checking platform health…":allHealthy?"All systems operational":"Attention required"}</strong>
          <p>{loading?"Running live health checks across all services.":allHealthy?"Control plane, data plane, and background workers are healthy.":"One or more services need attention. Review details below."}</p>
        </div>
      </section>

      {/* ── Service cards ── */}
      <div className="service-grid">
        {services.map(service=>(
          <article className="service-card" key={service.name}>
            <div className="service-card-head">
              <div>
                <span className={"service-dot "+(service.ok?"online":"offline")}/>
                <strong>{service.name}</strong>
              </div>
              <span className={"status "+(service.ok?"active":"revoked")}>
                {service.ok?"Operational":"Unavailable"}
              </span>
            </div>
            <dl className="definition-list compact-definition">
              <div><dt>Service</dt><dd>{service.response?.service||service.name}</dd></div>
              <div><dt>Status</dt><dd>{service.response?.status||"Failed"}</dd></div>
              <div><dt>Latency</dt><dd className="dd-latency">{fmtMs(service.latency)}</dd></div>
            </dl>
            {service.error&&<p className="service-error">{service.error}</p>}
          </article>
        ))}
      </div>

      {/* ── Latency trend card ── */}
      <article className="content-card">
        <div className="content-card-head">
          <div>
            <span className="card-eyebrow">Performance · {latencyHistory.length} / 20 checks</span>
            <h2>Service Latency Trend</h2>
          </div>
          <div className="chart-legend">
            <span className="chart-legend-item">
              <span className="chart-legend-dot" style={{background:"#5b7cff"}}/>Control API
            </span>
            <span className="chart-legend-item">
              <span className="chart-legend-dot" style={{background:"#78adff"}}/>Nexr Server 1.0
            </span>
          </div>
        </div>

        {/* Stat grid */}
        <div className="latency-stat-grid">
          <div className="latency-stat-block">
            <span className="latency-stat-label">Current · Control</span>
            <span className="latency-stat-value" style={{color:"#7c9fff"}}>{fmtMs(lastControl)}</span>
          </div>
          <div className="latency-stat-block">
            <span className="latency-stat-label">Current · Nexr Server</span>
            <span className="latency-stat-value" style={{color:"#78adff"}}>{fmtMs(lastGateway)}</span>
          </div>
          <div className="latency-stat-block">
            <span className="latency-stat-label">Avg · Control</span>
            <span className="latency-stat-value secondary">{avgControl!=null?`${avgControl} ms`:"—"}</span>
          </div>
          <div className="latency-stat-block">
            <span className="latency-stat-label">Avg · Nexr Server</span>
            <span className="latency-stat-value secondary">{avgGateway!=null?`${avgGateway} ms`:"—"}</span>
          </div>
          <div className="latency-stat-block">
            <span className="latency-stat-label">Peak · Control</span>
            <span className="latency-stat-value secondary">{fmtMs(peakControl)}</span>
          </div>
          <div className="latency-stat-block">
            <span className="latency-stat-label">Peak · Nexr Server</span>
            <span className="latency-stat-value secondary">{fmtMs(peakGateway)}</span>
          </div>
        </div>

        <DualBarChart data={latencyHistory} height={100}/>

        {/* Analysis row */}
        {latencyHistory.length>0&&(
          <div className="chart-analysis-row">
            {ctrlTag&&<span className={`analysis-tag ${ctrlTag.cls}`}>{ctrlTag.label} · Control</span>}
            {gwTag&&<span className={`analysis-tag ${gwTag.cls}`}>{gwTag.label} · Nexr Server</span>}
            {avgControl!=null&&avgGateway!=null&&(
              <span className="analysis-note">
                Combined avg&nbsp;<strong>{Math.round((avgControl+avgGateway)/2)} ms</strong>
              </span>
            )}
          </div>
        )}
      </article>

      {/* ── Job Duration History ── */}
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
              const running=runs.filter(r=>r.status==="running").length;
              const successRate=runs.length?Math.round(succeeded/runs.length*100):0;
              return(
                <div key={name} className="job-chart-item">
                  <div className="job-chart-header">
                    <strong>{formatJobName(name)}</strong>
                    <div className="job-chart-meta-row">
                      {avg!=null&&<span className="job-meta-pill">avg {avg} ms</span>}
                      <span className="job-meta-pill success-pill">{succeeded} ok</span>
                      {failed>0&&<span className="job-meta-pill fail-pill">{failed} failed</span>}
                      {running>0&&<span className="job-meta-pill run-pill">{running} running</span>}
                      <span className="job-meta-pill">{runs.length} total · {successRate}% success</span>
                    </div>
                  </div>
                  <BarChart
                    height={52}
                    unit="ms"
                    data={runs.map(r=>({
                      label:relativeTime(r.started_at),
                      value:r.duration_ms,
                      status:runStatus(r),
                      tooltip:r.duration_ms!=null?`${r.duration_ms} ms · ${r.status}`:r.status,
                    }))}
                  />
                </div>
              );
            })}
          </div>
        </article>
      )}

      {/* ── Operations ── */}
      {operationsError&&<p className="error">{operationsError}</p>}
      {operations&&(
        <section className="operations-grid">
          <article className="content-card catalog-status-card">
            <div className="content-card-head">
              <div>
                <span className="card-eyebrow">Model Catalog</span>
                <h2>Catalog Freshness</h2>
              </div>
              <span className={"status "+(!operations.catalog.stale&&operations.catalog.status==="succeeded"?"active":"pending")}>
                {operations.catalog.stale?"Stale":operations.catalog.status}
              </span>
            </div>
            <dl className="definition-list compact-definition">
              <div><dt>Active models</dt><dd><strong>{operations.catalog.active_model_count}</strong></dd></div>
              <div><dt>Last successful sync</dt><dd>{relativeTime(operations.catalog.last_success_at)}</dd></div>
              <div><dt>Freshness window</dt><dd>{operations.catalog.stale_after_minutes} minutes</dd></div>
              <div><dt>Redis cache</dt><dd>{operations.catalog.cache_available?`Ready · ${operations.catalog.cache_ttl_seconds}s TTL`:"Cold / unavailable"}</dd></div>
            </dl>
            {operations.catalog.stale&&(
              <p className="service-error">Catalog has not synced within the {operations.catalog.stale_after_minutes}-minute freshness window.</p>
            )}
          </article>

          <article className="content-card">
            <div className="content-card-head">
              <div>
                <span className="card-eyebrow">Celery</span>
                <h2>Background Jobs</h2>
              </div>
              <span className={"status "+(operations.status==="ok"?"active":"pending")}>
                {operations.status==="ok"?"Ok":operations.status}
              </span>
            </div>
            <div className="job-health-list">
              {operations.jobs.map(job=>(
                <div className="job-health-row" key={job.job_name}>
                  <span className={"service-dot "+(job.status==="succeeded"?"online":job.status==="failed"?"offline":"idle")}/>
                  <div>
                    <strong>{formatJobName(job.job_name)}</strong>
                    <small>{job.started_at?`Last run ${relativeTime(job.started_at)}`:"No run recorded yet"}</small>
                    {job.error_message&&(
                      <small className="job-error" title={job.error_message}>{job.error_message}</small>
                    )}
                  </div>
                  <div className="job-health-meta">
                    <span className={"status "+jobStatusClass(job)}>{job.status}</span>
                    <small>{fmtMs(job.duration_ms)}</small>
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
          <p>Click "Check services" to run health probes.</p>
        </div>
      )}
    </div>
  );
}
