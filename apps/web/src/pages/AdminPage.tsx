import React,{useEffect,useMemo,useState} from "react";
import {api,AdminJobRun,AuditLogEntry} from "../api";
import {Pagination} from "../components/Pagination";

const PAGE_SIZE=20;

function formatDuration(value:number|null){
  if(value==null)return "—";
  if(value<1000)return `${value} ms`;
  return `${(value/1000).toFixed(value<10000?1:0)} s`;
}

function shortId(value:string|null){
  if(!value)return "—";
  return value.length>18?`${value.slice(0,10)}…${value.slice(-4)}`:value;
}

function summarizeObject(value:Record<string,unknown>){
  const entries=Object.entries(value).slice(0,4);
  if(!entries.length)return "—";
  return entries.map(([key,item])=>{
    if(item===null||item===undefined)return `${key}=—`;
    if(typeof item==="object")return `${key}=…`;
    return `${key}=${String(item)}`;
  }).join(" · ");
}

function statusClass(value:string){
  if(value==="succeeded")return "active";
  if(value==="failed")return "revoked";
  if(value==="running")return "pending";
  return "";
}

export function AdminPage(){
  const [jobs,setJobs]=useState<AdminJobRun[]>([]);
  const [audit,setAudit]=useState<AuditLogEntry[]>([]);
  const [loading,setLoading]=useState(true);
  const [error,setError]=useState("");
  const [checkedAt,setCheckedAt]=useState<Date|null>(null);
  const [jobsPage,setJobsPage]=useState(1);
  const [auditPage,setAuditPage]=useState(1);

  async function load(){
    setLoading(true);setError("");
    try{
      const [jobRows,auditRows]=await Promise.all([
        api.adminJobs(100),
        api.adminAudit(100),
      ]);
      setJobs(jobRows);
      setAudit(auditRows);
      setJobsPage(1);
      setAuditPage(1);
      setCheckedAt(new Date());
    }catch(err){
      setError(err instanceof Error?err.message:String(err));
    }finally{
      setLoading(false);
    }
  }

  useEffect(()=>{void load()},[]);

  const pagedJobs=useMemo(()=>jobs.slice((jobsPage-1)*PAGE_SIZE,jobsPage*PAGE_SIZE),[jobs,jobsPage]);
  const pagedAudit=useMemo(()=>audit.slice((auditPage-1)*PAGE_SIZE,auditPage*PAGE_SIZE),[audit,auditPage]);

  const summary=useMemo(()=>({
    failed:jobs.filter(item=>item.status==="failed").length,
    running:jobs.filter(item=>item.status==="running").length,
    succeeded:jobs.filter(item=>item.status==="succeeded").length,
    audit:audit.length,
  }),[jobs,audit]);

  return <div className="page-stack">
    <div className="section-toolbar">
      <div>
        <p className="section-copy">Administrator-only background job history and security-sensitive audit metadata.</p>
        {checkedAt&&<small className="toolbar-meta">Last refreshed {checkedAt.toLocaleTimeString()}</small>}
      </div>
      <button onClick={load} disabled={loading}>{loading?"Refreshing…":"Refresh"}</button>
    </div>

    {error&&<p className="error">{error}</p>}

    <section className="overview-grid">
      <article className="stat-card"><span>Successful jobs</span><strong>{summary.succeeded}</strong><small>Loaded history</small></article>
      <article className="stat-card"><span>Running jobs</span><strong>{summary.running}</strong><small>Current executions</small></article>
      <article className="stat-card"><span>Failed jobs</span><strong>{summary.failed}</strong><small>Needs operator review</small></article>
      <article className="stat-card"><span>Audit events</span><strong>{summary.audit}</strong><small>Latest activity</small></article>
    </section>

    {loading&&!jobs.length&&!audit.length?<div className="loading-card"><span className="spinner"/>Loading operations data…</div>:<>
      <section className="content-card">
        <div className="content-card-head">
          <div><span className="card-eyebrow">Background jobs</span><h2>Recent executions</h2></div>
          <span className="method-badge">{jobs.length} loaded</span>
        </div>
        {jobs.length?<div className="data-table-wrap">
          <table className="data-table admin-jobs-table">
            <thead><tr><th>Started</th><th>Job</th><th>Status</th><th>Duration</th><th>Result</th><th>Worker</th></tr></thead>
            <tbody>{pagedJobs.map(item=><tr key={item.id}>
              <td data-label="Started">{new Date(item.started_at).toLocaleString()}</td>
              <td data-label="Job"><code>{item.job_name.replace("nexora.","")}</code></td>
              <td data-label="Status"><span className={"status "+statusClass(item.status)}>{item.status}</span>{item.error_message&&<small className="table-error" title={item.error_message}>{item.error_message}</small>}</td>
              <td data-label="Duration">{formatDuration(item.duration_ms)}</td>
              <td data-label="Result"><span className="table-summary" title={summarizeObject(item.result)}>{summarizeObject(item.result)}</span></td>
              <td data-label="Worker"><code>{shortId(item.worker_name)}</code></td>
            </tr>)}</tbody>
          </table>
        </div>:<div className="empty compact-empty">No background job executions have been recorded yet.</div>}
        <Pagination page={jobsPage} pageSize={PAGE_SIZE} totalItems={jobs.length} onPageChange={setJobsPage}/>
      </section>

      <section className="content-card">
        <div className="content-card-head">
          <div><span className="card-eyebrow">Audit trail</span><h2>Recent control-plane activity</h2></div>
          <span className="method-badge">{audit.length} loaded</span>
        </div>
        {audit.length?<div className="data-table-wrap">
          <table className="data-table admin-audit-table">
            <thead><tr><th>Time</th><th>Action</th><th>Resource</th><th>Actor</th><th>Metadata</th></tr></thead>
            <tbody>{pagedAudit.map(item=><tr key={item.id}>
              <td data-label="Time">{new Date(item.created_at).toLocaleString()}</td>
              <td data-label="Action"><code>{item.action}</code></td>
              <td data-label="Resource">{item.resource_type}<small className="cell-subtle">{shortId(item.resource_id)}</small></td>
              <td data-label="Actor"><code>{shortId(item.actor_user_id)}</code></td>
              <td data-label="Metadata"><span className="table-summary" title={summarizeObject(item.metadata)}>{summarizeObject(item.metadata)}</span></td>
            </tr>)}</tbody>
          </table>
        </div>:<div className="empty compact-empty">No audit events are available.</div>}
        <Pagination page={auditPage} pageSize={PAGE_SIZE} totalItems={audit.length} onPageChange={setAuditPage}/>
      </section>
    </>}
  </div>;
}
