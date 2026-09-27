import React,{useEffect,useMemo,useState} from "react";
import {api,RequestEvent} from "../api";

function shortId(value:string|null){
  if(!value)return "—";
  return value.length>20?value.slice(0,12)+"…"+value.slice(-4):value;
}

function statusClass(status:number){
  if(status>=200&&status<300)return "success";
  if(status>=400&&status<500)return "warning";
  return "failure";
}

export function RequestsPage(){
  const [rows,setRows]=useState<RequestEvent[]>([]);
  const [loading,setLoading]=useState(true);
  const [error,setError]=useState("");

  async function load(){
    setLoading(true);setError("");
    try{setRows(await api.requests())}
    catch(err){setError(err instanceof Error?err.message:String(err))}
    finally{setLoading(false)}
  }

  useEffect(()=>{void load()},[]);

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
      <div className="data-table-wrap">
        <table className="data-table request-table">
          <thead><tr><th>Time</th><th>Request</th><th>Status</th><th>Latency</th><th>TTFT</th><th>Tokens</th></tr></thead>
          <tbody>{rows.map(row=><tr key={row.request_id}>
            <td data-label="Time">{new Date(row.created_at).toLocaleString()}</td>
            <td data-label="Request"><code title={row.request_id}>{shortId(row.request_id)}</code></td>
            <td data-label="Status"><span className={"http-status "+statusClass(row.status)}>{row.status}</span></td>
            <td data-label="Latency">{row.latency_ms==null?"—":`${row.latency_ms} ms`}</td>
            <td data-label="TTFT">{row.ttft_ms==null?"—":`${row.ttft_ms} ms`}</td>
            <td data-label="Tokens">{((row.prompt_tokens||0)+(row.completion_tokens||0)).toLocaleString()}</td>
          </tr>)}</tbody>
        </table>
      </div>
    </section>:!loading&&!error?<div className="empty-state-card">
      <div className="empty-state-icon" aria-hidden="true">R</div>
      <h2>No requests recorded</h2>
      <p>Gateway metadata appears here after requests complete or fail.</p>
    </div>:null}
  </div>;
}
