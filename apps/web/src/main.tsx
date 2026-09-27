import React, { useState } from "react";
import { createRoot } from "react-dom/client";
import "./styles.css";

const modules = ["Overview","Projects","API Keys","Models","Playground","Usage","Requests","Docs","Status","Settings","Admin"];

function App() {
  const [active,setActive]=useState("Overview");
  return <main className="shell">
    <header><div><p className="eyebrow">NEXORA AI</p><h1>Developer Console</h1><p className="subtitle">OpenAI-compatible free-model gateway for reusable project integrations.</p></div><span className="badge">0.1.0-alpha</span></header>
    <nav aria-label="Console modules">{modules.map(name=><button key={name} className={active===name?"active":""} onClick={()=>setActive(name)}>{name}</button>)}</nav>
    <section className="panel">
      <p className="eyebrow">{active}</p>
      <h2>{active==="Overview"?"Gateway control center":active}</h2>
      <p>{copy[active]}</p>
      {active==="Playground" && <pre>POST /v1/chat/completions{"\n"}Authorization: Bearer nxa_live_...</pre>}
      {active==="API Keys" && <p className="notice">Create, scope and revoke keys through the authenticated Control API. Raw keys are shown once.</p>}
      {active==="Status" && <div className="status"><span /> Gateway readiness is available at <code>/ready</code>.</div>}
    </section>
  </main>;
}
const copy:Record<string,string>={
 Overview:"Inspect gateway configuration, free-model availability and operational status.",
 Projects:"Organize API keys and usage by reusable client project.",
 "API Keys":"Manage Nexora credentials without exposing upstream provider secrets.",
 Models:"Browse the dynamically verified zero-cost text model catalog.",
 Playground:"Test the OpenAI-compatible endpoint using a Nexora API key.",
 Usage:"Review request and token aggregates produced by background workers.",
 Requests:"Inspect sanitized request metadata and operational outcomes.",
 Docs:"Integration guidance for JavaScript, Python and curl clients.",
 Status:"Check gateway, PostgreSQL and Redis readiness.",
 Settings:"Manage safe project-level defaults and limits.",
 Admin:"Administrative controls for catalog and global safety settings."
};
createRoot(document.getElementById("root")!).render(<React.StrictMode><App /></React.StrictMode>);
