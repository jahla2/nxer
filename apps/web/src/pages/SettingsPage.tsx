import React,{useEffect,useState} from "react";
import {api,ConsoleSettings,User} from "../api";

export function SettingsPage({onUserUpdated}:{onUserUpdated:(user:User)=>void}){
  const [settings,setSettings]=useState<ConsoleSettings|null>(null);
  const [loading,setLoading]=useState(true);
  const [error,setError]=useState("");
  const [verificationBusy,setVerificationBusy]=useState(false);
  const [verificationMessage,setVerificationMessage]=useState("");
  const [devVerificationToken,setDevVerificationToken]=useState("");
  const [passwordResetBusy,setPasswordResetBusy]=useState(false);
  const [passwordResetMessage,setPasswordResetMessage]=useState("");

  async function load(){
    setLoading(true);setError("");
    try{setSettings(await api.settings())}
    catch(err){setError(err instanceof Error?err.message:String(err))}
    finally{setLoading(false)}
  }

  useEffect(()=>{void load()},[]);

  async function requestVerification(){
    setVerificationBusy(true);setError("");setVerificationMessage("");setDevVerificationToken("");
    try{
      const response=await api.requestEmailVerification();
      setVerificationMessage(response.message);
      if(response.verification_token)setDevVerificationToken(response.verification_token);
    }catch(err){
      setError(err instanceof Error?err.message:String(err));
    }finally{
      setVerificationBusy(false);
    }
  }

  async function confirmDevelopmentVerification(){
    if(!devVerificationToken)return;
    setVerificationBusy(true);setError("");
    try{
      const verified=await api.confirmEmailVerification(devVerificationToken);
      onUserUpdated(verified);
      setVerificationMessage("Email verified successfully.");
      setDevVerificationToken("");
      await load();
    }catch(err){
      setError(err instanceof Error?err.message:String(err));
    }finally{
      setVerificationBusy(false);
    }
  }

  async function requestPasswordReset(){
    if(!settings)return;
    setPasswordResetBusy(true);setError("");setPasswordResetMessage("");
    try{
      const response=await api.requestPasswordReset(settings.profile.email);
      setPasswordResetMessage(response.message);
      if(response.reset_token){
        const url=new URL(window.location.href);
        url.searchParams.set("reset_password",response.reset_token);
        window.location.assign(url.toString());
      }
    }catch(err){
      setError(err instanceof Error?err.message:String(err));
    }finally{
      setPasswordResetBusy(false);
    }
  }

  if(loading&&!settings)return <div className="loading-card"><span className="spinner"/>Loading settings…</div>;

  return <div className="page-stack">
    <div className="section-toolbar">
      <div><p className="section-copy">Account identity, email verification and server-managed session policy.</p></div>
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

        {!settings.profile.email_verified&&<div className="settings-action-card">
          <div>
            <strong>Verify your email address</strong>
            <p>Nexora sends a time-limited verification link to your account email.</p>
          </div>
          <button className="primary" disabled={verificationBusy} onClick={requestVerification}>{verificationBusy?"Sending…":"Send verification email"}</button>
          {verificationMessage&&<p className="success-message settings-action-message">{verificationMessage}</p>}
          {devVerificationToken&&<button disabled={verificationBusy} onClick={confirmDevelopmentVerification}>Complete local verification</button>}
        </div>}
      </article>

      <article className="content-card">
        <div className="content-card-head"><div><span className="card-eyebrow">Security</span><h2>Session & password</h2></div></div>
        <dl className="definition-list">
          <div><dt>Access session</dt><dd>{settings.session.access_ttl_minutes} minutes</dd></div>
          <div><dt>Refresh session</dt><dd>{settings.session.refresh_ttl_days} days</dd></div>
          <div><dt>Authentication</dt><dd>Secure HttpOnly cookies</dd></div>
          <div><dt>Mutation protection</dt><dd>CSRF token required</dd></div>
        </dl>

        <div className="settings-action-card compact-action">
          <div>
            <strong>Reset your password</strong>
            <p>Send a time-limited reset link to {settings.profile.email}. A completed reset revokes all active sessions.</p>
          </div>
          <button disabled={passwordResetBusy} onClick={requestPasswordReset}>{passwordResetBusy?"Sending…":"Send reset link"}</button>
          {passwordResetMessage&&<p className="success-message settings-action-message">{passwordResetMessage}</p>}
        </div>

        <div className="security-note">
          <strong>Session secrets remain server-side.</strong>
          <p>The browser does not receive refresh-token hashes, API-key hashes or provider credentials.</p>
        </div>
      </article>
    </section>}
  </div>;
}
