import React from "react";
import { createRoot } from "react-dom/client";
import "./styles.css";

const modules = ["Overview","Projects","API Keys","Models","Playground","Usage","Requests","Documentation","Status","Settings"];

function App() {
  return <main className="shell">
    <header><div><p className="eyebrow">NEXORA AI</p><h1>Developer Console</h1><p className="subtitle">OpenAI-compatible free-model gateway for reusable project integrations.</p></div><span className="badge">0.1.0-alpha</span></header>
    <section className="grid">{modules.map((name) => <article className="card" key={name}><h2>{name}</h2><p>Module scaffold ready for control-plane integration.</p></article>)}</section>
  </main>;
}
createRoot(document.getElementById("root")!).render(<React.StrictMode><App /></React.StrictMode>);
