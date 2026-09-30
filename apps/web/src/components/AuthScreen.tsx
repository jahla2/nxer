import React,{useEffect,useState} from "react";
import {api,User} from "../api";
import type {AuthRouteMode} from "../routing";
import {DotGridBackground} from "./DotGridBackground";

type SuccessState={title:string;body:string}|null;

export function AuthScreen({
  mode,
  sessionUser,
  resetToken:resetTokenProp="",
  verifyToken="",
  onAuthenticated,
  onPasswordResetComplete,
  onNavigate,
  onActionComplete,
}:{
  mode:AuthRouteMode;
  sessionUser:User|null;
  resetToken?:string;
  verifyToken?:string;
  onAuthenticated:(user:User)=>void;
  onPasswordResetComplete:()=>void;
  onNavigate:(mode:AuthRouteMode,token?:string)=>void;
  onActionComplete:()=>void;
}){
  const [email,setEmail]=useState(sessionUser?.email||"");
  const [password,setPassword]=useState("");
  const [displayName,setDisplayName]=useState("");
  const [resetToken,setResetToken]=useState(resetTokenProp);
  const [newPassword,setNewPassword]=useState("");
  const [confirmPassword,setConfirmPassword]=useState("");
  const [busy,setBusy]=useState(false);
  const [error,setError]=useState("");
  const [notice,setNotice]=useState("");
  const [success,setSuccess]=useState<SuccessState>(null);
  const [showPassword,setShowPassword]=useState(false);

  useEffect(()=>{
    setError("");
    setNotice("");
    setPassword("");
    setSuccess(null);
    setShowPassword(false);
    setResetToken(resetTokenProp);
  },[mode,resetTokenProp,verifyToken]);

  async function submitAuth(event:React.FormEvent){
    event.preventDefault();
    setBusy(true);setError("");setNotice("");
    try{
      const user=mode==="register"
        ?await api.register(displayName,email,password)
        :await api.login(email,password);
      onAuthenticated(user);
    }catch(err){
      setError(err instanceof Error?err.message:String(err));
    }finally{
      setBusy(false);
    }
  }

  async function requestReset(event:React.FormEvent){
    event.preventDefault();
    setBusy(true);setError("");setNotice("");
    try{
      const response=await api.requestPasswordReset(email);
      if(response.reset_token){
        setNotice("Development mode: the one-time reset token was loaded automatically.");
        onNavigate("reset",response.reset_token);
      }else{
        setNotice(response.message);
      }
    }catch(err){
      setError(err instanceof Error?err.message:String(err));
    }finally{
      setBusy(false);
    }
  }

  async function confirmReset(event:React.FormEvent){
    event.preventDefault();
    setError("");setNotice("");
    if(newPassword.length<12){
      setError("Use at least 12 characters for the new password.");
      return;
    }
    if(newPassword!==confirmPassword){
      setError("Passwords do not match.");
      return;
    }
    if(!resetToken){
      setError("Password reset token is missing.");
      return;
    }

    setBusy(true);
    try{
      await api.confirmPasswordReset(resetToken,newPassword);
      onPasswordResetComplete();
      setSuccess({
        title:"Password updated",
        body:"Your existing sessions were revoked. Sign in again with your new password.",
      });
      setNewPassword("");
      setConfirmPassword("");
    }catch(err){
      setError(err instanceof Error?err.message:String(err));
    }finally{
      setBusy(false);
    }
  }

  async function confirmVerification(){
    if(!verifyToken){
      setError("Email verification token is missing.");
      return;
    }
    setBusy(true);setError("");
    try{
      const verified=await api.confirmEmailVerification(verifyToken);
      if(sessionUser){
        onAuthenticated(verified);
        onActionComplete();
        return;
      }
      setSuccess({
        title:"Email verified",
        body:"Your Nexora email address is verified. You can sign in and continue.",
      });
    }catch(err){
      setError(err instanceof Error?err.message:String(err));
    }finally{
      setBusy(false);
    }
  }

  if(success){
    return <main className="auth-shell" id="main-content">
      <DotGridBackground/>
      <section className="panel auth-panel auth-result-panel" aria-labelledby="auth-success-title">
        <div className="auth-result-icon" aria-hidden="true">✓</div>
        <p className="eyebrow">NEXORA AI</p>
        <h1 id="auth-success-title">{success.title}</h1>
        <p className="subtitle">{success.body}</p>
        <button className="primary auth-primary" onClick={()=>onNavigate("login")}>Sign in</button>
      </section>
    </main>;
  }

  if(mode==="verify"){
    return <main className="auth-shell" id="main-content">
      <DotGridBackground/>
      <section className="panel auth-panel auth-result-panel" aria-labelledby="verify-email-title">
        <div className="auth-result-icon verification" aria-hidden="true">✉</div>
        <p className="eyebrow">EMAIL VERIFICATION</p>
        <h1 id="verify-email-title">Verify your email</h1>
        <p className="subtitle">Confirm this one-time verification link to mark your Nexora account email as verified.</p>
        {error&&<p className="error" role="alert">{error}</p>}
        <button className="primary auth-primary" disabled={busy} onClick={confirmVerification}>{busy?"Verifying…":"Verify email"}</button>
        <button className="link-button" onClick={onActionComplete}>Cancel</button>
      </section>
    </main>;
  }

  if(mode==="reset"){
    return <main className="auth-shell" id="main-content">
      <DotGridBackground/>
      <section className="panel auth-panel" aria-labelledby="reset-password-title">
        <p className="eyebrow">PASSWORD RECOVERY</p>
        <h1 id="reset-password-title">Set a new password</h1>
        <p className="subtitle">Choose a new password for your Nexora account. Existing sessions will be revoked.</p>
        <form onSubmit={confirmReset}>
          {!resetTokenProp&&<label>Reset token<input value={resetToken} onChange={event=>setResetToken(event.target.value)} autoComplete="off" required data-autofocus="true"/></label>}
          <label>New password<input type="password" value={newPassword} onChange={event=>setNewPassword(event.target.value)} minLength={12} autoComplete="new-password" required data-autofocus={resetTokenProp?"true":undefined}/></label>
          <label>Confirm password<input type="password" value={confirmPassword} onChange={event=>setConfirmPassword(event.target.value)} minLength={12} autoComplete="new-password" required/></label>
          <p className="form-hint">Use at least 12 characters. A successful reset signs out all active sessions.</p>
          {notice&&<p className="success-message" role="status">{notice}</p>}
          {error&&<p className="error" role="alert">{error}</p>}
          <button disabled={busy} type="submit">{busy?"Updating…":"Update password"}</button>
        </form>
        <button className="link-button" onClick={()=>onNavigate("login")}>Back to sign in</button>
      </section>
    </main>;
  }

  if(mode==="forgot"){
    return <main className="auth-shell" id="main-content">
      <DotGridBackground/>
      <section className="panel auth-panel" aria-labelledby="forgot-password-title">
        <p className="eyebrow">PASSWORD RECOVERY</p>
        <h1 id="forgot-password-title">Reset your password</h1>
        <p className="subtitle">Enter your account email. If it exists, Nexora will send a time-limited reset link.</p>
        <form onSubmit={requestReset}>
          <label>Email<input type="email" value={email} onChange={event=>setEmail(event.target.value)} autoComplete="email" required data-autofocus="true"/></label>
          {notice&&<p className="success-message" role="status">{notice}</p>}
          {error&&<p className="error" role="alert">{error}</p>}
          <button disabled={busy} type="submit">{busy?"Sending…":"Send reset link"}</button>
        </form>
        <button className="link-button" onClick={()=>onNavigate("login")}>Back to sign in</button>
      </section>
    </main>;
  }

  return <main className="auth-shell" id="main-content">
    <DotGridBackground/>
    <section className="panel auth-panel" aria-labelledby="auth-title">
      <p className="eyebrow">NEXORA AI</p>
      <h1 id="auth-title">{mode==="login"?"Sign in":"Create account"}</h1>
      <p className="subtitle">{mode==="login"?"Access your Nexora developer console.":"Create your developer account and first project."}</p>
      <form className="auth-form" onSubmit={submitAuth}>
        {mode==="register"&&<label className="field">
          <span className="field-label">Full name</span>
          <span className="field-control">
            <UserIcon/>
            <input value={displayName} onChange={event=>setDisplayName(event.target.value)} minLength={2} autoComplete="name" required data-autofocus="true" placeholder="Ada Lovelace"/>
          </span>
        </label>}
        <label className="field">
          <span className="field-label">Email</span>
          <span className="field-control">
            <MailIcon/>
            <input type="email" value={email} onChange={event=>setEmail(event.target.value)} autoComplete="email" required data-autofocus={mode==="login"?"true":undefined} placeholder="you@company.com"/>
          </span>
        </label>
        <label className="field">
          <span className="field-label">Password</span>
          <span className="field-control">
            <LockIcon/>
            <input type={showPassword?"text":"password"} value={password} onChange={event=>setPassword(event.target.value)} minLength={mode==="register"?12:1} autoComplete={mode==="register"?"new-password":"current-password"} required placeholder="••••••••••••"/>
            <button type="button" className="field-toggle" aria-label={showPassword?"Hide password":"Show password"} aria-pressed={showPassword} onClick={()=>setShowPassword(value=>!value)}>
              {showPassword?<EyeOffIcon/>:<EyeIcon/>}
            </button>
          </span>
          {mode==="login"&&<button type="button" className="field-inline-link field-below-link" onClick={()=>onNavigate("forgot")}>Forgot password?</button>}
        </label>
        {mode==="register"&&<p className="form-hint">Use at least 12 characters. Nexora will create a verification link for your email address.</p>}
        {error&&<p className="error" role="alert"><ErrorIcon/><span>{error}</span></p>}
        <button className="auth-submit" disabled={busy} type="submit">
          {busy&&<span className="spinner" aria-hidden="true"/>}
          {busy?"Please wait…":mode==="login"?"Sign in":"Create account"}
        </button>
      </form>
      <p className="auth-switch">
        {mode==="login"?"Don't have an account?":"Already have an account?"}{" "}
        <button className="link-button" onClick={()=>onNavigate(mode==="login"?"register":"login")}>
          {mode==="login"?"Sign up":"Sign in"}
        </button>
      </p>
    </section>
  </main>;
}

