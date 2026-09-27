import React,{useMemo,useState} from "react";
import {api,User} from "../api";

type AuthMode="login"|"register"|"forgot"|"reset"|"verify"|"success";

function initialAction(){
  const params=new URLSearchParams(window.location.search);
  const resetToken=params.get("reset_password")||"";
  const verifyToken=params.get("verify_email")||"";
  return {
    resetToken,
    verifyToken,
    mode:(verifyToken?"verify":resetToken?"reset":"login") as AuthMode,
  };
}

function clearActionQuery(){
  const url=new URL(window.location.href);
  url.searchParams.delete("reset_password");
  url.searchParams.delete("verify_email");
  window.history.replaceState({},"",url.pathname+url.search+url.hash);
}

export function AuthScreen({
  sessionUser,
  onAuthenticated,
  onPasswordResetComplete,
  onActionComplete,
}:{
  sessionUser:User|null;
  onAuthenticated:(user:User)=>void;
  onPasswordResetComplete:()=>void;
  onActionComplete:()=>void;
}){
  const initial=useMemo(initialAction,[]);
  const [mode,setMode]=useState<AuthMode>(initial.mode);
  const [email,setEmail]=useState(sessionUser?.email||"");
  const [password,setPassword]=useState("");
  const [displayName,setDisplayName]=useState("");
  const [resetToken,setResetToken]=useState(initial.resetToken);
  const [verifyToken]=useState(initial.verifyToken);
  const [newPassword,setNewPassword]=useState("");
  const [confirmPassword,setConfirmPassword]=useState("");
  const [busy,setBusy]=useState(false);
  const [error,setError]=useState("");
  const [notice,setNotice]=useState("");
  const [successTitle,setSuccessTitle]=useState("");
  const [successBody,setSuccessBody]=useState("");

  function switchMode(next:AuthMode){
    setMode(next);
    setError("");
    setNotice("");
    setPassword("");
  }

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
        setResetToken(response.reset_token);
        setNotice("Development mode: the one-time reset token was loaded automatically.");
        setMode("reset");
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
      clearActionQuery();
      onPasswordResetComplete();
      setSuccessTitle("Password updated");
      setSuccessBody("Your existing sessions were revoked. Sign in again with your new password.");
      setMode("success");
      setPassword("");
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
      clearActionQuery();
      if(sessionUser){
        onAuthenticated(verified);
        onActionComplete();
        return;
      }
      setSuccessTitle("Email verified");
      setSuccessBody("Your Nexora email address is verified. You can sign in and continue.");
      setMode("success");
    }catch(err){
      setError(err instanceof Error?err.message:String(err));
    }finally{
      setBusy(false);
    }
  }

  if(mode==="success"){
    return <main className="auth-shell">
      <section className="panel auth-panel auth-result-panel">
        <div className="auth-result-icon" aria-hidden="true">✓</div>
        <p className="eyebrow">NEXORA AI</p>
        <h1>{successTitle}</h1>
        <p className="subtitle">{successBody}</p>
        <button className="primary auth-primary" onClick={()=>{onActionComplete();switchMode("login")}}>Sign in</button>
      </section>
    </main>;
  }

  if(mode==="verify"){
    return <main className="auth-shell">
      <section className="panel auth-panel auth-result-panel">
        <div className="auth-result-icon verification" aria-hidden="true">✉</div>
        <p className="eyebrow">EMAIL VERIFICATION</p>
        <h1>Verify your email</h1>
        <p className="subtitle">Confirm this one-time verification link to mark your Nexora account email as verified.</p>
        {error&&<p className="error">{error}</p>}
        <button className="primary auth-primary" disabled={busy} onClick={confirmVerification}>{busy?"Verifying…":"Verify email"}</button>
        <button className="link-button" onClick={()=>{clearActionQuery();onActionComplete()}}>Cancel</button>
      </section>
    </main>;
  }

  if(mode==="reset"){
    return <main className="auth-shell">
      <section className="panel auth-panel">
        <p className="eyebrow">PASSWORD RECOVERY</p>
        <h1>Set a new password</h1>
        <p className="subtitle">Choose a new password for your Nexora account. Existing sessions will be revoked.</p>
        <form onSubmit={confirmReset}>
          {!initial.resetToken&&<label>Reset token<input value={resetToken} onChange={event=>setResetToken(event.target.value)} autoComplete="off" required/></label>}
          <label>New password<input type="password" value={newPassword} onChange={event=>setNewPassword(event.target.value)} minLength={12} autoComplete="new-password" required/></label>
          <label>Confirm password<input type="password" value={confirmPassword} onChange={event=>setConfirmPassword(event.target.value)} minLength={12} autoComplete="new-password" required/></label>
          <p className="form-hint">Use at least 12 characters. A successful reset signs out all active sessions.</p>
          {notice&&<p className="success-message">{notice}</p>}
          {error&&<p className="error">{error}</p>}
          <button disabled={busy} type="submit">{busy?"Updating…":"Update password"}</button>
        </form>
        <button className="link-button" onClick={()=>{clearActionQuery();switchMode("login");onActionComplete()}}>Back to sign in</button>
      </section>
    </main>;
  }

  if(mode==="forgot"){
    return <main className="auth-shell">
      <section className="panel auth-panel">
        <p className="eyebrow">PASSWORD RECOVERY</p>
        <h1>Reset your password</h1>
        <p className="subtitle">Enter your account email. If it exists, Nexora will send a time-limited reset link.</p>
        <form onSubmit={requestReset}>
          <label>Email<input type="email" value={email} onChange={event=>setEmail(event.target.value)} autoComplete="email" required/></label>
          {notice&&<p className="success-message">{notice}</p>}
          {error&&<p className="error">{error}</p>}
          <button disabled={busy} type="submit">{busy?"Sending…":"Send reset link"}</button>
        </form>
        <button className="link-button" onClick={()=>switchMode("login")}>Back to sign in</button>
      </section>
    </main>;
  }

  return <main className="auth-shell">
    <section className="panel auth-panel">
      <p className="eyebrow">NEXORA AI</p>
      <h1>{mode==="login"?"Sign in":"Create account"}</h1>
      <p className="subtitle">{mode==="login"?"Access your Nexora developer console.":"Create your developer account and first project."}</p>
      <form onSubmit={submitAuth}>
        {mode==="register"&&<label>Name<input value={displayName} onChange={event=>setDisplayName(event.target.value)} minLength={2} autoComplete="name" required/></label>}
        <label>Email<input type="email" value={email} onChange={event=>setEmail(event.target.value)} autoComplete="email" required/></label>
        <label>Password<input type="password" value={password} onChange={event=>setPassword(event.target.value)} minLength={mode==="register"?12:1} autoComplete={mode==="register"?"new-password":"current-password"} required/></label>
        {mode==="register"&&<p className="form-hint">Use at least 12 characters. Nexora will create a verification link for your email address.</p>}
        {error&&<p className="error">{error}</p>}
        <button disabled={busy} type="submit">{busy?"Please wait…":mode==="login"?"Sign in":"Create account"}</button>
      </form>
      <div className="auth-links">
        {mode==="login"&&<button className="link-button" onClick={()=>switchMode("forgot")}>Forgot password?</button>}
        <button className="link-button" onClick={()=>switchMode(mode==="login"?"register":"login")}>
          {mode==="login"?"Need an account? Register":"Already registered? Sign in"}
        </button>
      </div>
    </section>
  </main>;
}
