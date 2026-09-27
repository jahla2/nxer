import React,{createContext,useCallback,useContext,useEffect,useMemo,useState} from "react";
import {api,User} from "../api";

type SessionContextValue={
  user:User|null;
  loading:boolean;
  setUser:(user:User|null)=>void;
  refreshSession:()=>Promise<User|null>;
  logout:()=>Promise<void>;
};

const SessionContext=createContext<SessionContextValue|null>(null);

export function SessionProvider({children}:{children:React.ReactNode}){
  const [user,setUser]=useState<User|null>(null);
  const [loading,setLoading]=useState(true);

  const refreshSession=useCallback(async()=>{
    try{
      const current=await api.me();
      setUser(current);
      return current;
    }catch{
      try{
        const refreshed=await api.refresh();
        setUser(refreshed);
        return refreshed;
      }catch{
        setUser(null);
        return null;
      }
    }
  },[]);

  useEffect(()=>{
    let cancelled=false;
    (async()=>{
      try{
        const current=await api.me().catch(()=>api.refresh());
        if(!cancelled)setUser(current);
      }catch{
        if(!cancelled)setUser(null);
      }finally{
        if(!cancelled)setLoading(false);
      }
    })();
    return()=>{cancelled=true};
  },[]);

  const logout=useCallback(async()=>{
    try{
      await api.logout();
    }finally{
      setUser(null);
    }
  },[]);

  const value=useMemo(()=>({
    user,
    loading,
    setUser,
    refreshSession,
    logout,
  }),[user,loading,refreshSession,logout]);

  return <SessionContext.Provider value={value}>{children}</SessionContext.Provider>;
}

export function useSession(){
  const value=useContext(SessionContext);
  if(!value)throw new Error("useSession must be used inside SessionProvider");
  return value;
}