function MailIcon(){
  return <svg viewBox="0 0 20 20" fill="none" aria-hidden="true"><path d="M3 5.5A1.5 1.5 0 0 1 4.5 4h11A1.5 1.5 0 0 1 17 5.5v9a1.5 1.5 0 0 1-1.5 1.5h-11A1.5 1.5 0 0 1 3 14.5v-9Z" stroke="currentColor" strokeWidth="1.4"/><path d="m3.5 5.5 6.5 5 6.5-5" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round"/></svg>;
}
function LockIcon(){
  return <svg viewBox="0 0 20 20" fill="none" aria-hidden="true"><rect x="4" y="8.5" width="12" height="8" rx="1.6" stroke="currentColor" strokeWidth="1.4"/><path d="M6.5 8.5V6a3.5 3.5 0 0 1 7 0v2.5" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round"/></svg>;
}
function UserIcon(){
  return <svg viewBox="0 0 20 20" fill="none" aria-hidden="true"><circle cx="10" cy="6.5" r="3" stroke="currentColor" strokeWidth="1.4"/><path d="M3.75 16.25c.9-3.1 3.4-4.75 6.25-4.75s5.35 1.65 6.25 4.75" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round"/></svg>;
}
function EyeIcon(){
  return <svg viewBox="0 0 20 20" fill="none" aria-hidden="true"><path d="M1.5 10S4.5 4.5 10 4.5 18.5 10 18.5 10 15.5 15.5 10 15.5 1.5 10 1.5 10Z" stroke="currentColor" strokeWidth="1.4" strokeLinejoin="round"/><circle cx="10" cy="10" r="2.4" stroke="currentColor" strokeWidth="1.4"/></svg>;
}
function EyeOffIcon(){
  return <svg viewBox="0 0 20 20" fill="none" aria-hidden="true"><path d="M2.5 2.5l15 15" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round"/><path d="M8.35 5.06A8.9 8.9 0 0 1 10 4.9c5.5 0 8.5 5.5 8.5 5.5a14.4 14.4 0 0 1-2.9 3.66M5.3 6.34C3.05 7.86 1.5 10 1.5 10s3 5.5 8.5 5.5c1 0 1.9-.16 2.72-.46" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round"/><path d="M8.1 10a1.9 1.9 0 0 0 2.85 1.85" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round"/></svg>;
}
function ErrorIcon(){
  return <svg viewBox="0 0 20 20" fill="none" aria-hidden="true" style={{flex:"0 0 auto"}}><circle cx="10" cy="10" r="7.5" stroke="currentColor" strokeWidth="1.4"/><path d="M10 6.5v4" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round"/><circle cx="10" cy="13.2" r=".9" fill="currentColor"/></svg>;
}
