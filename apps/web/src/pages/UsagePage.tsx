import React,{useEffect,useMemo,useState} from "react";
import {api,UsageDaily} from "../api";

function shortId(value:string|null){
  if(!value)return "—";
  return value.length>16?value.slice(0,8)+"…"+value.slice(-4):value;
}

export function UsagePage(){
  const [rows,setRows]=useState<UsageDaily[]>([]);
  const [loading,setLoading]=useState(true);
  const [error,setError]=useState("");

  async function load(){
    setLoading(true);setError("");
    try{setRows(await api.usage())}
    catch(err){setError(err instanceof Error?err.message:String(err))}
    finally{setLoading(false)}
  }

  useEffect(()=>{void load()},[]);

  const totals=useMemo(()=>rows.reduce((acc,row)=>({
    requests:acc.requests+Number(row.requests||0),
    prompt:acc.prompt+Number(row.prompt_tokens||0),
    completion:acc.completion+Number(row.completion_tokens||0),
  }),{requests:0,prompt:0,completion:0}),[rows]);

  const maxRequests=Math.max(1,...rows.map(row=>Number(row.requests||0)));

  return <div className="page-stack">
    <div className="section-toolbar">
      <div>
        <p className="section-copy">Aggregated daily usage across API keys you own.</p>
        <small className="toolbar-meta">Latest {rows.length} usage dimensions</small>
      </div>
      <button onClick={load} disabled={loading}>{loading?"Refreshing…":"Refresh"}</button>
    </div>

    {error&&<p className="error">{error}</p>}

    <section className="overview-grid">
      <article className="stat-card"><span>Requests</span><strong>{totals.requests.toLocaleString()}</strong><small>Across loaded rows</small></article>
      <article className="stat-card"><span>Prompt tokens</span><strong>{totals.prompt.toLocaleString()}</strong><small>Input consumption</small></article>
      <article className="stat-card"><span>Completion tokens</span><strong>{totals.completion.toLocaleString()}</strong><small>Generated output</small></article>
      <article className="stat-card"><span>Total tokens</span><strong>{(totals.prompt+totals.completion).toLocaleString()}</strong><small>Prompt + completion</small></article>
    </section>

    {loading&&!rows.length?<div className="loading-card"><span className="spinner"/>Loading usage…</div>:rows.length?<section className="content-card">
      <div className="content-card-head">
        <div><span className="card-eyebrow">Daily usage</span><h2>Recent consumption</h2></div>
      </div>
      <div className="data-table-wrap">
        <table className="data-table">
          <thead><tr><th>Date</th><th>API key</th><th>Model</th><th>Requests</th><th>Prompt</th><th>Completion</th></tr></thead>
          <tbody>{rows.map((row,index)=><tr key={row.usage_date+"-"+row.api_key_id+"-"+(row.model_id||index)}>
            <td data-label="Date">{new Date(row.usage_date).toLocaleDateString()}</td>
            <td data-label="API key"><code>{shortId(row.api_key_id)}</code></td>
            <td data-label="Model"><code>{shortId(row.model_id)}</code></td>
            <td data-label="Requests">
              <div className="usage-cell"><strong>{Number(row.requests).toLocaleString()}</strong><span className="mini-bar"><span style={{width:`${Math.max(4,Number(row.requests)/maxRequests*100)}%`}}/></span></div>
            </td>
            <td data-label="Prompt">{Number(row.prompt_tokens).toLocaleString()}</td>
            <td data-label="Completion">{Number(row.completion_tokens).toLocaleString()}</td>
          </tr>)}</tbody>
        </table>
      </div>
    </section>:!loading&&!error?<div className="empty-state-card">
      <div className="empty-state-icon" aria-hidden="true">U</div>
      <h2>No usage yet</h2>
      <p>Send requests through the gateway. Daily aggregates will appear here after the worker processes usage events.</p>
    </div>:null}
  </div>;
}
