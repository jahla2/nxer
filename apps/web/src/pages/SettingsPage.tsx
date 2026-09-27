import React,{useEffect,useState} from "react";
import {api,ConsoleSettings} from "../api";

export function SettingsPage(){
  const [settings,setSettings]=useState<ConsoleSettings|null>(null);
  const [loading,setLoading]=useState(true);
  const [error,setError]=useState("");

  async function load(){
    setLoading(true);setError("");
    try{setSettings(await api.settings())}
    catch(err){setError(err instanceof Error?err.message:String(err))}
    finally{setLoading(false)}
  }

  useEffect(()=>{void load()},[]);

  if(loading&&!settings)return <div className="loading-card"><span className="spinner"/>Loading settings…</div>;

  return <div className="page-stack">
    <div className="section-toolbar">
      <div><p className="section-copy">Account identity and server-managed session policy.</p></div>
      <button onClick={load} disabled={loading}>{loading?"Refreshing…":"Refresh"}</button>
    </div>
    {error&&<p className="error">{error}</p>}

    {settings&&<section className="settings-grid">
      <article className="content-card">
        <div className="content-card-head"><div><span className="card-eyebrow">Profile</span><h2>Account details</h2></div></div>
        <dl className="definition-list">
          <div><dt>Name</dt><dd>{settings.profile.display_name}</dd></div>
          <div><dt>Email</dt><dd>{settings.profile.email}</dd></div>
          <div><dt>Role</dt><dd><span className="status">{settings.profile.role}</span></dd></div>
          <div><dt>Email verification</dt><dd><span className={"status "+(settings.profile.email_verified?"active":"pending")}>{settings.profile.email_verified?"Verified":"Pending"}</span></dd></div>
          <div><dt>User ID</dt><dd><code>{settings.profile.id}</code></dd></div>
        </dl>
      </article>

      <article className="content-card">
        <div className="content-card-head"><div><span className="card-eyebrow">Security</span><h2>Session policy</h2></div></div>
        <dl className="definition-list">
          <div><dt>Access session</dt><dd>{settings.session.access_ttl_minutes} minutes</dd></div>
          <div><dt>Refresh session</dt><dd>{settings.session.refresh_ttl_days} days</dd></div>
          <div><dt>Authentication</dt><dd>Secure HttpOnly cookies</dd></div>
          <div><dt>Mutation protection</dt><dd>CSRF token required</dd></div>
        </dl>
        <div className="security-note">
          <strong>Session secrets remain server-side.</strong>
          <p>The browser does not receive refresh-token hashes, API-key hashes or provider credentials.</p>
        </div>
      </article>
    </section>}
  </div>;
}
