import React,{createContext,useCallback,useContext,useMemo,useRef,useState} from "react";

export type ToastKind="success"|"error"|"info";

type ToastItem={
  id:number;
  kind:ToastKind;
  title:string;
  message?:string;
};

type ToastContextValue={
  show:(kind:ToastKind,title:string,message?:string)=>void;
  success:(title:string,message?:string)=>void;
  error:(title:string,message?:string)=>void;
  info:(title:string,message?:string)=>void;
};

const ToastContext=createContext<ToastContextValue|null>(null);

export function ToastProvider({children}:{children:React.ReactNode}){
  const [items,setItems]=useState<ToastItem[]>([]);
  const nextId=useRef(1);

  const remove=useCallback((id:number)=>{
    setItems(current=>current.filter(item=>item.id!==id));
  },[]);

  const show=useCallback((kind:ToastKind,title:string,message?:string)=>{
    const id=nextId.current++;
    setItems(current=>[...current.slice(-3),{id,kind,title,message}]);
    window.setTimeout(()=>remove(id),kind==="error"?7000:4500);
  },[remove]);

  const value=useMemo<ToastContextValue>(()=>({
    show,
    success:(title,message)=>show("success",title,message),
    error:(title,message)=>show("error",title,message),
    info:(title,message)=>show("info",title,message),
  }),[show]);

  return <ToastContext.Provider value={value}>
    {children}
    <div className="toast-region" aria-live="polite" aria-atomic="false">
      {items.map(item=><div
        key={item.id}
        className={"toast toast-"+item.kind}
        role={item.kind==="error"?"alert":"status"}
      >
        <div className="toast-icon" aria-hidden="true">{item.kind==="success"?"✓":item.kind==="error"?"!":"i"}</div>
        <div className="toast-copy">
          <strong>{item.title}</strong>
          {item.message&&<p>{item.message}</p>}
        </div>
        <button className="toast-close" onClick={()=>remove(item.id)} aria-label="Dismiss notification">×</button>
      </div>)}
    </div>
  </ToastContext.Provider>;
}

export function useToast(){
  const value=useContext(ToastContext);
  if(!value)throw new Error("useToast must be used inside ToastProvider");
  return value;
}
