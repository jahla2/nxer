import React,{useEffect,useMemo,useState} from "react";
import {api,ConsoleModel,RequestEvent} from "../api";
import {Pagination} from "../components/Pagination";

const PAGE_SIZE=20;

function shortId(value:string|null){
  if(!value)return "—";
  return value.length>20?value.slice(0,12)+"…"+value.slice(-4):value;
}

function statusClass(status:number){
  if(status>=200&&status<300)return "success";
  if(status>=400&&status<500)return "warning";
  return "failure";
}

export function RequestsPage({models}:{models:ConsoleModel[]}){
  const [rows,setRows]=useState<RequestEvent[]>([]);
  const [loading,setLoading]=useState(true);
  const [error,setError]=useState("");
  const [page,setPage]=useState(1);
  const [statusFilter,setStatusFilter]=useState("all");
  const [modelFilter,setModelFilter]=useState("all");

  async function load(){
    setLoading(true);setError("");
    try{setRows(await api.requests());setPage(1)}
    catch(err){setError(err instanceof Error?err.message:String(err))}
    finally{setLoading(false)}
  }

  useEffect(()=>{void load()},[]);

  const modelById=useMemo(()=>new Map(models.map(model=>[model.id,model])),[models]);
  const presentModelIds=useMemo(()=>Array.from(new Set(rows.map(row=>row.model_id).filter((id):id is string=>Boolean(id)))),[rows]);

  const filteredRows=useMemo(()=>rows.filter(row=>{
    if(statusFilter==="success"&&!(row.status>=200&&row.status<300))return false;
    if(statusFilter==="failure"&&row.status>=200&&row.status<300)return false;
    if(modelFilter!=="all"&&row.model_id!==modelFilter)return false;
    return true;
  }),[rows,statusFilter,modelFilter]);

  useEffect(()=>{setPage(1)},[statusFilter,modelFilter]);

  const pageRows=useMemo(()=>filteredRows.slice((page-1)*PAGE_SIZE,page*PAGE_SIZE),[filteredRows,page]);

  const summary=useMemo(()=>{
    const success=rows.filter(row=>row.status>=200&&row.status<300).length;
    const avgLatency=rows.length?Math.round(rows.reduce((sum,row)=>sum+Number(row.latency_ms||0),0)/rows.length):0;
    const avgTtft=rows.filter(row=>row.ttft_ms!=null);
    return {
      success,
      failed:rows.length-success,
      avgLatency,
      avgTtft:avgTtft.length?Math.round(avgTtft.reduce((sum,row)=>sum+Number(row.ttft_ms||0),0)/avgTtft.length):0,
    };
  },[rows]);

  return <div className="page-stack">
    <div className="section-toolbar">
      <div>
        <p className="section-copy">Recent gateway request metadata. Prompt and completion text are not logged.</p>
        <small className="toolbar-meta">Up to 100 most recent requests</small>
      </div>
      <button onClick={load} disabled={loading}>{loading?"Refreshing…":"Refresh"}</button>
    </div>

    {error&&<p className="error">{error}</p>}

    <section className="overview-grid">
      <article className="stat-card"><span>Successful</span><strong>{summary.success}</strong><small>2xx responses</small></article>
      <article className="stat-card"><span>Non-success</span><strong>{summary.failed}</strong><small>All other statuses</small></article>
      <article className="stat-card"><span>Avg latency</span><strong>{summary.avgLatency} ms</strong><small>Loaded requests</small></article>
      <article className="stat-card"><span>Avg TTFT</span><strong>{summary.avgTtft} ms</strong><small>Where reported</small></article>
    </section>

    {loading&&!rows.length?<div className="loading-card"><span className="spinner"/>Loading requests…</div>:rows.length?<section className="content-card">
      <div className="content-card-head">
        <div><span className="card-eyebrow">Request log</span><h2>Recent gateway traffic</h2></div>
      </div>
      <div className="table-toolbar">
        <label className="inline-control">
          <span>Status</span>
          <select value={statusFilter} onChange={event=>setStatusFilter(event.target.value)}>
            <option value="all">All statuses</option>
            <option value="success">Success (2xx)</option>
            <option value="failure">Failure</option>
          </select>
        </label>
        <label className="inline-control">
          <span>Model</span>
          <select value={modelFilter} onChange={event=>setModelFilter(event.target.value)}>
            <option value="all">All models</option>
            {presentModelIds.map(id=><option key={id} value={id}>{modelById.get(id)?.display_name??shortId(id)}</option>)}
          </select>
        </label>
        {(statusFilter!=="all"||modelFilter!=="all")&&<button type="button" className="text-action" onClick={()=>{setStatusFilter("all");setModelFilter("all")}}>Clear filters</button>}
      </div>
      {filteredRows.length?<div className="data-table-wrap">
        <table className="data-table request-table">
          <thead><tr><th>Time</th><th>Request</th><th>Model</th><th>Status</th><th>Latency</th><th>TTFT</th><th>Tokens</th></tr></thead>
          <tbody>{pageRows.map(row=><tr key={row.request_id}>
            <td data-label="Time">{new Date(row.created_at).toLocaleString()}</td>
            <td data-label="Request"><code title={row.request_id}>{shortId(row.request_id)}</code></td>
            <td data-label="Model">{row.model_id?modelById.get(row.model_id)?.display_name??shortId(row.model_id):"—"}</td>
            <td data-label="Status"><span className={"http-status "+statusClass(row.status)}>{row.status}</span></td>
            <td data-label="Latency">{row.latency_ms==null?"—":`${row.latency_ms} ms`}</td>
            <td data-label="TTFT">{row.ttft_ms==null?"—":`${row.ttft_ms} ms`}</td>
            <td data-label="Tokens">{((row.prompt_tokens||0)+(row.completion_tokens||0)).toLocaleString()}</td>
          </tr>)}</tbody>
        </table>
      </div>:<div className="empty compact-empty">No requests match the current filters.</div>}
      <Pagination page={page} pageSize={PAGE_SIZE} totalItems={filteredRows.length} onPageChange={setPage}/>
    </section>:!loading&&!error?<div className="empty-state-card">
      <div className="empty-state-icon" aria-hidden="true">R</div>
      <h2>No requests recorded</h2>
      <p>Gateway metadata appears here after requests complete or fail.</p>
    </div>:null}
  </div>;
}
